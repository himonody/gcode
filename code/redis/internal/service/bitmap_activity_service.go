package service

import (
	"context"
	"fmt"
	"time"

	"gcode/redis/client"
	rediserr "gcode/redis/pkg/errors"
	"gcode/redis/pkg/logger"
	"gcode/redis/pkg/metrics"
)

// UserActivityService 用户活动服务接口
// 基于 Redis Bitmap 实现的用户活动记录
// 适用场景：
// - 用户行为追踪
// - DAU/MAU 统计
// - 活跃度分析
// - 留存率计算
// - 用户画像构建
type UserActivityService interface {
	// RecordActivity 记录用户活动
	// activityType: 活动类型，如 "login", "purchase", "view"
	RecordActivity(ctx context.Context, activityType string, userID int64, date time.Time) error

	// IsActive 检查用户是否有活动记录
	IsActive(ctx context.Context, activityType string, userID int64, date time.Time) (bool, error)

	// GetActivityCount 获取活跃用户数
	// 统计指定日期有活动的用户总数
	GetActivityCount(ctx context.Context, activityType string, date time.Time) (int64, error)

	// GetDAU 获取日活跃用户数
	GetDAU(ctx context.Context, activityType string, date time.Time) (int64, error)

	// GetMAU 获取月活跃用户数
	// 统计整个月份的活跃用户数
	GetMAU(ctx context.Context, activityType string, year int, month int) (int64, error)

	// GetContinuousActiveDays 获取连续活跃天数
	// 从指定日期往前计算连续活跃的天数
	GetContinuousActiveDays(ctx context.Context, activityType string, userID int64, endDate time.Time) (int, error)

	// GetActiveUsersInRange 获取时间范围内的活跃用户数
	// 计算在指定时间范围内至少活跃一次的用户数
	GetActiveUsersInRange(ctx context.Context, activityType string, startDate, endDate time.Time) (int64, error)

	// GetRetentionRate 获取留存率
	// 计算在 baseDate 活跃的用户中，在 targetDate 仍然活跃的比例
	GetRetentionRate(ctx context.Context, activityType string, baseDate, targetDate time.Time) (float64, error)

	// BatchRecordActivity 批量记录用户活动
	BatchRecordActivity(ctx context.Context, activityType string, userIDs []int64, date time.Time) error

	// GetActivityDays 获取用户在指定时间范围内的活跃天数
	GetActivityDays(ctx context.Context, activityType string, userID int64, startDate, endDate time.Time) (int, error)

	// CompareActivity 比较两个日期的活跃用户
	// 返回：仅在date1活跃、仅在date2活跃、两天都活跃的用户数
	CompareActivity(ctx context.Context, activityType string, date1, date2 time.Time) (onlyDate1, onlyDate2, both int64, err error)

	// ClearActivity 清除指定日期的活动记录
	ClearActivity(ctx context.Context, activityType string, date time.Time) error
}

type userActivityService struct {
	client  client.Client
	logger  logger.Logger
	metrics metrics.Metrics
	prefix  string
}

// NewUserActivityService 创建用户活动服务
func NewUserActivityService(client client.Client, log logger.Logger, m metrics.Metrics, prefix string) UserActivityService {
	if prefix == "" {
		prefix = "activity"
	}
	return &userActivityService{
		client:  client,
		logger:  log,
		metrics: m,
		prefix:  prefix,
	}
}

// buildKey 构建活动记录键
// 格式: activity:{type}:{date}
func (s *userActivityService) buildKey(activityType string, date time.Time) string {
	dateStr := date.Format("2006-01-02")
	return fmt.Sprintf("%s:%s:%s", s.prefix, activityType, dateStr)
}

func (s *userActivityService) RecordActivity(ctx context.Context, activityType string, userID int64, date time.Time) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("activity.record", time.Since(start), true)
	}()

	if activityType == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "activity type cannot be empty")
	}

	if userID < 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID must be non-negative")
	}

	key := s.buildKey(activityType, date)

	err := s.client.GetClient().SetBit(ctx, key, userID, 1).Err()
	if err != nil {
		s.logger.Error("记录用户活动失败",
			logger.String("activity_type", activityType),
			logger.Int64("user_id", userID),
			logger.String("date", date.Format("2006-01-02")),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "record activity failed")
	}

	s.logger.Debug("记录用户活动",
		logger.String("activity_type", activityType),
		logger.Int64("user_id", userID),
		logger.String("date", date.Format("2006-01-02")))

	return nil
}

func (s *userActivityService) IsActive(ctx context.Context, activityType string, userID int64, date time.Time) (bool, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("activity.is_active", time.Since(start), true)
	}()

	if activityType == "" {
		return false, rediserr.New(rediserr.ErrCodeInvalidInput, "activity type cannot be empty")
	}

	if userID < 0 {
		return false, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID must be non-negative")
	}

	key := s.buildKey(activityType, date)

	bit, err := s.client.GetClient().GetBit(ctx, key, userID).Result()
	if err != nil {
		return false, rediserr.Wrap(err, rediserr.ErrCodeInternal, "is active failed")
	}

	return bit == 1, nil
}

func (s *userActivityService) GetActivityCount(ctx context.Context, activityType string, date time.Time) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("activity.get_count", time.Since(start), true)
	}()

	if activityType == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "activity type cannot be empty")
	}

	key := s.buildKey(activityType, date)

	count, err := s.client.GetClient().BitCount(ctx, key, nil).Result()
	if err != nil {
		s.logger.Error("获取活跃用户数失败",
			logger.String("activity_type", activityType),
			logger.String("date", date.Format("2006-01-02")),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get activity count failed")
	}

	return count, nil
}

func (s *userActivityService) GetDAU(ctx context.Context, activityType string, date time.Time) (int64, error) {
	return s.GetActivityCount(ctx, activityType, date)
}

func (s *userActivityService) GetMAU(ctx context.Context, activityType string, year int, month int) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("activity.get_mau", time.Since(start), true)
	}()

	if activityType == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "activity type cannot be empty")
	}

	// 获取该月的第一天和最后一天
	firstDay := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.Local)
	lastDay := firstDay.AddDate(0, 1, -1)

	// 使用 BITOP OR 合并整个月的数据
	keys := make([]string, 0)
	for d := firstDay; !d.After(lastDay); d = d.AddDate(0, 0, 1) {
		keys = append(keys, s.buildKey(activityType, d))
	}

	if len(keys) == 0 {
		return 0, nil
	}

	// 创建临时键存储结果
	destKey := fmt.Sprintf("%s:temp:mau:%s:%d-%02d", s.prefix, activityType, year, month)

	err := s.client.GetClient().BitOpOr(ctx, destKey, keys...).Err()
	if err != nil {
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "bitop or failed")
	}

	// 统计活跃用户数
	count, err := s.client.GetClient().BitCount(ctx, destKey, nil).Result()
	if err != nil {
		s.client.GetClient().Del(ctx, destKey)
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "bitcount failed")
	}

	// 删除临时键
	s.client.GetClient().Del(ctx, destKey)

	s.logger.Info("获取月活跃用户数",
		logger.String("activity_type", activityType),
		logger.Int("year", year),
		logger.Int("month", month),
		logger.Int64("mau", count))

	return count, nil
}

func (s *userActivityService) GetContinuousActiveDays(ctx context.Context, activityType string, userID int64, endDate time.Time) (int, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("activity.get_continuous_days", time.Since(start), true)
	}()

	if activityType == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "activity type cannot be empty")
	}

	if userID < 0 {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID must be non-negative")
	}

	continuousDays := 0
	currentDate := endDate

	// 从指定日期往前检查
	for i := 0; i < 365; i++ { // 最多检查一年
		isActive, err := s.IsActive(ctx, activityType, userID, currentDate)
		if err != nil {
			return 0, err
		}

		if !isActive {
			break
		}

		continuousDays++
		currentDate = currentDate.AddDate(0, 0, -1)
	}

	return continuousDays, nil
}

func (s *userActivityService) GetActiveUsersInRange(ctx context.Context, activityType string, startDate, endDate time.Time) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("activity.get_active_users_in_range", time.Since(start), true)
	}()

	if activityType == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "activity type cannot be empty")
	}

	if endDate.Before(startDate) {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "end date must be after start date")
	}

	// 收集时间范围内所有的键
	keys := make([]string, 0)
	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		keys = append(keys, s.buildKey(activityType, d))
	}

	if len(keys) == 0 {
		return 0, nil
	}

	// 使用 BITOP OR 合并
	destKey := fmt.Sprintf("%s:temp:range:%d", s.prefix, time.Now().UnixNano())

	err := s.client.GetClient().BitOpOr(ctx, destKey, keys...).Err()
	if err != nil {
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "bitop or failed")
	}

	count, err := s.client.GetClient().BitCount(ctx, destKey, nil).Result()
	if err != nil {
		s.client.GetClient().Del(ctx, destKey)
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "bitcount failed")
	}

	s.client.GetClient().Del(ctx, destKey)

	return count, nil
}

func (s *userActivityService) GetRetentionRate(ctx context.Context, activityType string, baseDate, targetDate time.Time) (float64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("activity.get_retention_rate", time.Since(start), true)
	}()

	if activityType == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "activity type cannot be empty")
	}

	baseKey := s.buildKey(activityType, baseDate)
	targetKey := s.buildKey(activityType, targetDate)

	// 获取基准日期的活跃用户数
	baseCount, err := s.client.GetClient().BitCount(ctx, baseKey, nil).Result()
	if err != nil {
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get base count failed")
	}

	if baseCount == 0 {
		return 0, nil
	}

	// 使用 BITOP AND 获取两天都活跃的用户
	destKey := fmt.Sprintf("%s:temp:retention:%d", s.prefix, time.Now().UnixNano())

	err = s.client.GetClient().BitOpAnd(ctx, destKey, baseKey, targetKey).Err()
	if err != nil {
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "bitop and failed")
	}

	retainedCount, err := s.client.GetClient().BitCount(ctx, destKey, nil).Result()
	if err != nil {
		s.client.GetClient().Del(ctx, destKey)
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "bitcount failed")
	}

	s.client.GetClient().Del(ctx, destKey)

	rate := float64(retainedCount) / float64(baseCount) * 100

	s.logger.Info("计算留存率",
		logger.String("activity_type", activityType),
		logger.String("base_date", baseDate.Format("2006-01-02")),
		logger.String("target_date", targetDate.Format("2006-01-02")),
		logger.Int64("base_count", baseCount),
		logger.Int64("retained_count", retainedCount),
		logger.Float64("rate", rate))

	return rate, nil
}

func (s *userActivityService) BatchRecordActivity(ctx context.Context, activityType string, userIDs []int64, date time.Time) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("activity.batch_record", time.Since(start), true)
	}()

	if activityType == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "activity type cannot be empty")
	}

	if len(userIDs) == 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user IDs cannot be empty")
	}

	key := s.buildKey(activityType, date)

	pipe := s.client.GetClient().Pipeline()
	for _, userID := range userIDs {
		if userID >= 0 {
			pipe.SetBit(ctx, key, userID, 1)
		}
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		s.logger.Error("批量记录用户活动失败",
			logger.String("activity_type", activityType),
			logger.Int("count", len(userIDs)),
			logger.String("date", date.Format("2006-01-02")),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "batch record activity failed")
	}

	s.logger.Info("批量记录用户活动",
		logger.String("activity_type", activityType),
		logger.Int("count", len(userIDs)),
		logger.String("date", date.Format("2006-01-02")))

	return nil
}

func (s *userActivityService) GetActivityDays(ctx context.Context, activityType string, userID int64, startDate, endDate time.Time) (int, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("activity.get_activity_days", time.Since(start), true)
	}()

	if activityType == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "activity type cannot be empty")
	}

	if userID < 0 {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID must be non-negative")
	}

	if endDate.Before(startDate) {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "end date must be after start date")
	}

	activeDays := 0
	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		isActive, err := s.IsActive(ctx, activityType, userID, d)
		if err != nil {
			return 0, err
		}
		if isActive {
			activeDays++
		}
	}

	return activeDays, nil
}

func (s *userActivityService) CompareActivity(ctx context.Context, activityType string, date1, date2 time.Time) (onlyDate1, onlyDate2, both int64, err error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("activity.compare", time.Since(start), true)
	}()

	if activityType == "" {
		return 0, 0, 0, rediserr.New(rediserr.ErrCodeInvalidInput, "activity type cannot be empty")
	}

	key1 := s.buildKey(activityType, date1)
	key2 := s.buildKey(activityType, date2)

	// 计算两天都活跃的用户数 (AND)
	bothKey := fmt.Sprintf("%s:temp:both:%d", s.prefix, time.Now().UnixNano())
	err = s.client.GetClient().BitOpAnd(ctx, bothKey, key1, key2).Err()
	if err != nil {
		return 0, 0, 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "bitop and failed")
	}
	both, _ = s.client.GetClient().BitCount(ctx, bothKey, nil).Result()
	s.client.GetClient().Del(ctx, bothKey)

	// 计算仅在date1活跃的用户数
	notKey2 := fmt.Sprintf("%s:temp:not2:%d", s.prefix, time.Now().UnixNano())
	err = s.client.GetClient().BitOpNot(ctx, notKey2, key2).Err()
	if err != nil {
		return 0, 0, 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "bitop not failed")
	}

	onlyDate1Key := fmt.Sprintf("%s:temp:only1:%d", s.prefix, time.Now().UnixNano())
	err = s.client.GetClient().BitOpAnd(ctx, onlyDate1Key, key1, notKey2).Err()
	if err != nil {
		s.client.GetClient().Del(ctx, notKey2)
		return 0, 0, 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "bitop and failed")
	}
	onlyDate1, _ = s.client.GetClient().BitCount(ctx, onlyDate1Key, nil).Result()
	s.client.GetClient().Del(ctx, notKey2, onlyDate1Key)

	// 计算仅在date2活跃的用户数
	notKey1 := fmt.Sprintf("%s:temp:not1:%d", s.prefix, time.Now().UnixNano())
	err = s.client.GetClient().BitOpNot(ctx, notKey1, key1).Err()
	if err != nil {
		return 0, 0, 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "bitop not failed")
	}

	onlyDate2Key := fmt.Sprintf("%s:temp:only2:%d", s.prefix, time.Now().UnixNano())
	err = s.client.GetClient().BitOpAnd(ctx, onlyDate2Key, key2, notKey1).Err()
	if err != nil {
		s.client.GetClient().Del(ctx, notKey1)
		return 0, 0, 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "bitop and failed")
	}
	onlyDate2, _ = s.client.GetClient().BitCount(ctx, onlyDate2Key, nil).Result()
	s.client.GetClient().Del(ctx, notKey1, onlyDate2Key)

	return onlyDate1, onlyDate2, both, nil
}

func (s *userActivityService) ClearActivity(ctx context.Context, activityType string, date time.Time) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("activity.clear", time.Since(start), true)
	}()

	if activityType == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "activity type cannot be empty")
	}

	key := s.buildKey(activityType, date)

	err := s.client.GetClient().Del(ctx, key).Err()
	if err != nil {
		s.logger.Error("清除活动记录失败",
			logger.String("activity_type", activityType),
			logger.String("date", date.Format("2006-01-02")),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "clear activity failed")
	}

	s.logger.Info("清除活动记录",
		logger.String("activity_type", activityType),
		logger.String("date", date.Format("2006-01-02")))

	return nil
}
