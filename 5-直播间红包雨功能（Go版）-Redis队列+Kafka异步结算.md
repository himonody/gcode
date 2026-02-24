# 直播间红包雨功能（Go版）-Redis队列 + Kafka异步结算

[TOC]

# 1 红包雨功能的设计与分析（对齐原文档）

红包雨包含两段链路：
- 生成红包：主播准备红包雨金额列表并存储（Redis或本地内存）
- 抢红包：观众领取红包（高并发），并异步结算到钱包

关键设计点（与原文档一致）：
- 红包数据是临时性数据，适合Redis或本地内存
- 防止重复生成、重复开始
- 领取接口安全：用随机code作为领取凭证，避免跨房间刷接口
- 高并发下避免大Key与Redis阻塞

# 2 红包雨功能的实现（Go版映射）

## 2.1 红包雨配置对象操作

### 2.1.1 数据表（沿用原文档概念）

`t_red_packet_config`：
- `anchor_id` 主播id
- `total_price` 总金额
- `total_count` 红包数量
- `config_code` 唯一code（关键：领取凭证）
- `status` 状态：待准备/已准备/已发送
- `total_get/total_get_price/max_get_price` 统计字段

### 2.1.2 服务拆分

- API（Hertz）：主播准备/开始红包雨；观众抢红包
- RedPacket服务（Kitex）：红包雨配置、生成金额列表、领取、统计
- 钱包服务（Kitex）：加款入账
- IM/Router：开始红包雨时通知全房间

## 2.2 红包雨金额列表生成与存储（Redis List + 分片）

原文档方案：
- Redis List 存储金额列表
- 列表拆分成小批量写入，避免Redis输出缓冲区堵塞
- 金额生成算法：二倍均值法

Go版落地建议：
- Redis Key：
  - `redpkt:list:{code}`：List，元素为整数金额
  - `redpkt:init:lock:{code}`：准备锁（防重复生成）
  - `redpkt:prepare:ok:{code}`：准备完成标记
  - `redpkt:notify:{code}`：已通知开始标记（防重复广播）

大Key治理：
- 若单场红包数量很大（如10万），建议分片：
  - `redpkt:list:{code}:{shard}`，shard=hash(userId)%N 或 round-robin

准备流程（主播点击“准备红包雨”）：
1. 校验配置状态必须是“待准备”
2. `SETNX redpkt:init:lock:{code}` 加锁
3. 生成金额列表（二倍均值法）
4. 按批次（如100个一批）写入Redis List
5. 更新DB状态为“已准备”，并写 `redpkt:prepare:ok:{code}`

## 2.3 开始红包雨（IM广播通知）

主播点击“开始红包雨”：
1. 校验 `redpkt:prepare:ok:{code}` 存在
2. 校验 `redpkt:notify:{code}` 不存在，然后写入（防重复开始）
3. 查询房间在线用户（或直接对房间广播）
4. 通过Router/RoomWorker广播 `RED_PACKET_CONFIG` 事件：payload包含 `config_code/total_count/total_price/倒计时` 等
5. 更新DB状态为“已发送”

推送建议：
- 不要在业务服务里“逐用户批量发送”，而是调用 `PushToRoom(roomId, bizCode, payload)`

## 2.4 领红包（Redis pop + Kafka异步结算）

领取流程（观众点击抢红包）：
1. 入参必须携带 `config_code`（不要仅用roomId/anchorId）
2. 从 `redpkt:list:{code}` 执行 `RPOP/LPOP` 取出一个金额
3. 取不到则返回“已抢完”
4. 取到金额后，发送Kafka消息 `receive_red_packet` 做异步结算与统计

Kafka消息体（概念）：
- `{price, userId, roomId, code}`

消费者（ReceiveRedPacketConsumer）处理：
- 统计：
  - `redpkt:total_get_count:{bucket}` hash(code->count)
  - `redpkt:total_get_price:{bucket}` hash(code->price)
  - `redpkt:user_total:{userId}` string（记录用户累计）
- 入账：调用钱包 `Incr(userId, price, bizId=code+userId+offset)`
- 持久化：
  - 可以按原文档“每次抢到就持久化”
  - 更推荐：活动结束后汇总落库（降低DB压力）

幂等建议：
- 每次领取生成 `bizId = code + ":" + userId + ":" + seq`，钱包侧以bizId去重
- 或消费者侧 `SETNX redpkt:consume:{bizId}` 防重复

# 3 安全与风控（原文档问题的Go版回答）

- 防刷：
  - 对领取接口按 `userId + code` 限流
  - 对IP维度限流（游客）
- 防跨房间领取：
  - 用随机 `config_code` 做凭证
  - code与roomId在DB中可校验绑定关系（防伪造）

# 4 面试讲述稿

## 4.1 30秒版本

> 红包雨是典型的高并发抢占场景。我把红包金额列表预先生成并存入Redis List，领取时用pop原子取出一个金额，避免锁竞争；抢到后通过Kafka异步做统计与钱包入账，削峰并隔离DB压力。开始红包雨通过IM房间广播通知观众，领取凭证使用随机code防止跨房间刷接口，并配合限流与幂等保证资金正确。

## 4.2 高频追问

- 为什么用List pop？
  - pop是原子操作，天然适合“一个红包只能被一个人领到”的竞争语义。

- 如何避免Redis大Key？
  - 批量拆分写入；红包数量极大时按code分片多个List。

- 钱包入账如何幂等？
  - 用bizId唯一键（code+userId+seq），钱包流水表唯一约束，重复消息不重复入账。
