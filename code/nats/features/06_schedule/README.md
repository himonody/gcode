# NATS JetStream 定时消息

## 概述

本示例演示如何使用 NATS JetStream 的定时消息功能，通过 `Nats-Schedule` 头部实现消息的定时投递。与延迟消息类似，但更侧重于在特定时间点投递消息。

**注意：** 此功能需要 NATS Server 2.11+ 版本，并且需要在流上启用调度器功能。

## 核心概念

### 什么是定时消息？

定时消息是指在指定的绝对时间点投递的消息，而不是相对于发布时间的延迟。适用于需要在特定时刻执行的任务。

### 延迟消息 vs 定时消息

| 特性 | 延迟消息 (05_delay) | 定时消息 (06_schedule) |
|------|-------------------|---------------------|
| 时间指定 | 相对时间（延迟） | 绝对时间（时间点） |
| 使用场景 | "5分钟后执行" | "明天10点执行" |
| 计算方式 | `Now() + delay` | 指定具体时间 |
| 典型应用 | 重试、超时检查 | 定时任务、预约提醒 |

## 架构图

```
┌─────────────┐
│  Producer   │
└──────┬──────┘
       │ Publish (T0 = 18:00)
       │ Header: Nats-Schedule = @at 18:10:00
       ▼
┌─────────────────────────────────────────┐
│   JetStream Stream (F_SCHEDULE)         │
│   消息存储但不立即投递                    │
└──────────────┬──────────────────────────┘
               │
               │ 等待到 18:10:00...
               │
               ▼ (18:10:00)
┌─────────────────────────────────────────┐
│   Scheduler 触发投递                     │
└──────────────┬──────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────┐
│   Consumer                              │
│   在 18:10:00 收到消息                   │
└─────────────────────────────────────────┘
```

## 关键配置

### 1. Nats-Schedule 头部

```go
// 指定绝对时间
fireAt := time.Date(2026, 2, 3, 10, 30, 0, 0, time.UTC)
msg.Header.Set("Nats-Schedule", "@at " + fireAt.Format(time.RFC3339))
```

**格式说明：**
- `@at` 前缀：表示绝对时间
- 时间格式：RFC3339 格式（UTC 时区）
- 示例：`@at 2026-02-03T10:30:00Z`

### 2. Nats-Schedule-Target 头部

```go
msg.Header.Set("Nats-Schedule-Target", subject)
```

**作用：** 指定消息到期后投递到哪个主题。

## 运行示例

### 前置条件

```bash
# 启动 NATS Server 2.11+
docker run -p 4222:4222 nats:2.11-alpine -js
```

### 运行程序

```bash
cd /Users/mac/workspace/code/gcode/nats/features/06_schedule
go run main.go
```

### 预期输出

```
2026/02/02 18:40:00 发布定时消息，投递时间 2026-02-02T18:40:10+08:00（当前时间=2026-02-02T18:40:00+08:00）
2026/02/02 18:40:00 定时消息演示运行中；按 Ctrl+C 退出
... 等待 10 秒 ...
2026/02/02 18:40:10 收到消息于 2026-02-02T18:40:10.123456789+08:00: 定时消息内容
```

## 使用场景

### 1. 定时报告生成

```go
// 每天凌晨 2 点生成报告
func scheduleReport(js nats.JetStreamContext, reportDate time.Time) error {
    // 设置为第二天凌晨 2 点
    fireAt := time.Date(reportDate.Year(), reportDate.Month(), reportDate.Day()+1, 2, 0, 0, 0, time.UTC)
    
    msg := &nats.Msg{
        Subject: "reports.generate",
        Data:    []byte(reportDate.Format("2006-01-02")),
        Header:  nats.Header{},
    }
    msg.Header.Set("Nats-Schedule", "@at " + fireAt.Format(time.RFC3339))
    msg.Header.Set("Nats-Schedule-Target", "reports.generate")
    
    return js.PublishMsg(msg)
}
```

### 2. 预约提醒

```go
// 预约时间前 1 小时提醒
func scheduleAppointmentReminder(js nats.JetStreamContext, appointment Appointment) error {
    reminderTime := appointment.Time.Add(-1 * time.Hour)
    
    msg := &nats.Msg{
        Subject: "appointments.reminder",
        Data:    marshalAppointment(appointment),
        Header:  nats.Header{},
    }
    msg.Header.Set("Nats-Schedule", "@at " + reminderTime.UTC().Format(time.RFC3339))
    msg.Header.Set("Nats-Schedule-Target", "appointments.reminder")
    
    return js.PublishMsg(msg)
}
```

### 3. 活动开始通知

```go
// 活动开始时发送通知
func scheduleEventNotification(js nats.JetStreamContext, event Event) error {
    msg := &nats.Msg{
        Subject: "events.start",
        Data:    marshalEvent(event),
        Header:  nats.Header{},
    }
    msg.Header.Set("Nats-Schedule", "@at " + event.StartTime.UTC().Format(time.RFC3339))
    msg.Header.Set("Nats-Schedule-Target", "events.start")
    
    return js.PublishMsg(msg)
}
```

### 4. 定时任务调度

```go
// 每天特定时间执行任务
func scheduleDailyTask(js nats.JetStreamContext, hour, minute int) error {
    now := time.Now().UTC()
    nextRun := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)
    
    // 如果今天的时间已过，安排到明天
    if nextRun.Before(now) {
        nextRun = nextRun.Add(24 * time.Hour)
    }
    
    msg := &nats.Msg{
        Subject: "tasks.daily",
        Data:    []byte("daily-task"),
        Header:  nats.Header{},
    }
    msg.Header.Set("Nats-Schedule", "@at " + nextRun.Format(time.RFC3339))
    msg.Header.Set("Nats-Schedule-Target", "tasks.daily")
    
    return js.PublishMsg(msg)
}
```

## 最佳实践

### 1. 时区处理

```go
// ✅ 推荐：统一使用 UTC 时区
fireAt := time.Date(2026, 2, 3, 10, 30, 0, 0, time.UTC)
msg.Header.Set("Nats-Schedule", "@at " + fireAt.Format(time.RFC3339))

// ⚠️ 注意：本地时区需要转换为 UTC
localTime := time.Date(2026, 2, 3, 18, 30, 0, 0, time.Local)
utcTime := localTime.UTC()
msg.Header.Set("Nats-Schedule", "@at " + utcTime.Format(time.RFC3339))
```

### 2. 时间验证

```go
func scheduleMessage(js nats.JetStreamContext, fireAt time.Time, data []byte) error {
    // 验证时间不能是过去
    if fireAt.Before(time.Now()) {
        return errors.New("不能安排过去的时间")
    }
    
    // 验证时间不能太远（例如：不超过 1 年）
    if fireAt.After(time.Now().Add(365 * 24 * time.Hour)) {
        return errors.New("时间不能超过 1 年")
    }
    
    msg := &nats.Msg{
        Subject: "scheduled.task",
        Data:    data,
        Header:  nats.Header{},
    }
    msg.Header.Set("Nats-Schedule", "@at " + fireAt.UTC().Format(time.RFC3339))
    msg.Header.Set("Nats-Schedule-Target", "scheduled.task")
    
    return js.PublishMsg(msg)
}
```

### 3. 重复任务

```go
// 实现每天重复的任务
func scheduleRecurringTask(js nats.JetStreamContext) {
    js.Subscribe("tasks.daily", func(m *nats.Msg) {
        // 执行任务
        executeTask()
        
        // 安排明天的任务
        tomorrow := time.Now().UTC().Add(24 * time.Hour)
        nextRun := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 2, 0, 0, 0, time.UTC)
        
        msg := &nats.Msg{
            Subject: "tasks.daily",
            Data:    []byte("daily-task"),
            Header:  nats.Header{},
        }
        msg.Header.Set("Nats-Schedule", "@at " + nextRun.Format(time.RFC3339))
        msg.Header.Set("Nats-Schedule-Target", "tasks.daily")
        
        js.PublishMsg(msg)
        m.Ack()
    })
}
```

### 4. 错误处理

```go
func publishScheduled(js nats.JetStreamContext, subject string, fireAt time.Time, data []byte) error {
    msg := &nats.Msg{
        Subject: subject,
        Data:    data,
        Header:  nats.Header{},
    }
    msg.Header.Set("Nats-Schedule", "@at " + fireAt.UTC().Format(time.RFC3339))
    msg.Header.Set("Nats-Schedule-Target", subject)
    
    ack, err := js.PublishMsg(msg)
    if err != nil {
        return fmt.Errorf("发布定时消息失败: %w", err)
    }
    
    log.Printf("定时消息已发布: seq=%d, 投递时间=%s", ack.Sequence, fireAt.Format(time.RFC3339))
    return nil
}
```

## 常见问题

### Q1: 如何取消已安排的定时消息？

**答案：** 定时消息一旦发布，无法直接取消。

**解决方案：**
```go
// 方案 1：在消费时检查状态
js.Subscribe("scheduled.task", func(m *nats.Msg) {
    taskID := getTaskID(m.Data)
    task := getTask(taskID)
    
    if task.Cancelled {
        // 任务已取消，忽略
        m.Ack()
        return
    }
    
    // 执行任务
    executeTask(task)
    m.Ack()
})

// 方案 2：使用外部状态存储
type TaskScheduler struct {
    cancelledTasks map[string]bool
    mu             sync.RWMutex
}

func (s *TaskScheduler) CancelTask(taskID string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.cancelledTasks[taskID] = true
}

func (s *TaskScheduler) IsTaskCancelled(taskID string) bool {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.cancelledTasks[taskID]
}
```

### Q2: 如何实现 cron 表达式风格的定时任务？

**答案：** NATS 不直接支持 cron 表达式，需要在应用层实现。

```go
import "github.com/robfig/cron/v3"

func setupCronScheduler(js nats.JetStreamContext) {
    c := cron.New()
    
    // 每天凌晨 2 点执行
    c.AddFunc("0 2 * * *", func() {
        publishTask(js, "daily-report")
    })
    
    // 每小时执行
    c.AddFunc("0 * * * *", func() {
        publishTask(js, "hourly-check")
    })
    
    c.Start()
}

func publishTask(js nats.JetStreamContext, taskType string) {
    js.Publish("tasks.scheduled", []byte(taskType))
}
```

### Q3: 定时消息的精度如何？

**答案：** 精度约为 1 秒。NATS 调度器每秒检查一次到期消息。

### Q4: 可以安排多远未来的消息？

**答案：** 理论上没有限制，但建议不超过 1 年。

**原因：**
- 长时间存储占用空间
- 业务需求可能变化
- 系统升级可能影响

### Q5: 如何处理夏令时？

**答案：** 使用 UTC 时区避免夏令时问题。

```go
// ✅ 推荐：使用 UTC
fireAt := time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC)

// ❌ 避免：使用本地时区（可能受夏令时影响）
fireAt := time.Date(2026, 3, 10, 10, 0, 0, 0, time.Local)
```

## 性能考虑

### 定时消息数量

```go
// 建议的定时消息数量：
// - 1 小时内：< 10,000 条
// - 1 天内：< 100,000 条
// - 1 周内：< 500,000 条
// - 1 月内：< 1,000,000 条
```

### 调度器性能

```go
// 调度器性能指标：
// - 检查频率：每秒 1 次
// - 检查速度：~100,000 条/秒
// - 投递速度：~10,000 条/秒
// - 内存占用：~1KB/1000 条定时消息
```

## 实战示例

### 示例 1：会议提醒系统

```go
type MeetingScheduler struct {
    js nats.JetStreamContext
}

func (s *MeetingScheduler) ScheduleMeeting(meeting Meeting) error {
    // 1. 保存会议信息
    if err := saveMeeting(meeting); err != nil {
        return err
    }
    
    // 2. 安排提前 1 小时提醒
    reminderTime := meeting.StartTime.Add(-1 * time.Hour)
    if err := s.scheduleReminder(meeting, reminderTime, "1小时后开始"); err != nil {
        return err
    }
    
    // 3. 安排提前 10 分钟提醒
    reminderTime = meeting.StartTime.Add(-10 * time.Minute)
    if err := s.scheduleReminder(meeting, reminderTime, "10分钟后开始"); err != nil {
        return err
    }
    
    // 4. 安排会议开始通知
    if err := s.scheduleStart(meeting); err != nil {
        return err
    }
    
    return nil
}

func (s *MeetingScheduler) scheduleReminder(meeting Meeting, fireAt time.Time, message string) error {
    data := map[string]interface{}{
        "meeting_id": meeting.ID,
        "message":    message,
    }
    
    msg := &nats.Msg{
        Subject: "meetings.reminder",
        Data:    marshal(data),
        Header:  nats.Header{},
    }
    msg.Header.Set("Nats-Schedule", "@at " + fireAt.UTC().Format(time.RFC3339))
    msg.Header.Set("Nats-Schedule-Target", "meetings.reminder")
    
    _, err := s.js.PublishMsg(msg)
    return err
}

func (s *MeetingScheduler) scheduleStart(meeting Meeting) error {
    msg := &nats.Msg{
        Subject: "meetings.start",
        Data:    marshal(meeting),
        Header:  nats.Header{},
    }
    msg.Header.Set("Nats-Schedule", "@at " + meeting.StartTime.UTC().Format(time.RFC3339))
    msg.Header.Set("Nats-Schedule-Target", "meetings.start")
    
    _, err := s.js.PublishMsg(msg)
    return err
}
```

### 示例 2：定时报告系统

```go
type ReportScheduler struct {
    js nats.JetStreamContext
}

func (s *ReportScheduler) ScheduleDailyReport() error {
    // 每天凌晨 2 点生成报告
    now := time.Now().UTC()
    nextRun := time.Date(now.Year(), now.Month(), now.Day()+1, 2, 0, 0, 0, time.UTC)
    
    msg := &nats.Msg{
        Subject: "reports.daily",
        Data:    []byte(now.Format("2006-01-02")),
        Header:  nats.Header{},
    }
    msg.Header.Set("Nats-Schedule", "@at " + nextRun.Format(time.RFC3339))
    msg.Header.Set("Nats-Schedule-Target", "reports.daily")
    
    _, err := s.js.PublishMsg(msg)
    return err
}

func (s *ReportScheduler) HandleDailyReport() {
    s.js.Subscribe("reports.daily", func(m *nats.Msg) {
        date := string(m.Data)
        log.Printf("生成 %s 的日报", date)
        
        // 生成报告
        generateReport(date)
        
        // 安排明天的报告
        s.ScheduleDailyReport()
        
        m.Ack()
    })
}
```

## 相关示例

- **05_delay**: 延迟消息
- **07_dlq**: 死信队列
- **08_sync_async**: 同步/异步发布

## 总结

本示例展示了 NATS JetStream 的定时消息功能：

✅ **绝对时间**：使用 `@at` 格式指定具体时间点  
✅ **灵活调度**：支持任意未来时间点  
✅ **持久化存储**：服务器重启后定时消息不丢失  
✅ **应用场景**：定时报告、预约提醒、活动通知等  
⚠️ **版本要求**：需要 NATS 2.11+ 版本  
⚠️ **精度限制**：约 1 秒精度  
⚠️ **无法取消**：消息发布后无法直接取消  

通过定时消息功能，可以轻松实现各种需要在特定时间点执行的业务场景。
