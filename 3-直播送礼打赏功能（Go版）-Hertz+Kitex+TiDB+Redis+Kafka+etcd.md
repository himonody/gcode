# 直播送礼打赏功能（Go版）-Hertz/Kitex + TiDB + Redis + Kafka + etcd

[TOC]

# 1 直播间送礼打赏功能的设计与分析（对齐原文档）

## 1.1 业务链路概览

送礼的链路与原文档一致，核心参与方：
- IM/Conn：负责把礼物特效、送礼成功/失败等事件推送给直播间观众或单个用户
- API：提供送礼入口、参数校验、限流与鉴权
- 钱包：负责余额扣减、流水记录、充值入账
- 礼物：礼物配置、礼物记录
- Router/房间路由：将“直播间广播”缩小到订阅该房间的Conn节点集合
- 支付中台：负责充值、支付回调（本章主要讲钱包侧如何承接回调）

送礼的关键目标：
- 余额扣减正确（不能多扣/少扣）
- 送礼请求幂等（防止重复点击/重试导致重复扣款）
- 广播推送高性能（不要对每个观众逐一RPC）

## 1.2 送礼场景的可靠性与一致性取舍

送礼属于“资金相关”场景，必须强调：
- 扣款与流水必须一致
- MQ消费要防重复（幂等）
- 推送失败不影响扣款正确性，但要有补偿或可观测性

推荐一致性方案：
- API发起送礼：只负责写入“送礼请求事件”到Kafka（或写入Outbox表）
- 钱包扣款：在钱包服务内使用事务保证“余额变更 + 流水记录”一致
- 礼物记录：可异步落库（不影响资金正确性）

# 2 公共组件优化（Go版映射）

> 原文档在本章做了断言与全局异常、以及计数器限流。

## 2.1 断言/错误码/全局异常（Go落地）

在Go中建议统一错误规范：
- `type BizError struct { Code int; Msg string }`
- Hertz中使用中间件统一把 `error` 转换为标准JSON响应
- 断言函数：
  - `AssertNotNil(v, err)`
  - `AssertTrue(cond, err)`

目标：
- 让业务代码减少大量 `if err != nil` + 拼接返回结构
- 面试可说“统一错误码与全链路可观测”

## 2.2 限流（计数器/令牌桶）

原文档采用“计数器算法 + Redis + 注解拦截器”。Go可以这样映射：
- Hertz中间件：在路由处理前执行限流
- Redis计数器Key：`limit:{userId}:{path}`
- 过期时间=窗口期（second）

建议增强：
- 对关键接口（送礼/开播/关播/红包雨准备）启用限流
- 对游客/未登录请求使用IP维度限流

# 3 基本服务的搭建（Go版：礼物服务 + 钱包服务 + 支付回调承接）

## 3.1 礼物服务

### 3.1.1 职责与接口

礼物服务负责：
- 礼物配置查询：单个礼物/礼物面板
- 礼物记录写入：记录送礼行为（可异步）

服务形态：
- 对外：Kitex RPC（供API、送礼消费者调用）
- 对内存储：TiDB（或MySQL兼容协议）
- 缓存：Redis（礼物列表、单个礼物配置）

### 3.1.2 数据表（沿用原文档概念）

- `t_gift_config(gift_id, price, gift_name, status, cover_img_url, svga_url, create_time, update_time)`
- `t_gift_record(id, user_id, object_id, gift_id, price, price_unit, source, send_time, json)`

### 3.1.3 Redis Key建议

- `gift:config:{giftId}`：单个礼物配置（string/json）
- `gift:list`：礼物列表（list或string/json）
- `gift:list:lock`：初始化礼物列表锁（防重复写入）

缓存策略：
- 列表缓存30分钟
- 单品缓存可更长（如1小时），更新礼物配置后主动删除缓存

## 3.2 钱包体系

### 3.2.1 职责

钱包服务负责：
- 查询余额
- 扣款/加款
- 流水记录
- 充值入账（承接支付回调）

### 3.2.2 核心表（概念）

- `wallet_account(user_id, balance, update_time, version)`
- `wallet_trade(id, user_id, amount, trade_type, biz_id, create_time)`

关键点：
- 扣款采用“余额充足校验 + 原子扣减”
- 建议使用乐观锁（version）或 `update ... where balance>=?` 的方式保证不超扣

### 3.2.3 扣款接口（送礼专用）

- `ConsumeForSendGift(userId, amount, bizId)`

幂等建议：
- bizId（或sendGiftUuid）作为幂等键
- 钱包侧在流水表增加唯一索引：`unique(user_id, biz_id, trade_type)`
- 若重复请求，直接返回“已处理”结果

## 3.3 送礼链路的异步化（Kafka）

### 3.3.1 为什么用Kafka

- 削峰：送礼是高频峰值动作
- 解耦：API不直接依赖钱包/礼物/IM的RT
- 便于扩容：消费者可水平扩展

### 3.3.2 Topic设计

- `send_gift`：送礼事件
  - key建议：`roomId`（房间内事件更自然；若无房间概念也可用userId）
  - value：`{uuid, roomId, userId, receiverId, giftId, price, url, type}`

### 3.3.3 消费端（SendGiftConsumer）的职责

消费端完成：
1. 消费幂等：Redis `setnx(send_gift:consume:{uuid})` 或 钱包侧bizId唯一约束
2. 调钱包扣款：成功/失败
3. 成功：
   - 异步写礼物记录（可同consumer内调用礼物服务RPC写入）
   - 推送礼物特效给直播间观众（广播）
4. 失败：
   - 单播通知送礼人失败原因

# 4 推送与直播间广播（Go版与IM文档衔接）

## 4.1 广播推送的正确姿势

不要对房间每个观众逐个调用Router/Conn。

建议：
- 先获得订阅房间的Conn节点集合：`room:subs:{roomId}`（Redis set）
- RoomWorker或Router调用这些Conn节点的 `PushRoomEvent(roomId, payload)`
- Conn节点在本机内存 `roomId -> conns` 扇出

## 4.2 送礼成功/失败事件（bizCode）

沿用原文档“bizCode区分业务消息”的思想，在Go中可以定义：
- `LIVING_ROOM_SEND_GIFT_SUCCESS`
- `LIVING_ROOM_SEND_GIFT_FAIL`

送礼成功：广播给全房间（特效、飘屏）
送礼失败：只通知送礼人（失败原因）

# 5 面试讲述稿（目标-架构-难点-指标）

## 5.1 30秒版本

> 送礼属于资金相关链路，我把入口做成异步化：API鉴权限流后把送礼事件写入Kafka；消费端做幂等校验后调用钱包服务原子扣减余额并落流水，扣款成功再异步写礼物记录并通过房间路由广播礼物特效给观众。资金链路强调幂等与一致性，推送链路强调房间维度广播与性能。

## 5.2 高频追问回答

- 为什么用Kafka？
  - 送礼峰值高，Kafka用于削峰解耦；同时方便按roomId分区减少房间内乱序。

- 如何防止重复扣款？
  - 使用业务幂等键（uuid/bizId），钱包流水表加唯一约束；消费端也可用Redis setnx防重复消费。

- 推送怎么做高性能？
  - 用房间订阅集合把广播范围缩小到Conn节点集合，节点内扇出，不做逐观众RPC。
