# 功能模块划分（按目录/职责）
## 1) 接入层（WebSocket Server）
- **`internal/websocket_server.go`**
    - 监听端口、`Upgrade` 成 WebSocket
    - 创建 `Link` 并进入收包循环
    - 启动 MQ 消费者（后端推送、扩容事件）
    - etcd 注册、续租、节点 load 上报
    - **优雅下线**（停止接入、降权/注销、连接迁移/关闭）

## 2) 连接抽象与连接管理（Link / LinkManager）
- **`internal/link/*` + `internal/link/manager.go`**
    - `Link`：封装 net.Conn + WS session，提供 `Send/Receive/Close`、超时、缓冲、重试等
    - `LinkManager`：
        - 按 `bizID-userID` 生成 linkID，维护连接表
        - 支持按用户查找连接（后端 push 定位）
        - 维护连接数 `Len()` 供 load 上报/调度
          -（项目里还有 GracefulClose/重定向相关逻辑，属于面试高频点）

## 3) 消息处理链（LinkEventHandler）
- **`internal/linkevent/*`**
    - `Handler`：单条上行（前端→网关→业务后端 gRPC）+ 下行 push + ACK + 重传管理
    - `BatchHandler + Coordinator`：将上行请求按 biz 聚合 **批量调用后端**
    - `LimitHandler`：消息限流（`golang.org/x/time/rate`），心跳放行，超限回“被限流”消息并中断链路
- 典型能力：
    - 编解码（`codec`）、加解密（`encrypt`）
    - 幂等去重（Redis `SetNX`，按 `bizID-key`）
    - 上行 ACK、下行 ACK、下行重传（`pushretry`）

## 4) 后端交互层（gRPC + MQ）
- **gRPC**（同步链路）
    - `BackendService.OnReceive`：前端上行消息转业务后端，带超时+退避重试
- **MQ 事件系统**（异步链路）
    - `internal/event/*`
    - `Consumer`：消费后端推送（`pushMessage`）等事件
    - `SendToBackendEventProducer / BatchSendToBackendEventProducer`：把上行消息（或批量）投递到 MQ（削峰填谷/解耦）

## 5) 注册发现与集群治理
- **ServiceRegistry（etcd）**
    - lease 注册/续租
    - 节点状态更新：`load = linkManager.Len()`
    - 优雅注销：降权→等待→删除（配合连接迁移）

## 6) 限流/灰度/扩缩容（稳定性与容量）
- **连接接入侧限流（灰度放量）**
    - `TokenLimiter` + `StartRampUp`：逐步增加可接入令牌
    - `ExponentialBackOff`：容量不足时临时拒绝并退避
- **扩缩容**
    - `scaleUp` 事件消费 + `DockerScaler`（从 wire 注入看）实现自动扩容与再均衡
- **WebhookServer**
    - 控制面入口（扩容、再均衡触发等）

---

# 系统设计（端到端数据流）
## A. 前端→网关→后端（上行）
1. `Accept` 前先拿连接令牌（灰度/容量控制）
2. `Upgrade` 成 WS，创建 `Link`（带超时、缓冲、单用户限流等）
3. 收到 payload：
    - `codec.Unmarshal` → `decrypt`
    - Redis `SetNX` 做幂等（重复 key 丢弃）
    - 按 `Cmd` 分发：
        - 心跳：原样回
        - 上行消息：走 gRPC `OnReceive` 或走批处理 `Coordinator`
    - 成功后回 `UPSTREAM_ACK`

## B. 后端→网关→前端（下行）
1. 后端把 `PushMessage` 发 MQ
2. 网关 consumer 收到后，按 `(bizID, receiverId)` 去 `LinkManager` 找连接
3. `linkEventHandler.OnBackendPushMessage`：
    - 加密 + 编码 + `Link.Send`
    - 启动重传任务，等待前端 `DOWNSTREAM_ACK` 停止重传

## C. 集群层（调度/下线）
- 节点注册 etcd + keepalive
- 周期上报 load
- 优雅下线：停止接入 → 降权/注销 → 获取可用节点 → 连接迁移/关闭

---

# 项目“痛点”（为什么需要这么设计）
## 1) 海量长连接带来的容量与稳定性
- **痛点**：瞬时涌入、发布放量、雪崩（Accept 过快导致 FD/CPU/内存顶满）
- **现有解法**：`TokenLimiter` + ramp-up + backoff 控制接入速度；空闲连接清理；优雅下线

## 2) 消息可靠性（幂等/重传/ACK）
- **痛点**：断网重连、重复发送、下行丢包/乱序
- **现有解法**：
    - 上行幂等（Redis SetNX）
    - 下行 ACK + 重传管理（pushretry）
    - Key 贯穿端到端关联

## 3) 吞吐与后端压力
- **痛点**：每条消息都 gRPC，RPC 数量爆炸、尾延迟上升
- **现有解法**：
    - `Coordinator` 做批量 gRPC
    - 或通过 MQ producer 批量投递削峰填谷

## 4) 多节点路由与连接定位
- **痛点**：后端推送要命中“持有该用户连接的网关节点”（否则本机找不到 link）
- **项目暗示的方向**：
    - 通过注册发现/标签/权重/负载做调度
    - 扩容再均衡与连接迁移（scaleUp + GracefulClose）
- **面试会追问**：如果推送打错节点怎么办？（要么业务侧先查 user->node 映射，要么网关间转发，要么统一通过路由层/一致性哈希）

---

# 面试点（高频可深挖的问题清单）
## 1) 为什么选 `gobwas/ws`？性能点在哪里？
- 零拷贝/更少分配、性能优先的 WS 实现
- 压缩协商（你还打开了 `wsflate/extension.go`）在高吞吐下的 CPU vs 带宽权衡

## 2) Link 抽象的意义？为什么不直接在 handler 里用 conn？
- 把超时、重试、缓冲、限流、关闭语义统一封装
- 便于 LinkManager 做生命周期管理与迁移

## 3) 幂等怎么做？为什么用 `SetNX(bizID-key)`？过期怎么设？
- key 的生成/去重窗口/误判风险
- 失败场景：后端处理成功但 ACK 丢了，是否允许重放？（需要结合业务幂等）

## 4) 下行 ACK + 重传怎么设计？何时停止？最大重试策略？
- 重试间隔、最大次数、客户端 ACK 丢失、客户端重复 ACK 的处理

## 5) 批处理两套方案怎么取舍？
- `Coordinator`（批量 RPC） vs batch_send_to_backend.go（批量 MQ）
- 各自适合：
    - RPC：需要同步响应、强交互
    - MQ：解耦削峰、最终一致

## 6) etcd 注册、租约、load 上报如何用于调度？
- 用 `Load/Capacity/Weight/Labels/Location` 怎么做就近路由/灰度隔离
- 优雅下线为什么要“先降权再迁移再注销”

## 7) 灰度放量为什么用 token + ramp-up + backoff？
- 解决“发布/扩容后瞬间接入导致雪崩”
- backoff 为什么要 reset（成功后重置，避免持续长退避）

---

# 状态总结
- **已完成**：把项目功能模块、系统设计主链路、核心痛点与面试可深挖点按你仓库的真实实现做了结构化梳理。  


# 30 秒概述（面试开场电梯稿）
这是一个 **企业级 IM WebSocket 网关**：前端长连接接入后抽象成 `Link`，由 `LinkManager` 统一管理；所有消息通过 `LinkEventHandler` 链路做 **解密/编解码/限流/幂等/ACK 与重传**，上行可走 **gRPC 同步**或 **MQ 异步（支持批量）** 到业务后端；节点通过 **etcd 注册发现 + load 上报** 支撑多实例水平扩展，并提供 **灰度接入（令牌桶 ramp-up + 退避）**、**扩缩容再均衡** 和 **优雅下线（停止接入→降权→迁移/关闭连接）**。

---

# 3 分钟架构（讲清模块 + 两条主链路）
## 1) 系统分层
- **接入层**：`WebSocketServer` 监听/Upgrade/收包循环，负责启动消费者、注册中心、优雅关闭
- **连接层**：`Link` 封装发送接收、超时/缓冲/重试；`LinkManager` 负责连接生命周期与按用户定位
- **消息处理层**：`linkevent` 里一组 handler 形成处理链
    - `LimitHandler`（消息限流）
    - `Handler/BatchHandler`（解密、幂等、cmd 分发、上行转发、下行 push、ACK、重传）
    - `Coordinator`（按 biz 聚合批量转发）
- **后端交互层**：
    - gRPC：`BackendService.OnReceive`
    - MQ：consumer（push/扩容事件）+ producer（上行事件，含批量）
- **治理层**：etcd 注册、租约 keepalive、load 上报、可用节点查询、优雅下线与迁移
- **容量与稳定性**：接入侧 `TokenLimiter` 灰度放量 + 指数退避；连接空闲清理；单用户/全局限流

## 2) 两条主链路（端到端）
### A. 上行（客户端 → 网关 → 业务后端）
1. `Accept` 前先拿令牌（容量不足则退避拒绝）
2. Upgrade 成 WS，创建 `Link` 纳入 `LinkManager`
3. 收到 payload：
    - 反序列化 `Message` → 解密 `Body`
    - Redis `SetNX(bizID-key)` 幂等去重
    - `Cmd` 分发：心跳回显；上行业务消息转发到后端（gRPC 或批量）；成功回 `UPSTREAM_ACK`

### B. 下行（业务后端 → 网关 → 客户端）
1. 后端发 `PushMessage` 到 MQ
2. 网关 consumer 收到后，用 `(bizID, userID)` 从 `LinkManager` 定位连接
3. 加密 + 编码后 `Link.Send()` 下发；启动重传任务
4. 前端回 `DOWNSTREAM_ACK` 后停止重传

## 3) 多节点与运维
- 节点 `Node{Id, Ip, Port, Weight, Capacity, Load, Labels}` 注册到 etcd，定期上报 `Load=连接数`
- 优雅下线：停止接入、降权/注销、获取其他可用节点，然后迁移/关闭连接

---

# 10 分钟深挖（按“为什么这么做 + 关键细节 + trade-off”讲）
下面是一套你可以照着讲的 10 分钟结构（每段 1-2 分钟），面试官想深入时你也能顺势展开。

## 1) 为什么需要网关（职责边界）
- **目标**：把“长连接接入、协议、安全、可靠性、路由、治理”从业务服务里抽出来
- **边界**：
    - 网关不做业务逻辑，只做协议/连接/投递可靠性与治理
    - 业务后端只关心 `OnReceive`/推送事件消费

可追问点：
- 为什么不用业务服务直接持 WS？（资源模型、扩缩容、发布风险、协议与业务耦合）

## 2) Link / LinkManager 的抽象价值（连接模型）
- **Link**：把 `Send/Receive/Close`、超时、重试、缓冲、压缩状态等封装，避免散落在 handler
- **LinkManager**：
    - 用 `bizID-userID` 生成 `linkID`，便于定位
    - `Len()` 作为节点负载指标（上报/调度）
    - 生命周期：创建/移除/优雅关闭（迁移）

可追问点：
- 为什么用 `(bizID,userID)` 做 linkID？多端登录/多设备怎么办？（可以扩展为 `deviceID`/`connID`）
- 内存连接表的并发安全与性能（sync map / 分片 map）

## 3) 消息协议 Message 的设计（Cmd/Key/Body）
- `Cmd` 把“心跳/上行/ACK/限流提示”等控制语义显式化
- `Key` 贯穿幂等与 ACK 关联
- `Body` 支持加密 + 承载业务 protobuf（网关只当不透明载荷处理）

可追问点：
- Key 的生成策略、重复窗口、过期时间如何选
- 加密/压缩在 CPU 与带宽之间如何权衡（尤其移动端弱网）

## 4) 幂等去重（Redis SetNX）的工程化细节
- **做法**：`SetNX(bizID-key, key, expiration)`
    - 重复上行直接丢弃，避免业务侧重复写入/重复投递
- **关键参数**：
    - `expiration` 是幂等窗口：太短会漏防重，太长占内存且影响重试语义

可追问点（很加分）：
- 如果“业务处理成功但 ACK 丢了”，前端重发会被丢弃，这是否符合业务？
    - 解决方案：ACK 设计、业务幂等、或区分“可重放/不可重放”消息类型

## 5) 下行可靠性：ACK + 重传（pushretry）
- **机制**：下行发送后启动重传任务；收到 `DOWNSTREAM_ACK` 后停止重传
- **设计要点**：
    - 重传间隔、最大次数、超时后策略（告警/降级/丢弃）
    - ACK 幂等（重复 ACK 不应有副作用）

可追问点：
- 如何避免重传风暴（指数退避、抖动、全局限流）
- 客户端重连后 ACK 如何处理（会话/消息状态同步）

## 6) 上行到后端：gRPC vs MQ（取舍与场景）
- **gRPC（同步）**：
    - 优点：请求-响应语义清晰，适合强交互（比如必须立即回执）
    - 风险：RPC 数量爆炸、尾延迟、后端抖动会反噬网关
- **MQ（异步）**：
    - 优点：削峰填谷、解耦、后端可水平扩展消费
    - 风险：最终一致、链路更复杂（幂等/顺序/回执）

可追问点：
- 你项目里为什么两套都保留？（面向不同业务类型或演进路线）

## 7) 批处理：两种批量（Coordinator vs Batch MQ Producer）
你项目里有两类“批”：
- **`Coordinator`（批量 gRPC）**：按 `bizID` 聚合 `OnReceiveRequest`，到阈值/超时触发批调用
- **batch_send_to_backend.go（批量 MQ）**：把多条上行聚合成 JSON `{msgs:[...]}` 一次 Produce

面试讲法（trade-off）：
- gRPC 批：减少 RPC 次数、降低 overhead，仍保留“近实时响应”
- MQ 批：减少 MQ Produce 次数，适合大吞吐异步链路

可追问点（亮点）：
- 批超时/批大小如何调参（吞吐 vs 延迟）
- 批内失败怎么回传、如何定位单条错误
- 批处理 goroutine/锁竞争、内存增长与 backpressure

## 8) 多节点治理：etcd 注册 + load 上报 + 优雅下线
- **Node**：`Weight/Capacity/Load/Labels/Location` 是调度输入
- **优雅下线**：
    - 先停止接入（避免新连接进入）
    - 降权/注销（让路由层不再选中）
    - 获取可用节点列表，引导连接迁移/重连
- **灰度接入**：`TokenLimiter` ramp-up + backoff 防雪崩

可追问点：
- 推送打错节点怎么办？（user->node 映射、路由层一致性哈希、网关间转发）
- load 指标为什么用连接数？能否引入 CPU/内存/消息速率等综合负载？

---

## 你可以直接背的“收尾一句”
这套网关的核心价值是：**用连接抽象 + 事件处理链把协议、安全、可靠性与吞吐治理标准化**；再用 **注册发现、负载上报、灰度接入、优雅下线/扩缩容** 让长连接系统可以像普通微服务一样发布、扩展和回滚。

---

# 状态总结
- **已完成**：给出可直接用于面试的 `30秒/3分钟/10分钟` 三套讲稿结构，并覆盖你项目的关键实现点与可追问深挖点。