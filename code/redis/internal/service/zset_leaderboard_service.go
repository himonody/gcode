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

// PlayerScore 玩家分数结构
type PlayerScore struct {
	PlayerID string                 `json:"player_id"`
	Score    float64                `json:"score"`
	Rank     int64                  `json:"rank"`
	Extra    map[string]interface{} `json:"extra,omitempty"`
}

// LeaderboardService 排行榜服务接口
// 基于 Redis ZSet 实现的排行榜功能
// 适用场景：
// - 游戏排行榜
// - 销量排行
// - 热度排行
// - 积分榜
// - 贡献榜
type LeaderboardService interface {
	// AddScore 添加或更新玩家分数
	// 如果玩家已存在，覆盖分数
	AddScore(ctx context.Context, boardName string, playerID string, score float64) error

	// IncrScore 增加玩家分数
	// delta: 可以为负数（减少分数）
	IncrScore(ctx context.Context, boardName string, playerID string, delta float64) (float64, error)

	// GetScore 获取玩家分数
	GetScore(ctx context.Context, boardName string, playerID string) (float64, error)

	// GetRank 获取玩家排名
	// 返回排名（从1开始，1表示第一名）
	GetRank(ctx context.Context, boardName string, playerID string) (int64, error)

	// GetTopN 获取前N名玩家
	// 返回按分数降序排列的玩家列表
	GetTopN(ctx context.Context, boardName string, n int) ([]*PlayerScore, error)

	// GetRange 获取指定排名范围的玩家
	// start, end: 排名范围（从0开始）
	GetRange(ctx context.Context, boardName string, start, end int64) ([]*PlayerScore, error)

	// GetRangeByScore 获取指定分数范围的玩家
	// min, max: 分数范围
	GetRangeByScore(ctx context.Context, boardName string, min, max float64) ([]*PlayerScore, error)

	// GetCount 获取排行榜总人数
	GetCount(ctx context.Context, boardName string) (int64, error)

	// GetCountByScore 获取指定分数范围的人数
	GetCountByScore(ctx context.Context, boardName string, min, max float64) (int64, error)

	// Remove 移除玩家
	Remove(ctx context.Context, boardName string, playerID string) error

	// RemoveByRank 移除指定排名范围的玩家
	// start, end: 排名范围（从0开始）
	RemoveByRank(ctx context.Context, boardName string, start, end int64) error

	// RemoveByScore 移除指定分数范围的玩家
	RemoveByScore(ctx context.Context, boardName string, min, max float64) error

	// Clear 清空排行榜
	Clear(ctx context.Context, boardName string) error

	// GetPlayerWithRank 获取玩家信息及排名
	GetPlayerWithRank(ctx context.Context, boardName string, playerID string) (*PlayerScore, error)

	// GetAroundPlayers 获取玩家周围的排名
	// count: 上下各获取多少名
	GetAroundPlayers(ctx context.Context, boardName string, playerID string, count int) ([]*PlayerScore, error)

	// BatchAddScores 批量添加分数
	BatchAddScores(ctx context.Context, boardName string, scores map[string]float64) error
}

type leaderboardService struct {
	client  client.Client
	logger  logger.Logger
	metrics metrics.Metrics
	prefix  string
}

// NewLeaderboardService 创建排行榜服务
func NewLeaderboardService(client client.Client, log logger.Logger, m metrics.Metrics, prefix string) LeaderboardService {
	if prefix == "" {
		prefix = "leaderboard"
	}
	return &leaderboardService{
		client:  client,
		logger:  log,
		metrics: m,
		prefix:  prefix,
	}
}

func (s *leaderboardService) buildKey(boardName string) string {
	return fmt.Sprintf("%s:%s", s.prefix, boardName)
}

func (s *leaderboardService) AddScore(ctx context.Context, boardName string, playerID string, score float64) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("leaderboard.add_score", time.Since(start), true)
	}()

	if boardName == "" || playerID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "board name and player ID cannot be empty")
	}

	key := s.buildKey(boardName)

	err := s.client.GetClient().ZAdd(ctx, key, redis.Z{
		Score:  score,
		Member: playerID,
	}).Err()

	if err != nil {
		s.logger.Error("添加分数失败",
			logger.String("board", boardName),
			logger.String("player_id", playerID),
			logger.Float64("score", score),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "add score failed")
	}

	s.logger.Debug("添加分数成功",
		logger.String("board", boardName),
		logger.String("player_id", playerID),
		logger.Float64("score", score))

	return nil
}

func (s *leaderboardService) IncrScore(ctx context.Context, boardName string, playerID string, delta float64) (float64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("leaderboard.incr_score", time.Since(start), true)
	}()

	if boardName == "" || playerID == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "board name and player ID cannot be empty")
	}

	key := s.buildKey(boardName)

	newScore, err := s.client.GetClient().ZIncrBy(ctx, key, delta, playerID).Result()
	if err != nil {
		s.logger.Error("增加分数失败",
			logger.String("board", boardName),
			logger.String("player_id", playerID),
			logger.Float64("delta", delta),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "incr score failed")
	}

	s.logger.Info("分数变更",
		logger.String("board", boardName),
		logger.String("player_id", playerID),
		logger.Float64("delta", delta),
		logger.Float64("new_score", newScore))

	return newScore, nil
}

func (s *leaderboardService) GetScore(ctx context.Context, boardName string, playerID string) (float64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("leaderboard.get_score", time.Since(start), true)
	}()

	if boardName == "" || playerID == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "board name and player ID cannot be empty")
	}

	key := s.buildKey(boardName)

	score, err := s.client.GetClient().ZScore(ctx, key, playerID).Result()
	if err == redis.Nil {
		return 0, rediserr.New(rediserr.ErrCodeNotFound, "player not found")
	}
	if err != nil {
		s.logger.Error("获取分数失败",
			logger.String("board", boardName),
			logger.String("player_id", playerID),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get score failed")
	}

	return score, nil
}

func (s *leaderboardService) GetRank(ctx context.Context, boardName string, playerID string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("leaderboard.get_rank", time.Since(start), true)
	}()

	if boardName == "" || playerID == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "board name and player ID cannot be empty")
	}

	key := s.buildKey(boardName)

	// ZRevRank 返回从0开始的排名，需要+1
	rank, err := s.client.GetClient().ZRevRank(ctx, key, playerID).Result()
	if err == redis.Nil {
		return 0, rediserr.New(rediserr.ErrCodeNotFound, "player not found")
	}
	if err != nil {
		s.logger.Error("获取排名失败",
			logger.String("board", boardName),
			logger.String("player_id", playerID),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get rank failed")
	}

	return rank + 1, nil
}

func (s *leaderboardService) GetTopN(ctx context.Context, boardName string, n int) ([]*PlayerScore, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("leaderboard.get_top_n", time.Since(start), true)
	}()

	if boardName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "board name cannot be empty")
	}

	if n <= 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "n must be positive")
	}

	key := s.buildKey(boardName)

	// 获取前N名（降序）
	results, err := s.client.GetClient().ZRevRangeWithScores(ctx, key, 0, int64(n-1)).Result()
	if err != nil {
		s.logger.Error("获取Top N失败",
			logger.String("board", boardName),
			logger.Int("n", n),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get top n failed")
	}

	players := make([]*PlayerScore, len(results))
	for i, z := range results {
		players[i] = &PlayerScore{
			PlayerID: z.Member.(string),
			Score:    z.Score,
			Rank:     int64(i + 1),
		}
	}

	return players, nil
}

func (s *leaderboardService) GetRange(ctx context.Context, boardName string, start, end int64) ([]*PlayerScore, error) {
	startTime := time.Now()
	defer func() {
		s.metrics.RecordOperation("leaderboard.get_range", time.Since(startTime), true)
	}()

	if boardName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "board name cannot be empty")
	}

	key := s.buildKey(boardName)

	results, err := s.client.GetClient().ZRevRangeWithScores(ctx, key, start, end).Result()
	if err != nil {
		s.logger.Error("获取排名范围失败",
			logger.String("board", boardName),
			logger.Int64("start", start),
			logger.Int64("end", end),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get range failed")
	}

	players := make([]*PlayerScore, len(results))
	for i, z := range results {
		players[i] = &PlayerScore{
			PlayerID: z.Member.(string),
			Score:    z.Score,
			Rank:     start + int64(i) + 1,
		}
	}

	return players, nil
}

func (s *leaderboardService) GetRangeByScore(ctx context.Context, boardName string, min, max float64) ([]*PlayerScore, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("leaderboard.get_range_by_score", time.Since(start), true)
	}()

	if boardName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "board name cannot be empty")
	}

	key := s.buildKey(boardName)

	results, err := s.client.GetClient().ZRevRangeByScoreWithScores(ctx, key, &redis.ZRangeBy{
		Min: fmt.Sprintf("%f", min),
		Max: fmt.Sprintf("%f", max),
	}).Result()

	if err != nil {
		s.logger.Error("获取分数范围失败",
			logger.String("board", boardName),
			logger.Float64("min", min),
			logger.Float64("max", max),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get range by score failed")
	}

	players := make([]*PlayerScore, len(results))
	for i, z := range results {
		players[i] = &PlayerScore{
			PlayerID: z.Member.(string),
			Score:    z.Score,
		}
	}

	return players, nil
}

func (s *leaderboardService) GetCount(ctx context.Context, boardName string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("leaderboard.get_count", time.Since(start), true)
	}()

	if boardName == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "board name cannot be empty")
	}

	key := s.buildKey(boardName)

	count, err := s.client.GetClient().ZCard(ctx, key).Result()
	if err != nil {
		s.logger.Error("获取总人数失败",
			logger.String("board", boardName),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get count failed")
	}

	return count, nil
}

func (s *leaderboardService) GetCountByScore(ctx context.Context, boardName string, min, max float64) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("leaderboard.get_count_by_score", time.Since(start), true)
	}()

	if boardName == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "board name cannot be empty")
	}

	key := s.buildKey(boardName)

	count, err := s.client.GetClient().ZCount(ctx, key, fmt.Sprintf("%f", min), fmt.Sprintf("%f", max)).Result()
	if err != nil {
		s.logger.Error("获取分数范围人数失败",
			logger.String("board", boardName),
			logger.Float64("min", min),
			logger.Float64("max", max),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get count by score failed")
	}

	return count, nil
}

func (s *leaderboardService) Remove(ctx context.Context, boardName string, playerID string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("leaderboard.remove", time.Since(start), true)
	}()

	if boardName == "" || playerID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "board name and player ID cannot be empty")
	}

	key := s.buildKey(boardName)

	err := s.client.GetClient().ZRem(ctx, key, playerID).Err()
	if err != nil {
		s.logger.Error("移除玩家失败",
			logger.String("board", boardName),
			logger.String("player_id", playerID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "remove player failed")
	}

	s.logger.Info("移除玩家成功",
		logger.String("board", boardName),
		logger.String("player_id", playerID))

	return nil
}

func (s *leaderboardService) RemoveByRank(ctx context.Context, boardName string, start, end int64) error {
	startTime := time.Now()
	defer func() {
		s.metrics.RecordOperation("leaderboard.remove_by_rank", time.Since(startTime), true)
	}()

	if boardName == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "board name cannot be empty")
	}

	key := s.buildKey(boardName)

	err := s.client.GetClient().ZRemRangeByRank(ctx, key, start, end).Err()
	if err != nil {
		s.logger.Error("按排名移除失败",
			logger.String("board", boardName),
			logger.Int64("start", start),
			logger.Int64("end", end),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "remove by rank failed")
	}

	s.logger.Info("按排名移除成功",
		logger.String("board", boardName),
		logger.Int64("start", start),
		logger.Int64("end", end))

	return nil
}

func (s *leaderboardService) RemoveByScore(ctx context.Context, boardName string, min, max float64) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("leaderboard.remove_by_score", time.Since(start), true)
	}()

	if boardName == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "board name cannot be empty")
	}

	key := s.buildKey(boardName)

	err := s.client.GetClient().ZRemRangeByScore(ctx, key, fmt.Sprintf("%f", min), fmt.Sprintf("%f", max)).Err()
	if err != nil {
		s.logger.Error("按分数移除失败",
			logger.String("board", boardName),
			logger.Float64("min", min),
			logger.Float64("max", max),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "remove by score failed")
	}

	s.logger.Info("按分数移除成功",
		logger.String("board", boardName),
		logger.Float64("min", min),
		logger.Float64("max", max))

	return nil
}

func (s *leaderboardService) Clear(ctx context.Context, boardName string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("leaderboard.clear", time.Since(start), true)
	}()

	if boardName == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "board name cannot be empty")
	}

	key := s.buildKey(boardName)

	err := s.client.GetClient().Del(ctx, key).Err()
	if err != nil {
		s.logger.Error("清空排行榜失败",
			logger.String("board", boardName),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "clear leaderboard failed")
	}

	s.logger.Info("清空排行榜成功", logger.String("board", boardName))
	return nil
}

func (s *leaderboardService) GetPlayerWithRank(ctx context.Context, boardName string, playerID string) (*PlayerScore, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("leaderboard.get_player_with_rank", time.Since(start), true)
	}()

	if boardName == "" || playerID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "board name and player ID cannot be empty")
	}

	// 获取分数和排名
	score, err := s.GetScore(ctx, boardName, playerID)
	if err != nil {
		return nil, err
	}

	rank, err := s.GetRank(ctx, boardName, playerID)
	if err != nil {
		return nil, err
	}

	return &PlayerScore{
		PlayerID: playerID,
		Score:    score,
		Rank:     rank,
	}, nil
}

func (s *leaderboardService) GetAroundPlayers(ctx context.Context, boardName string, playerID string, count int) ([]*PlayerScore, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("leaderboard.get_around_players", time.Since(start), true)
	}()

	if boardName == "" || playerID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "board name and player ID cannot be empty")
	}

	if count < 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "count cannot be negative")
	}

	// 获取玩家排名
	rank, err := s.GetRank(ctx, boardName, playerID)
	if err != nil {
		return nil, err
	}

	// 计算范围（排名从1开始，索引从0开始）
	startRank := rank - int64(count) - 1
	if startRank < 0 {
		startRank = 0
	}
	endRank := rank + int64(count) - 1

	return s.GetRange(ctx, boardName, startRank, endRank)
}

func (s *leaderboardService) BatchAddScores(ctx context.Context, boardName string, scores map[string]float64) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("leaderboard.batch_add_scores", time.Since(start), true)
	}()

	if boardName == "" || len(scores) == 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "board name and scores cannot be empty")
	}

	key := s.buildKey(boardName)

	// 构建 ZSet 成员
	members := make([]redis.Z, 0, len(scores))
	for playerID, score := range scores {
		members = append(members, redis.Z{
			Score:  score,
			Member: playerID,
		})
	}

	err := s.client.GetClient().ZAdd(ctx, key, members...).Err()
	if err != nil {
		s.logger.Error("批量添加分数失败",
			logger.String("board", boardName),
			logger.Int("count", len(scores)),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "batch add scores failed")
	}

	s.logger.Info("批量添加分数成功",
		logger.String("board", boardName),
		logger.Int("count", len(scores)))

	return nil
}
