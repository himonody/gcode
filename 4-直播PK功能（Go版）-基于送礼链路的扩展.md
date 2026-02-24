# 直播PK功能（Go版）-基于送礼链路的扩展

[TOC]

# 1 直播PK功能的设计与分析（对齐原文档）

> 直播PK是在“送礼扣费”链路之上叠加“全房间可见的PK进度条与上下线状态”。

核心要求：
- PK进度条全直播间可见
- 处理乱序：前端以最新进度为准
- 中途进房：能拿到当前PK状态

# 2 直播PK功能的实现（Go版映射）

## 2.1 复用并封装“单播/广播推送”能力

原文档在消费者内封装：
- `sendImMsgSingleton()`：单播
- `batchSendImMsg()`：批量推送（广播）

Go版建议拆分为两层：
- Router/RoomWorker：面向房间，推到订阅节点集合
- Conn节点：面向连接，节点内扇出

对业务侧暴露统一接口（Kitex RPC示例语义）：
- `PushToUser(userId, bizCode, payload)`
- `PushToRoom(roomId, bizCode, payload)`

这样送礼、PK、红包雨都复用同一套推送能力。

## 2.2 PK进度条实现（Redis + 顺序控制）

原文档思路：
1. Redis按roomId存PK进度
2. Redis按roomId存送礼消息有序id（避免乱序）
3. 用Lua或锁保证原子性
4. Kafka按roomId分区减少乱序

Go版落地建议：
- Kafka：送礼事件 topic 的 key 使用 `roomId`，同房间进入同分区，消费顺序更稳定
- Redis：
  - `pk:num:{roomId}`：当前进度值
  - `pk:seq:{roomId}`：单调递增序列（或直接用毫秒时间戳）
  - `pk:is_over:{roomId}`：PK结束标记（可选）

原子更新：
- 使用Lua一次性完成：
  - 判断是否结束
  - 获取并递增seq
  - 根据送礼方向 incr/decr pk:num
  - 返回 `pkNum, seq, winnerId(可选)`

前端渲染去乱序：
- payload带 `seq`（或timestamp）
- 前端只渲染 seq 更大的那条消息

## 2.3 PK上下线逻辑（Redis setnx + 过期）

原文档用 `setIfAbsent`：
- `livingOnlinePk:{roomId}` = pkObjId（过期12小时）

Go版落地：
- `pk:online:{roomId}` = pkObjId（TTL=PK最大时长）
- 上线接口：
  - 校验“主播不能参与PK”
  - `SETNX pk:online:{roomId}` 成功则上线
  - 上线成功后广播 `PK_ONLINE` 事件到房间
- 下线：
  - 用户主动下线
  - 用户断线：通过IM离线回调（心跳下线）触发
  - 下线必须做“身份校验”：只有当前pkObjId本人可下线

## 2.4 PK送礼事件如何驱动进度条

PK进度条本质是“送礼金额驱动的一条房间状态流”。

建议把PK送礼定义为一种送礼类型：
- `SendGiftType = PK_SEND_GIFT`

消费端（SendGiftConsumer）处理：
1. 扣款成功
2. Lua更新PK进度并生成 `pkNum/seq/winnerId`
3. 广播两类事件到房间：
   - 礼物特效（全房间可见）
   - PK进度更新（全房间可见）

# 3 面试讲述要点

## 3.1 30秒版本

> PK是在送礼链路上增加“房间状态同步”。我用Kafka按roomId分区保证同房间事件顺序，消费端用Redis+Lua原子更新PK进度并生成单调序列号；推送时携带序列号，前端只渲染最新序列，解决网络乱序。PK上下线用Redis setnx做并发互斥，并通过离线回调自动下线与清理状态。

## 3.2 高频追问

- 如何解决进度条乱序？
  - 后端生成seq/timestamp，前端以seq更大的覆盖；同时Kafka按roomId分区减少乱序。

- 中途进房怎么拿当前状态？
  - 进房时API/Conn拉取 `pk:online` 和 `pk:num`，作为初始化状态下发。

- 为什么Lua？
  - 把“判断结束 + seq递增 + num更新”合成一次Redis原子操作，避免并发读写导致状态不一致。
