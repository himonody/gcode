# IM系统（直播间聊天版）-Go Hertz/Kitex + WebSocket + TiDB + Redis + Kafka + etcd

[TOC]

# 1 IM系统的简介与实现思路（对齐：推/拉、路由、离线、心跳、ACK）

> 说明：本方案基于《2-即时通讯(IM)系统的实现.md》的核心思路（推模型、路由模式、心跳在线、ACK机制、协议与Handler分发），但业务场景切换为“直播间聊天（公屏/弹幕）”。
>
> 直播间聊天与私信的最大差异在于：
> - 优先级：低延迟、高吞吐、可扩展扇出
> - 一致性：弱一致，允许少量丢消息
> - 可靠性：普通公屏不做逐用户可靠投递；关键消息可走可靠投递

## 1.1 推模型和拉模型

### 1.1.1 推模型（主链路）

直播间聊天的业务本质仍然是：客户端与服务端之间基于长连接进行实时数据读写。与“拉模型轮询”相比，“推模型长连接”可以避免大量无效请求。

在直播间场景中：
- 公屏消息：全部走推模型
- 进房补历史：可使用拉模型（拉最近N条）

### 1.1.2 拉模型（辅助链路）

用户进入直播间后，为了提升体验（避免进房后一片空白），可以拉取“最近N条公屏消息”或“最近N秒的消息”。

常见策略：
- 仅保留最近N条（如50/100）
- 仅保留短时间窗口（如30s/60s）

## 1.2 在线消息推送和离线消息拉取（广播模式 vs 路由模式）

### 1.2.1 广播模式的问题

广播模式指：消息到达后推给所有IM/Conn节点，让每个节点再判断是否需要向客户端下发。

问题：
- 会把一条房间消息扩散到所有节点，浪费网络与CPU
- 节点规模越大，浪费越严重

### 1.2.2 路由模式（核心：缩小广播范围）

文档里的“路由模式”核心是把广播缩小到“确定目标机器”。

在直播间聊天中，路由从“用户维度”升级为“房间订阅维度”：
- 单聊：通常是 `userId -> connServer`
- 直播间：通常是 `roomId -> connServer集合（订阅该room的节点）`

核心目标：
- 一条房间消息只推给“有该房间订阅用户”的Conn节点
- Conn节点再在本机内存里扇出给该房间的连接列表

### 1.2.3 离线消息拉取（直播间取舍）

直播间公屏一般不做“逐用户离线可靠补发”，而是采用：
- 弱一致投递（尽力投递）：在线尽量推送，离线不保证补齐
- 进房/重连可拉取最近N条作为体验补偿

如果业务对某类消息要求必达（如系统公告、封禁通知、交易/礼物关键事件），可将其划为“关键消息”，走可靠投递子链路（见1.4）。

## 1.3 用户心跳检测和在线回调通知

文档中用Redis保存心跳时间来判断在线/离线。直播间场景依然需要心跳，目的主要是：
- 连接保活
- 及时识别僵尸连接
- 维护“房间订阅路由”的正确性（连接断开要清理订阅）

推荐实现方式：
- 使用Redis TTL表达在线：
  - `presence:{userId}` = 1，设置TTL=心跳周期*2
- Conn断开时：
  - 从本机 `roomId -> conns` 中移除连接
  - 若本机该房间连接数归零，再从 `room:subs:{roomId}` 中移除本Conn节点

## 1.4 发送消息的ACK确认机制（直播间适用范围）

文档中的ACK机制：
- 服务端推送消息后等待客户端ACK
- RedisMap（或Redis结构）存储未确认消息
- 超时后重发，收到ACK后移除

直播间公屏的取舍建议：
- 普通公屏消息：采用“尽力投递/弱一致投递”，不做逐用户ACK
  - 原因：房间人数高时，ACK会引入巨大状态量与重试压力
- 关键消息：保留ACK机制
  - 例如：系统公告、封禁通知、交易/礼物成交类事件

关键消息ACK的落地：
- Redis结构示例：
  - `ack:pending:{userId}` = zset(msgId -> timestamp)
- 重试策略：
  - 只重试可重试错误（连接抖动/短暂失败）
  - 重试次数上限 + 指数退避

# 2 应用选型和网络IO模型（对齐：WebSocket、IO多路复用）

## 2.1 WebSocket与长连接

直播间聊天要求服务端主动推送，WebSocket天然支持全双工通信，尤其适合Web端与跨平台场景。

如果是原生App场景，也可使用TCP长连接协议；但在本方案中以WebSocket为主。

## 2.2 IO模型与Go运行时

文档介绍了BIO/NIO/AIO以及epoll优势。落到Go实现中，可以这样表达：
- Go在Linux上底层依赖epoll/kqueue实现网络事件轮询
- Conn层用事件驱动 + goroutine 处理读写与业务分发
- 重点是把“连接维持”和“业务处理”解耦，避免阻塞导致连接吞吐下降

# 3 IM系统的搭建与全链路实现（对齐：协议、编解码、Handler分发、ACK）

## 3.1 全链路（直播间聊天版）

全链路建议拆成四段：
1. Conn/Gateway：维持WebSocket连接、订阅房间、下行推送
2. Ingest：鉴权、限流、内容校验、反垃圾/敏感词
3. Kafka：削峰、解耦、按房间分区保证“房间内近似顺序”
4. RoomWorker/Fanout：房间聚合、路由到订阅节点集合、节点内扇出

消息从发出到所有人看到（核心链路）：
1. 用户进房：Conn记录订阅关系
2. Conn将本机加入房间订阅集合：`room:subs:{roomId}`（Redis set）
3. 用户发消息：Conn转发到Ingest（Kitex RPC）
4. Ingest完成校验后写Kafka：topic=room_chat，key=roomId
5. RoomWorker消费该roomId对应分区，读取`room:subs:{roomId}`得到Conn节点集合
6. RoomWorker RPC调用这些Conn节点的`PushRoomMessage(roomId, msg)`
7. Conn节点在内存 `roomId -> conns` 列表循环写WS，下行给客户端

## 3.2 自定义协议与编解码（对齐：ImMsg）

文档通过`ImMsg(magic, code, len, body)`定义消息体并实现编解码。

在Go实现中可以保留同样的协议字段：
- magic：基础校验
- code/type：消息类型（login/logout/biz/heartbeat/ack/joinRoom/leaveRoom/roomChat）
- len：body长度
- body：payload（通常JSON/Protobuf）

选择建议：
- 内网RPC（Kitex）：Protobuf/Thrift
- WS下行：JSON更易调试；高性能可改为Protobuf二进制帧

## 3.3 核心Handler设计（对齐：工厂/单例/策略）

文档中提到：
- 策略模式：不同消息类型对应不同处理器
- 简单工厂：根据code分发到对应策略
- 单例：工厂内静态map维护handler

Go里可以用同样的思想表达：
- `map[msgType]HandlerFunc` 作为注册表
- 启动时注册所有handler
- WS收到消息后：解码 -> 根据type查表 -> 执行handler

建议具备的handler：
- LoginHandler：token校验，绑定userId与连接
- HeartbeatHandler：刷新TTL
- JoinRoomHandler：订阅房间，本机加入room连接集合
- LeaveRoomHandler：退房，清理订阅
- RoomChatHandler：上行聊天消息 -> Ingest/Kafka
- AckHandler：关键消息ACK（可选）

## 3.4 处理客户端ACK消息（对齐：3.10）

对关键消息启用ACK：
- Conn推送关键消息时，将msgId加入`ack:pending:{userId}`
- 客户端收到后回ACK
- Conn收到ACK后移除pending
- 超时未ACK：按重试策略重发

# 4 直播间场景的关键难点与治理（在文档思路上补齐直播间特性）

## 4.1 两级扇出：避免“对每个用户发一次”

正确的扩散路径是：
- 房间消息先扩散到“订阅节点集合”（几十台Conn）
- 再由Conn节点在本机内存扇出到连接列表

收益：
- RoomWorker侧复杂度从O(房间在线人数)降低到O(Conn节点数)
- 扇出压力被分摊到各Conn节点

## 4.2 热点房间治理

当房间超热时，`key=roomId` 的单分区会成为瓶颈。常见治理手段：

- 方案A：房间分片
  - 将同一room逻辑拆分为 `roomId#shard`
  - 允许“近似顺序”，换取吞吐扩展
- 方案B：批量聚合
  - RoomWorker按时间窗口（如20ms）聚合多条消息，批量RPC到Conn
- 方案C：降级策略
  - 普通消息降频/采样
  - 合并相近消息
  - 高优先级事件（礼物/关注/管理员）优先投递

## 4.3 限流与反刷

上行入口对 `userId/roomId` 做令牌桶限流，防止刷屏：
- 超限直接丢弃或返回错误码
- 对异常用户做封禁/冷却（Redis记录封禁状态与过期时间）

# 5 数据模型与关键Key设计（面试加分项）

## 5.1 Redis Key示例

- `presence:{userId}` -> 1（TTL，表示在线）
- `room:subs:{roomId}` -> set(connServerId)（订阅该房间的Conn节点集合）
- `room:hot:{roomId}` -> level/strategy（热点标记，可选）
- `ack:pending:{userId}` -> zset(msgId -> timestamp)（关键消息待确认，可选）

## 5.2 Kafka Topic与分区策略

- topic：`room_chat`
- key：`roomId`
- 语义：保证同房间内消费顺序更自然
- 热点治理：对超热房间可启用分片key（`roomId#shard`）或批量聚合

## 5.3 TiDB存储建议（直播间）

公屏消息通常不全量长期存储，常见做法：
- 存审核/风控需要的数据（全量或抽样）
- 存最近N条用于进房补历史（可TTL/归档）

可选表（概念示例）：
- `room_chat_message(room_id, msg_id, uid, content, created_at)`
- `room_chat_outbox(event_id, payload, status, created_at)`（如做Outbox）

# 6 面试讲述稿（按“目标-架构-难点-指标”）

## 6.1 30秒版本（电梯稿）

> 我做的是直播间公屏聊天，目标是低延迟和高吞吐。连接层用Hertz承载WebSocket长连接；用户进房后在Conn内存维护订阅关系，并在Redis维护`room:subs:{roomId}`的订阅节点集合。上行消息经Kitex的Ingest服务做鉴权、限流和内容校验后写入Kafka，按roomId分区；RoomWorker消费后只把消息投递到订阅该房间的Conn节点集合，再由Conn在本机内存扇出到具体连接。普通公屏采用“尽力投递/弱一致投递”，关键消息才启用ACK确认和重试。

## 6.2 2-3分钟版本（展开讲）

- 连接与推送：WebSocket长连接，心跳+TTL维护在线；断线清理订阅保证路由正确
- 路由与扇出：从广播升级为路由，按房间订阅节点集合投递，节点内再扇出
- 异步与削峰：Kafka解耦写入与投递，提升峰值抗压
- 可靠性取舍：普通公屏弱一致投递；关键消息通过ACK+pending集合+重试保证可追踪
- 热点治理：房间分片/批量聚合/降级策略应对超热房间

## 6.3 高频追问与回答模板

- 为什么不对公屏做ACK？
  - 公屏人数高，逐用户ACK会导致状态量与重试成本巨大；因此普通公屏走弱一致投递，关键消息才走可靠投递。

- 如何避免广播到所有机器？
  - 通过Redis维护`room:subs:{roomId}`，只投递到订阅该房间的Conn节点集合。

- 热点房间怎么办？
  - 通过房间分片、批量聚合、以及降级（采样/合并/优先级）来治理热点。

- 你们怎么做限流反刷？
  - 对userId/roomId做令牌桶限流，超限丢弃或返回错误，并用Redis做封禁与冷却。
