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

// DeduplicationService 去重服务接口
// 基于 Redis Set 实现的去重功能
// 适用场景：
// - 独立访客统计（UV）
// - 文章浏览人数统计
// - 标签管理
// - 用户去重
// - 唯一性检查
type DeduplicationService interface {
	// Add 添加元素
	// 返回是否为新元素（true表示新增，false表示已存在）
	Add(ctx context.Context, setName string, member string) (bool, error)

	// AddMultiple 批量添加元素
	// 返回新增的元素数量
	AddMultiple(ctx context.Context, setName string, members ...string) (int64, error)

	// IsMember 检查元素是否存在
	IsMember(ctx context.Context, setName string, member string) (bool, error)

	// Remove 移除元素
	Remove(ctx context.Context, setName string, member string) error

	// RemoveMultiple 批量移除元素
	RemoveMultiple(ctx context.Context, setName string, members ...string) error

	// GetAll 获取所有元素
	GetAll(ctx context.Context, setName string) ([]string, error)

	// Count 获取元素数量
	Count(ctx context.Context, setName string) (int64, error)

	// Clear 清空集合
	Clear(ctx context.Context, setName string) error

	// RandomMember 随机获取一个元素（不删除）
	RandomMember(ctx context.Context, setName string) (string, error)

	// RandomMembers 随机获取多个元素（不删除）
	// count: 获取数量
	RandomMembers(ctx context.Context, setName string, count int) ([]string, error)

	// Pop 随机弹出一个元素（删除）
	Pop(ctx context.Context, setName string) (string, error)

	// PopMultiple 随机弹出多个元素（删除）
	PopMultiple(ctx context.Context, setName string, count int) ([]string, error)

	// Union 求并集
	// 返回多个集合的并集
	Union(ctx context.Context, setNames ...string) ([]string, error)

	// Intersect 求交集
	// 返回多个集合的交集
	Intersect(ctx context.Context, setNames ...string) ([]string, error)

	// Diff 求差集
	// 返回第一个集合与其他集合的差集
	Diff(ctx context.Context, setNames ...string) ([]string, error)

	// Move 移动元素
	// 将元素从一个集合移动到另一个集合
	Move(ctx context.Context, fromSet, toSet, member string) error
}

type deduplicationService struct {
	client  client.Client
	logger  logger.Logger
	metrics metrics.Metrics
	prefix  string
}

// NewDeduplicationService 创建去重服务
func NewDeduplicationService(client client.Client, log logger.Logger, m metrics.Metrics, prefix string) DeduplicationService {
	if prefix == "" {
		prefix = "dedup"
	}
	return &deduplicationService{
		client:  client,
		logger:  log,
		metrics: m,
		prefix:  prefix,
	}
}

func (s *deduplicationService) buildKey(setName string) string {
	return fmt.Sprintf("%s:%s", s.prefix, setName)
}

func (s *deduplicationService) Add(ctx context.Context, setName string, member string) (bool, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("dedup.add", time.Since(start), true)
	}()

	if setName == "" || member == "" {
		return false, rediserr.New(rediserr.ErrCodeInvalidInput, "set name and member cannot be empty")
	}

	key := s.buildKey(setName)

	added, err := s.client.GetClient().SAdd(ctx, key, member).Result()
	if err != nil {
		s.logger.Error("添加元素失败",
			logger.String("set", setName),
			logger.String("member", member),
			logger.ErrorField(err))
		return false, rediserr.Wrap(err, rediserr.ErrCodeInternal, "add member failed")
	}

	isNew := added > 0

	s.logger.Debug("添加元素",
		logger.String("set", setName),
		logger.String("member", member),
		logger.Bool("is_new", isNew))

	return isNew, nil
}

func (s *deduplicationService) AddMultiple(ctx context.Context, setName string, members ...string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("dedup.add_multiple", time.Since(start), true)
	}()

	if setName == "" || len(members) == 0 {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "set name and members cannot be empty")
	}

	key := s.buildKey(setName)

	// 转换为 interface{} 切片
	values := make([]interface{}, len(members))
	for i, m := range members {
		values[i] = m
	}

	added, err := s.client.GetClient().SAdd(ctx, key, values...).Result()
	if err != nil {
		s.logger.Error("批量添加元素失败",
			logger.String("set", setName),
			logger.Int("count", len(members)),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "add multiple failed")
	}

	s.logger.Debug("批量添加元素",
		logger.String("set", setName),
		logger.Int("total", len(members)),
		logger.Int64("added", added))

	return added, nil
}

func (s *deduplicationService) IsMember(ctx context.Context, setName string, member string) (bool, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("dedup.is_member", time.Since(start), true)
	}()

	if setName == "" || member == "" {
		return false, rediserr.New(rediserr.ErrCodeInvalidInput, "set name and member cannot be empty")
	}

	key := s.buildKey(setName)

	exists, err := s.client.GetClient().SIsMember(ctx, key, member).Result()
	if err != nil {
		s.logger.Error("检查元素存在性失败",
			logger.String("set", setName),
			logger.String("member", member),
			logger.ErrorField(err))
		return false, rediserr.Wrap(err, rediserr.ErrCodeInternal, "is member failed")
	}

	return exists, nil
}

func (s *deduplicationService) Remove(ctx context.Context, setName string, member string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("dedup.remove", time.Since(start), true)
	}()

	if setName == "" || member == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "set name and member cannot be empty")
	}

	key := s.buildKey(setName)

	err := s.client.GetClient().SRem(ctx, key, member).Err()
	if err != nil {
		s.logger.Error("移除元素失败",
			logger.String("set", setName),
			logger.String("member", member),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "remove member failed")
	}

	s.logger.Debug("移除元素成功",
		logger.String("set", setName),
		logger.String("member", member))

	return nil
}

func (s *deduplicationService) RemoveMultiple(ctx context.Context, setName string, members ...string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("dedup.remove_multiple", time.Since(start), true)
	}()

	if setName == "" || len(members) == 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "set name and members cannot be empty")
	}

	key := s.buildKey(setName)

	values := make([]interface{}, len(members))
	for i, m := range members {
		values[i] = m
	}

	err := s.client.GetClient().SRem(ctx, key, values...).Err()
	if err != nil {
		s.logger.Error("批量移除元素失败",
			logger.String("set", setName),
			logger.Int("count", len(members)),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "remove multiple failed")
	}

	s.logger.Debug("批量移除元素成功",
		logger.String("set", setName),
		logger.Int("count", len(members)))

	return nil
}

func (s *deduplicationService) GetAll(ctx context.Context, setName string) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("dedup.get_all", time.Since(start), true)
	}()

	if setName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "set name cannot be empty")
	}

	key := s.buildKey(setName)

	members, err := s.client.GetClient().SMembers(ctx, key).Result()
	if err != nil {
		s.logger.Error("获取所有元素失败",
			logger.String("set", setName),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get all failed")
	}

	return members, nil
}

func (s *deduplicationService) Count(ctx context.Context, setName string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("dedup.count", time.Since(start), true)
	}()

	if setName == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "set name cannot be empty")
	}

	key := s.buildKey(setName)

	count, err := s.client.GetClient().SCard(ctx, key).Result()
	if err != nil {
		s.logger.Error("获取元素数量失败",
			logger.String("set", setName),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "count failed")
	}

	return count, nil
}

func (s *deduplicationService) Clear(ctx context.Context, setName string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("dedup.clear", time.Since(start), true)
	}()

	if setName == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "set name cannot be empty")
	}

	key := s.buildKey(setName)

	err := s.client.GetClient().Del(ctx, key).Err()
	if err != nil {
		s.logger.Error("清空集合失败",
			logger.String("set", setName),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "clear set failed")
	}

	s.logger.Info("清空集合成功", logger.String("set", setName))
	return nil
}

func (s *deduplicationService) RandomMember(ctx context.Context, setName string) (string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("dedup.random_member", time.Since(start), true)
	}()

	if setName == "" {
		return "", rediserr.New(rediserr.ErrCodeInvalidInput, "set name cannot be empty")
	}

	key := s.buildKey(setName)

	member, err := s.client.GetClient().SRandMember(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		s.logger.Error("随机获取元素失败",
			logger.String("set", setName),
			logger.ErrorField(err))
		return "", rediserr.Wrap(err, rediserr.ErrCodeInternal, "random member failed")
	}

	return member, nil
}

func (s *deduplicationService) RandomMembers(ctx context.Context, setName string, count int) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("dedup.random_members", time.Since(start), true)
	}()

	if setName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "set name cannot be empty")
	}

	if count <= 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "count must be positive")
	}

	key := s.buildKey(setName)

	members, err := s.client.GetClient().SRandMemberN(ctx, key, int64(count)).Result()
	if err != nil {
		s.logger.Error("随机获取多个元素失败",
			logger.String("set", setName),
			logger.Int("count", count),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "random members failed")
	}

	return members, nil
}

func (s *deduplicationService) Pop(ctx context.Context, setName string) (string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("dedup.pop", time.Since(start), true)
	}()

	if setName == "" {
		return "", rediserr.New(rediserr.ErrCodeInvalidInput, "set name cannot be empty")
	}

	key := s.buildKey(setName)

	member, err := s.client.GetClient().SPop(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		s.logger.Error("弹出元素失败",
			logger.String("set", setName),
			logger.ErrorField(err))
		return "", rediserr.Wrap(err, rediserr.ErrCodeInternal, "pop failed")
	}

	s.logger.Debug("弹出元素",
		logger.String("set", setName),
		logger.String("member", member))

	return member, nil
}

func (s *deduplicationService) PopMultiple(ctx context.Context, setName string, count int) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("dedup.pop_multiple", time.Since(start), true)
	}()

	if setName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "set name cannot be empty")
	}

	if count <= 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "count must be positive")
	}

	key := s.buildKey(setName)

	members, err := s.client.GetClient().SPopN(ctx, key, int64(count)).Result()
	if err != nil {
		s.logger.Error("批量弹出元素失败",
			logger.String("set", setName),
			logger.Int("count", count),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "pop multiple failed")
	}

	s.logger.Debug("批量弹出元素",
		logger.String("set", setName),
		logger.Int("count", len(members)))

	return members, nil
}

func (s *deduplicationService) Union(ctx context.Context, setNames ...string) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("dedup.union", time.Since(start), true)
	}()

	if len(setNames) == 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "set names cannot be empty")
	}

	keys := make([]string, len(setNames))
	for i, name := range setNames {
		keys[i] = s.buildKey(name)
	}

	members, err := s.client.GetClient().SUnion(ctx, keys...).Result()
	if err != nil {
		s.logger.Error("求并集失败",
			logger.Int("set_count", len(setNames)),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "union failed")
	}

	return members, nil
}

func (s *deduplicationService) Intersect(ctx context.Context, setNames ...string) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("dedup.intersect", time.Since(start), true)
	}()

	if len(setNames) == 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "set names cannot be empty")
	}

	keys := make([]string, len(setNames))
	for i, name := range setNames {
		keys[i] = s.buildKey(name)
	}

	members, err := s.client.GetClient().SInter(ctx, keys...).Result()
	if err != nil {
		s.logger.Error("求交集失败",
			logger.Int("set_count", len(setNames)),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "intersect failed")
	}

	return members, nil
}

func (s *deduplicationService) Diff(ctx context.Context, setNames ...string) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("dedup.diff", time.Since(start), true)
	}()

	if len(setNames) == 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "set names cannot be empty")
	}

	keys := make([]string, len(setNames))
	for i, name := range setNames {
		keys[i] = s.buildKey(name)
	}

	members, err := s.client.GetClient().SDiff(ctx, keys...).Result()
	if err != nil {
		s.logger.Error("求差集失败",
			logger.Int("set_count", len(setNames)),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "diff failed")
	}

	return members, nil
}

func (s *deduplicationService) Move(ctx context.Context, fromSet, toSet, member string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("dedup.move", time.Since(start), true)
	}()

	if fromSet == "" || toSet == "" || member == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "set names and member cannot be empty")
	}

	fromKey := s.buildKey(fromSet)
	toKey := s.buildKey(toSet)

	success, err := s.client.GetClient().SMove(ctx, fromKey, toKey, member).Result()
	if err != nil {
		s.logger.Error("移动元素失败",
			logger.String("from_set", fromSet),
			logger.String("to_set", toSet),
			logger.String("member", member),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "move failed")
	}

	if !success {
		return rediserr.New(rediserr.ErrCodeNotFound, "member not found in source set")
	}

	s.logger.Debug("移动元素成功",
		logger.String("from_set", fromSet),
		logger.String("to_set", toSet),
		logger.String("member", member))

	return nil
}
