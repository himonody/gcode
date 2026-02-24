package service

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"gcode/redis/client"
	rediserr "gcode/redis/pkg/errors"
	"gcode/redis/pkg/logger"
	"gcode/redis/pkg/metrics"
)

// LotteryService 抽奖服务接口
// 基于 Redis Set 实现的抽奖功能
// 适用场景：
// - 活动抽奖
// - 随机推荐
// - A/B测试分组
// - 红包雨
// - 幸运用户选择
type LotteryService interface {
	// AddParticipant 添加参与者
	// 返回是否为新参与者
	AddParticipant(ctx context.Context, lotteryID string, userID string) (bool, error)

	// AddParticipants 批量添加参与者
	// 返回新增的参与者数量
	AddParticipants(ctx context.Context, lotteryID string, userIDs []string) (int64, error)

	// RemoveParticipant 移除参与者
	RemoveParticipant(ctx context.Context, lotteryID string, userID string) error

	// IsParticipating 检查是否参与
	IsParticipating(ctx context.Context, lotteryID string, userID string) (bool, error)

	// GetParticipantCount 获取参与人数
	GetParticipantCount(ctx context.Context, lotteryID string) (int64, error)

	// DrawWinner 抽取一个中奖者（不移除）
	// 可重复抽取
	DrawWinner(ctx context.Context, lotteryID string) (string, error)

	// DrawWinners 抽取多个中奖者（不移除）
	// count: 抽取数量
	// allowDuplicate: 是否允许重复中奖
	DrawWinners(ctx context.Context, lotteryID string, count int, allowDuplicate bool) ([]string, error)

	// DrawAndRemoveWinner 抽取并移除一个中奖者
	// 确保不会重复中奖
	DrawAndRemoveWinner(ctx context.Context, lotteryID string) (string, error)

	// DrawAndRemoveWinners 抽取并移除多个中奖者
	// 确保不会重复中奖
	DrawAndRemoveWinners(ctx context.Context, lotteryID string, count int) ([]string, error)

	// GetAllParticipants 获取所有参与者
	GetAllParticipants(ctx context.Context, lotteryID string) ([]string, error)

	// Clear 清空抽奖池
	Clear(ctx context.Context, lotteryID string) error

	// GetRemainingCount 获取剩余参与者数量
	GetRemainingCount(ctx context.Context, lotteryID string) (int64, error)

	// SaveWinners 保存中奖名单
	// 将中奖者保存到另一个集合
	SaveWinners(ctx context.Context, lotteryID string, winners []string) error

	// GetWinners 获取中奖名单
	GetWinners(ctx context.Context, lotteryID string) ([]string, error)

	// IsWinner 检查是否中奖
	IsWinner(ctx context.Context, lotteryID string, userID string) (bool, error)
}

type lotteryService struct {
	client  client.Client
	logger  logger.Logger
	metrics metrics.Metrics
	prefix  string
}

// NewLotteryService 创建抽奖服务
func NewLotteryService(client client.Client, log logger.Logger, m metrics.Metrics, prefix string) LotteryService {
	if prefix == "" {
		prefix = "lottery"
	}
	return &lotteryService{
		client:  client,
		logger:  log,
		metrics: m,
		prefix:  prefix,
	}
}

func (s *lotteryService) buildKey(lotteryID string) string {
	return fmt.Sprintf("%s:%s", s.prefix, lotteryID)
}

func (s *lotteryService) buildWinnersKey(lotteryID string) string {
	return fmt.Sprintf("%s:%s:winners", s.prefix, lotteryID)
}

func (s *lotteryService) AddParticipant(ctx context.Context, lotteryID string, userID string) (bool, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("lottery.add_participant", time.Since(start), true)
	}()

	if lotteryID == "" || userID == "" {
		return false, rediserr.New(rediserr.ErrCodeInvalidInput, "lottery ID and user ID cannot be empty")
	}

	key := s.buildKey(lotteryID)

	added, err := s.client.GetClient().SAdd(ctx, key, userID).Result()
	if err != nil {
		s.logger.Error("添加参与者失败",
			logger.String("lottery_id", lotteryID),
			logger.String("user_id", userID),
			logger.ErrorField(err))
		return false, rediserr.Wrap(err, rediserr.ErrCodeInternal, "add participant failed")
	}

	isNew := added > 0

	s.logger.Debug("添加参与者",
		logger.String("lottery_id", lotteryID),
		logger.String("user_id", userID),
		logger.Bool("is_new", isNew))

	return isNew, nil
}

func (s *lotteryService) AddParticipants(ctx context.Context, lotteryID string, userIDs []string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("lottery.add_participants", time.Since(start), true)
	}()

	if lotteryID == "" || len(userIDs) == 0 {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "lottery ID and user IDs cannot be empty")
	}

	key := s.buildKey(lotteryID)

	values := make([]interface{}, len(userIDs))
	for i, id := range userIDs {
		values[i] = id
	}

	added, err := s.client.GetClient().SAdd(ctx, key, values...).Result()
	if err != nil {
		s.logger.Error("批量添加参与者失败",
			logger.String("lottery_id", lotteryID),
			logger.Int("count", len(userIDs)),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "add participants failed")
	}

	s.logger.Info("批量添加参与者",
		logger.String("lottery_id", lotteryID),
		logger.Int("total", len(userIDs)),
		logger.Int64("added", added))

	return added, nil
}

func (s *lotteryService) RemoveParticipant(ctx context.Context, lotteryID string, userID string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("lottery.remove_participant", time.Since(start), true)
	}()

	if lotteryID == "" || userID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "lottery ID and user ID cannot be empty")
	}

	key := s.buildKey(lotteryID)

	err := s.client.GetClient().SRem(ctx, key, userID).Err()
	if err != nil {
		s.logger.Error("移除参与者失败",
			logger.String("lottery_id", lotteryID),
			logger.String("user_id", userID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "remove participant failed")
	}

	s.logger.Debug("移除参与者",
		logger.String("lottery_id", lotteryID),
		logger.String("user_id", userID))

	return nil
}

func (s *lotteryService) IsParticipating(ctx context.Context, lotteryID string, userID string) (bool, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("lottery.is_participating", time.Since(start), true)
	}()

	if lotteryID == "" || userID == "" {
		return false, rediserr.New(rediserr.ErrCodeInvalidInput, "lottery ID and user ID cannot be empty")
	}

	key := s.buildKey(lotteryID)

	exists, err := s.client.GetClient().SIsMember(ctx, key, userID).Result()
	if err != nil {
		s.logger.Error("检查参与状态失败",
			logger.String("lottery_id", lotteryID),
			logger.String("user_id", userID),
			logger.ErrorField(err))
		return false, rediserr.Wrap(err, rediserr.ErrCodeInternal, "is participating failed")
	}

	return exists, nil
}

func (s *lotteryService) GetParticipantCount(ctx context.Context, lotteryID string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("lottery.get_participant_count", time.Since(start), true)
	}()

	if lotteryID == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "lottery ID cannot be empty")
	}

	key := s.buildKey(lotteryID)

	count, err := s.client.GetClient().SCard(ctx, key).Result()
	if err != nil {
		s.logger.Error("获取参与人数失败",
			logger.String("lottery_id", lotteryID),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get count failed")
	}

	return count, nil
}

func (s *lotteryService) DrawWinner(ctx context.Context, lotteryID string) (string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("lottery.draw_winner", time.Since(start), true)
	}()

	if lotteryID == "" {
		return "", rediserr.New(rediserr.ErrCodeInvalidInput, "lottery ID cannot be empty")
	}

	key := s.buildKey(lotteryID)

	winner, err := s.client.GetClient().SRandMember(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		s.logger.Error("抽取中奖者失败",
			logger.String("lottery_id", lotteryID),
			logger.ErrorField(err))
		return "", rediserr.Wrap(err, rediserr.ErrCodeInternal, "draw winner failed")
	}

	s.logger.Info("抽取中奖者",
		logger.String("lottery_id", lotteryID),
		logger.String("winner", winner))

	return winner, nil
}

func (s *lotteryService) DrawWinners(ctx context.Context, lotteryID string, count int, allowDuplicate bool) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("lottery.draw_winners", time.Since(start), true)
	}()

	if lotteryID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "lottery ID cannot be empty")
	}

	if count <= 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "count must be positive")
	}

	key := s.buildKey(lotteryID)

	var winners []string
	var err error

	if allowDuplicate {
		// 允许重复，使用 SRandMemberN（可能返回重复）
		winners, err = s.client.GetClient().SRandMemberN(ctx, key, int64(count)).Result()
	} else {
		// 不允许重复，多次调用 SRandMember
		winnerSet := make(map[string]bool)
		maxAttempts := count * 10 // 防止无限循环

		for len(winnerSet) < count && maxAttempts > 0 {
			winner, err := s.client.GetClient().SRandMember(ctx, key).Result()
			if err == redis.Nil {
				break
			}
			if err != nil {
				return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "draw winners failed")
			}
			winnerSet[winner] = true
			maxAttempts--
		}

		winners = make([]string, 0, len(winnerSet))
		for winner := range winnerSet {
			winners = append(winners, winner)
		}
	}

	if err != nil {
		s.logger.Error("抽取多个中奖者失败",
			logger.String("lottery_id", lotteryID),
			logger.Int("count", count),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "draw winners failed")
	}

	s.logger.Info("抽取多个中奖者",
		logger.String("lottery_id", lotteryID),
		logger.Int("count", len(winners)))

	return winners, nil
}

func (s *lotteryService) DrawAndRemoveWinner(ctx context.Context, lotteryID string) (string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("lottery.draw_and_remove_winner", time.Since(start), true)
	}()

	if lotteryID == "" {
		return "", rediserr.New(rediserr.ErrCodeInvalidInput, "lottery ID cannot be empty")
	}

	key := s.buildKey(lotteryID)

	winner, err := s.client.GetClient().SPop(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		s.logger.Error("抽取并移除中奖者失败",
			logger.String("lottery_id", lotteryID),
			logger.ErrorField(err))
		return "", rediserr.Wrap(err, rediserr.ErrCodeInternal, "draw and remove winner failed")
	}

	s.logger.Info("抽取并移除中奖者",
		logger.String("lottery_id", lotteryID),
		logger.String("winner", winner))

	return winner, nil
}

func (s *lotteryService) DrawAndRemoveWinners(ctx context.Context, lotteryID string, count int) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("lottery.draw_and_remove_winners", time.Since(start), true)
	}()

	if lotteryID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "lottery ID cannot be empty")
	}

	if count <= 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "count must be positive")
	}

	key := s.buildKey(lotteryID)

	winners, err := s.client.GetClient().SPopN(ctx, key, int64(count)).Result()
	if err != nil {
		s.logger.Error("抽取并移除多个中奖者失败",
			logger.String("lottery_id", lotteryID),
			logger.Int("count", count),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "draw and remove winners failed")
	}

	s.logger.Info("抽取并移除多个中奖者",
		logger.String("lottery_id", lotteryID),
		logger.Int("count", len(winners)))

	return winners, nil
}

func (s *lotteryService) GetAllParticipants(ctx context.Context, lotteryID string) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("lottery.get_all_participants", time.Since(start), true)
	}()

	if lotteryID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "lottery ID cannot be empty")
	}

	key := s.buildKey(lotteryID)

	participants, err := s.client.GetClient().SMembers(ctx, key).Result()
	if err != nil {
		s.logger.Error("获取所有参与者失败",
			logger.String("lottery_id", lotteryID),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get all participants failed")
	}

	return participants, nil
}

func (s *lotteryService) Clear(ctx context.Context, lotteryID string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("lottery.clear", time.Since(start), true)
	}()

	if lotteryID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "lottery ID cannot be empty")
	}

	key := s.buildKey(lotteryID)

	err := s.client.GetClient().Del(ctx, key).Err()
	if err != nil {
		s.logger.Error("清空抽奖池失败",
			logger.String("lottery_id", lotteryID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "clear lottery failed")
	}

	s.logger.Info("清空抽奖池", logger.String("lottery_id", lotteryID))
	return nil
}

func (s *lotteryService) GetRemainingCount(ctx context.Context, lotteryID string) (int64, error) {
	return s.GetParticipantCount(ctx, lotteryID)
}

func (s *lotteryService) SaveWinners(ctx context.Context, lotteryID string, winners []string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("lottery.save_winners", time.Since(start), true)
	}()

	if lotteryID == "" || len(winners) == 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "lottery ID and winners cannot be empty")
	}

	key := s.buildWinnersKey(lotteryID)

	values := make([]interface{}, len(winners))
	for i, winner := range winners {
		values[i] = winner
	}

	err := s.client.GetClient().SAdd(ctx, key, values...).Err()
	if err != nil {
		s.logger.Error("保存中奖名单失败",
			logger.String("lottery_id", lotteryID),
			logger.Int("count", len(winners)),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "save winners failed")
	}

	s.logger.Info("保存中奖名单",
		logger.String("lottery_id", lotteryID),
		logger.Int("count", len(winners)))

	return nil
}

func (s *lotteryService) GetWinners(ctx context.Context, lotteryID string) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("lottery.get_winners", time.Since(start), true)
	}()

	if lotteryID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "lottery ID cannot be empty")
	}

	key := s.buildWinnersKey(lotteryID)

	winners, err := s.client.GetClient().SMembers(ctx, key).Result()
	if err != nil {
		s.logger.Error("获取中奖名单失败",
			logger.String("lottery_id", lotteryID),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get winners failed")
	}

	return winners, nil
}

func (s *lotteryService) IsWinner(ctx context.Context, lotteryID string, userID string) (bool, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("lottery.is_winner", time.Since(start), true)
	}()

	if lotteryID == "" || userID == "" {
		return false, rediserr.New(rediserr.ErrCodeInvalidInput, "lottery ID and user ID cannot be empty")
	}

	key := s.buildWinnersKey(lotteryID)

	exists, err := s.client.GetClient().SIsMember(ctx, key, userID).Result()
	if err != nil {
		s.logger.Error("检查中奖状态失败",
			logger.String("lottery_id", lotteryID),
			logger.String("user_id", userID),
			logger.ErrorField(err))
		return false, rediserr.Wrap(err, rediserr.ErrCodeInternal, "is winner failed")
	}

	return exists, nil
}
