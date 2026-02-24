package service

import (
	"context"
	"fmt"
	"time"

	"gcode/redis/client"
	rediserr "gcode/redis/pkg/errors"
	"gcode/redis/pkg/logger"
	"gcode/redis/pkg/metrics"
	"github.com/redis/go-redis/v9"
)

// OnlineStatusService 在线状态服务接口
// 基于 Redis Bitmap 实现的在线状态管理
// 适用场景：
// - 用户在线状态
// - 设备在线监控
// - 实时在线人数统计
// - 在线用户列表
// - 多端在线检测
type OnlineStatusService interface {
	// SetOnline 设置用户在线
	SetOnline(ctx context.Context, userID int64) error

	// SetOffline 设置用户离线
	SetOffline(ctx context.Context, userID int64) error

	// IsOnline 检查用户是否在线
	IsOnline(ctx context.Context, userID int64) (bool, error)

	// GetOnlineCount 获取在线用户总数
	GetOnlineCount(ctx context.Context) (int64, error)

	// BatchSetOnline 批量设置用户在线
	BatchSetOnline(ctx context.Context, userIDs []int64) error

	// BatchSetOffline 批量设置用户离线
	BatchSetOffline(ctx context.Context, userIDs []int64) error

	// BatchCheckOnline 批量检查用户在线状态
	BatchCheckOnline(ctx context.Context, userIDs []int64) (map[int64]bool, error)

	// GetBothOnlineCount 获取两组用户都在线的数量
	// 用于统计共同在线的用户
	GetBothOnlineCount(ctx context.Context, date1, date2 string) (int64, error)

	// GetEitherOnlineCount 获取任一组在线的用户数量
	GetEitherOnlineCount(ctx context.Context, date1, date2 string) (int64, error)

	// GetOnlyFirstOnlineCount 获取只在第一组在线的用户数量
	GetOnlyFirstOnlineCount(ctx context.Context, date1, date2 string) (int64, error)

	// ClearAll 清空所有在线状态
	ClearAll(ctx context.Context) error

	// SetOnlineWithExpire 设置在线状态并自动过期
	// 用于自动清理长时间未活跃的在线状态
	SetOnlineWithExpire(ctx context.Context, userID int64, ttl time.Duration) error

	// GetOnlineUsers 获取在线用户列表（适用于小规模场景）
	// maxUserID: 最大用户ID范围
	GetOnlineUsers(ctx context.Context, maxUserID int64) ([]int64, error)
}

type onlineStatusService struct {
	client  client.Client
	logger  logger.Logger
	metrics metrics.Metrics
	prefix  string
}

// NewOnlineStatusService 创建在线状态服务
func NewOnlineStatusService(client client.Client, log logger.Logger, m metrics.Metrics, prefix string) OnlineStatusService {
	if prefix == "" {
		prefix = "online"
	}
	return &onlineStatusService{
		client:  client,
		logger:  log,
		metrics: m,
		prefix:  prefix,
	}
}

// buildKey 构建在线状态键
// 使用日期作为键的一部分，便于按天统计
func (s *onlineStatusService) buildKey() string {
	today := time.Now().Format("2006-01-02")
	return fmt.Sprintf("%s:%s", s.prefix, today)
}

func (s *onlineStatusService) buildKeyWithDate(date string) string {
	return fmt.Sprintf("%s:%s", s.prefix, date)
}

func (s *onlineStatusService) SetOnline(ctx context.Context, userID int64) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("online.set_online", time.Since(start), true)
	}()

	if userID < 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID must be non-negative")
	}

	key := s.buildKey()

	err := s.client.GetClient().SetBit(ctx, key, userID, 1).Err()
	if err != nil {
		s.logger.Error("设置在线状态失败",
			logger.Int64("user_id", userID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "set online failed")
	}

	s.logger.Debug("设置用户在线",
		logger.Int64("user_id", userID))

	return nil
}

func (s *onlineStatusService) SetOffline(ctx context.Context, userID int64) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("online.set_offline", time.Since(start), true)
	}()

	if userID < 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID must be non-negative")
	}

	key := s.buildKey()

	err := s.client.GetClient().SetBit(ctx, key, userID, 0).Err()
	if err != nil {
		s.logger.Error("设置离线状态失败",
			logger.Int64("user_id", userID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "set offline failed")
	}

	s.logger.Debug("设置用户离线",
		logger.Int64("user_id", userID))

	return nil
}

func (s *onlineStatusService) IsOnline(ctx context.Context, userID int64) (bool, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("online.is_online", time.Since(start), true)
	}()

	if userID < 0 {
		return false, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID must be non-negative")
	}

	key := s.buildKey()

	bit, err := s.client.GetClient().GetBit(ctx, key, userID).Result()
	if err != nil {
		s.logger.Error("检查在线状态失败",
			logger.Int64("user_id", userID),
			logger.ErrorField(err))
		return false, rediserr.Wrap(err, rediserr.ErrCodeInternal, "is online failed")
	}

	return bit == 1, nil
}

func (s *onlineStatusService) GetOnlineCount(ctx context.Context) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("online.get_online_count", time.Since(start), true)
	}()

	key := s.buildKey()

	count, err := s.client.GetClient().BitCount(ctx, key, nil).Result()
	if err != nil {
		s.logger.Error("获取在线人数失败",
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get online count failed")
	}

	return count, nil
}

func (s *onlineStatusService) BatchSetOnline(ctx context.Context, userIDs []int64) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("online.batch_set_online", time.Since(start), true)
	}()

	if len(userIDs) == 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user IDs cannot be empty")
	}

	key := s.buildKey()

	pipe := s.client.GetClient().Pipeline()
	for _, userID := range userIDs {
		if userID >= 0 {
			pipe.SetBit(ctx, key, userID, 1)
		}
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		s.logger.Error("批量设置在线状态失败",
			logger.Int("count", len(userIDs)),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "batch set online failed")
	}

	s.logger.Info("批量设置用户在线",
		logger.Int("count", len(userIDs)))

	return nil
}

func (s *onlineStatusService) BatchSetOffline(ctx context.Context, userIDs []int64) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("online.batch_set_offline", time.Since(start), true)
	}()

	if len(userIDs) == 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user IDs cannot be empty")
	}

	key := s.buildKey()

	pipe := s.client.GetClient().Pipeline()
	for _, userID := range userIDs {
		if userID >= 0 {
			pipe.SetBit(ctx, key, userID, 0)
		}
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		s.logger.Error("批量设置离线状态失败",
			logger.Int("count", len(userIDs)),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "batch set offline failed")
	}

	s.logger.Info("批量设置用户离线",
		logger.Int("count", len(userIDs)))

	return nil
}

func (s *onlineStatusService) BatchCheckOnline(ctx context.Context, userIDs []int64) (map[int64]bool, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("online.batch_check_online", time.Since(start), true)
	}()

	if len(userIDs) == 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "user IDs cannot be empty")
	}

	key := s.buildKey()
	result := make(map[int64]bool, len(userIDs))

	pipe := s.client.GetClient().Pipeline()
	cmds := make(map[int64]*redis.IntCmd)

	for _, userID := range userIDs {
		if userID >= 0 {
			cmds[userID] = pipe.GetBit(ctx, key, userID)
		}
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		s.logger.Error("批量检查在线状态失败",
			logger.Int("count", len(userIDs)),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "batch check online failed")
	}

	for userID, cmd := range cmds {
		bit, _ := cmd.Result()
		result[userID] = bit == 1
	}

	return result, nil
}

func (s *onlineStatusService) GetBothOnlineCount(ctx context.Context, date1, date2 string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("online.get_both_online_count", time.Since(start), true)
	}()

	key1 := s.buildKeyWithDate(date1)
	key2 := s.buildKeyWithDate(date2)

	// 使用 BITOP AND 求交集
	destKey := fmt.Sprintf("%s:temp:and:%d", s.prefix, time.Now().UnixNano())

	err := s.client.GetClient().BitOpAnd(ctx, destKey, key1, key2).Err()
	if err != nil {
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "bitop and failed")
	}

	// 统计结果
	count, err := s.client.GetClient().BitCount(ctx, destKey, nil).Result()
	if err != nil {
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "bitcount failed")
	}

	// 删除临时键
	s.client.GetClient().Del(ctx, destKey)

	return count, nil
}

func (s *onlineStatusService) GetEitherOnlineCount(ctx context.Context, date1, date2 string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("online.get_either_online_count", time.Since(start), true)
	}()

	key1 := s.buildKeyWithDate(date1)
	key2 := s.buildKeyWithDate(date2)

	// 使用 BITOP OR 求并集
	destKey := fmt.Sprintf("%s:temp:or:%d", s.prefix, time.Now().UnixNano())

	err := s.client.GetClient().BitOpOr(ctx, destKey, key1, key2).Err()
	if err != nil {
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "bitop or failed")
	}

	count, err := s.client.GetClient().BitCount(ctx, destKey, nil).Result()
	if err != nil {
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "bitcount failed")
	}

	s.client.GetClient().Del(ctx, destKey)

	return count, nil
}

func (s *onlineStatusService) GetOnlyFirstOnlineCount(ctx context.Context, date1, date2 string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("online.get_only_first_online_count", time.Since(start), true)
	}()

	key1 := s.buildKeyWithDate(date1)
	key2 := s.buildKeyWithDate(date2)

	// 先对 key2 取反，再与 key1 做 AND
	destKey := fmt.Sprintf("%s:temp:not:%d", s.prefix, time.Now().UnixNano())

	err := s.client.GetClient().BitOpNot(ctx, destKey, key2).Err()
	if err != nil {
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "bitop not failed")
	}

	resultKey := fmt.Sprintf("%s:temp:result:%d", s.prefix, time.Now().UnixNano())
	err = s.client.GetClient().BitOpAnd(ctx, resultKey, key1, destKey).Err()
	if err != nil {
		s.client.GetClient().Del(ctx, destKey)
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "bitop and failed")
	}

	count, err := s.client.GetClient().BitCount(ctx, resultKey, nil).Result()
	if err != nil {
		s.client.GetClient().Del(ctx, destKey, resultKey)
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "bitcount failed")
	}

	s.client.GetClient().Del(ctx, destKey, resultKey)

	return count, nil
}

func (s *onlineStatusService) ClearAll(ctx context.Context) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("online.clear_all", time.Since(start), true)
	}()

	key := s.buildKey()

	err := s.client.GetClient().Del(ctx, key).Err()
	if err != nil {
		s.logger.Error("清空在线状态失败",
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "clear all failed")
	}

	s.logger.Info("清空所有在线状态")
	return nil
}

func (s *onlineStatusService) SetOnlineWithExpire(ctx context.Context, userID int64, ttl time.Duration) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("online.set_online_with_expire", time.Since(start), true)
	}()

	if userID < 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID must be non-negative")
	}

	if ttl <= 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "TTL must be positive")
	}

	key := s.buildKey()

	pipe := s.client.GetClient().Pipeline()
	pipe.SetBit(ctx, key, userID, 1)
	pipe.Expire(ctx, key, ttl)

	_, err := pipe.Exec(ctx)
	if err != nil {
		s.logger.Error("设置在线状态（带过期）失败",
			logger.Int64("user_id", userID),
			logger.Duration("ttl", ttl),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "set online with expire failed")
	}

	s.logger.Debug("设置用户在线（带过期）",
		logger.Int64("user_id", userID),
		logger.Duration("ttl", ttl))

	return nil
}

func (s *onlineStatusService) GetOnlineUsers(ctx context.Context, maxUserID int64) ([]int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("online.get_online_users", time.Since(start), true)
	}()

	if maxUserID < 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "max user ID must be non-negative")
	}

	// 注意：此方法仅适用于小规模场景
	// 对于大规模用户，建议使用其他数据结构
	if maxUserID > 100000 {
		s.logger.Warn("GetOnlineUsers 不适用于大规模用户场景",
			logger.Int64("max_user_id", maxUserID))
	}

	key := s.buildKey()
	onlineUsers := make([]int64, 0)

	// 批量检查
	batchSize := int64(1000)
	for offset := int64(0); offset <= maxUserID; offset += batchSize {
		end := offset + batchSize - 1
		if end > maxUserID {
			end = maxUserID
		}

		pipe := s.client.GetClient().Pipeline()
		cmds := make([]*redis.IntCmd, end-offset+1)

		for i := offset; i <= end; i++ {
			cmds[i-offset] = pipe.GetBit(ctx, key, i)
		}

		_, err := pipe.Exec(ctx)
		if err != nil {
			return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get online users failed")
		}

		for i, cmd := range cmds {
			bit, _ := cmd.Result()
			if bit == 1 {
				onlineUsers = append(onlineUsers, offset+int64(i))
			}
		}
	}

	return onlineUsers, nil
}
