package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gcode/redis/client"
	rediserr "gcode/redis/pkg/errors"
	"gcode/redis/pkg/logger"
	"gcode/redis/pkg/metrics"
)

// LatestPost 最新消息/帖子结构
type LatestPost struct {
	ID        string                 `json:"id"`
	UserID    string                 `json:"user_id"`
	Username  string                 `json:"username"`
	Content   string                 `json:"content"`
	Timestamp int64                  `json:"timestamp"`
	Type      string                 `json:"type"`
	Extra     map[string]interface{} `json:"extra,omitempty"`
}

// LatestMessagesService 最新消息服务接口
// 基于 Redis List 实现的固定长度最新列表
// 适用场景：
// - 动态时间线
// - 最新评论列表
// - 最新文章列表
// - 操作日志
// - 消息通知列表
type LatestMessagesService interface {
	// AddPost 添加消息到列表头部
	// maxSize: 列表最大长度，超过后自动删除旧消息
	AddPost(ctx context.Context, listName string, post *LatestPost, maxSize int) error

	// GetLatest 获取最新的N条消息
	GetLatest(ctx context.Context, listName string, count int) ([]*LatestPost, error)

	// GetAll 获取所有消息
	GetAll(ctx context.Context, listName string) ([]*LatestPost, error)

	// GetRange 获取指定范围的消息
	// start, stop: 索引范围（0开始）
	GetRange(ctx context.Context, listName string, start, stop int64) ([]*LatestPost, error)

	// GetCount 获取消息总数
	GetCount(ctx context.Context, listName string) (int64, error)

	// Clear 清空列表
	Clear(ctx context.Context, listName string) error

	// RemovePost 移除指定消息
	// 根据消息ID查找并移除
	RemovePost(ctx context.Context, listName string, postID string) error

	// Trim 修剪列表，只保留指定数量
	// maxSize: 保留的最大数量
	Trim(ctx context.Context, listName string, maxSize int) error

	// GetPage 分页获取消息
	// page: 页码（从1开始）
	// pageSize: 每页数量
	GetPage(ctx context.Context, listName string, page, pageSize int) ([]*LatestPost, int64, error)

	// BatchAdd 批量添加消息
	BatchAdd(ctx context.Context, listName string, posts []*LatestPost, maxSize int) error
}

type latestMessagesService struct {
	client  client.Client
	logger  logger.Logger
	metrics metrics.Metrics
	prefix  string
}

// NewLatestMessagesService 创建最新消息服务
func NewLatestMessagesService(client client.Client, log logger.Logger, m metrics.Metrics, prefix string) LatestMessagesService {
	if prefix == "" {
		prefix = "latest"
	}
	return &latestMessagesService{
		client:  client,
		logger:  log,
		metrics: m,
		prefix:  prefix,
	}
}

func (s *latestMessagesService) buildKey(listName string) string {
	return fmt.Sprintf("%s:%s", s.prefix, listName)
}

func (s *latestMessagesService) serializePost(post *LatestPost) (string, error) {
	if post.Timestamp == 0 {
		post.Timestamp = time.Now().Unix()
	}

	data, err := json.Marshal(post)
	if err != nil {
		return "", rediserr.Wrap(err, rediserr.ErrCodeSerialization, "serialize post failed")
	}
	return string(data), nil
}

func (s *latestMessagesService) deserializePost(data string) (*LatestPost, error) {
	var post LatestPost
	if err := json.Unmarshal([]byte(data), &post); err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeSerialization, "deserialize post failed")
	}
	return &post, nil
}

func (s *latestMessagesService) AddPost(ctx context.Context, listName string, post *LatestPost, maxSize int) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("latest.add_post", time.Since(start), true)
	}()

	if listName == "" || post == nil {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "list name and post cannot be empty")
	}

	if maxSize <= 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "max size must be positive")
	}

	key := s.buildKey(listName)

	data, err := s.serializePost(post)
	if err != nil {
		return err
	}

	// 使用 Pipeline 保证原子性
	pipe := s.client.GetClient().Pipeline()
	pipe.LPush(ctx, key, data)
	pipe.LTrim(ctx, key, 0, int64(maxSize-1))

	_, err = pipe.Exec(ctx)
	if err != nil {
		s.logger.Error("添加消息失败",
			logger.String("list", listName),
			logger.String("post_id", post.ID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "add post failed")
	}

	s.logger.Debug("添加消息成功",
		logger.String("list", listName),
		logger.String("post_id", post.ID),
		logger.String("user_id", post.UserID),
		logger.Int("max_size", maxSize))

	return nil
}

func (s *latestMessagesService) GetLatest(ctx context.Context, listName string, count int) ([]*LatestPost, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("latest.get_latest", time.Since(start), true)
	}()

	if listName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "list name cannot be empty")
	}

	if count <= 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "count must be positive")
	}

	return s.GetRange(ctx, listName, 0, int64(count-1))
}

func (s *latestMessagesService) GetAll(ctx context.Context, listName string) ([]*LatestPost, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("latest.get_all", time.Since(start), true)
	}()

	if listName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "list name cannot be empty")
	}

	return s.GetRange(ctx, listName, 0, -1)
}

func (s *latestMessagesService) GetRange(ctx context.Context, listName string, start, stop int64) ([]*LatestPost, error) {
	startTime := time.Now()
	defer func() {
		s.metrics.RecordOperation("latest.get_range", time.Since(startTime), true)
	}()

	if listName == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "list name cannot be empty")
	}

	key := s.buildKey(listName)

	results, err := s.client.GetClient().LRange(ctx, key, start, stop).Result()
	if err != nil {
		s.logger.Error("获取消息范围失败",
			logger.String("list", listName),
			logger.Int64("start", start),
			logger.Int64("stop", stop),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get range failed")
	}

	posts := make([]*LatestPost, 0, len(results))
	for _, data := range results {
		if post, err := s.deserializePost(data); err == nil {
			posts = append(posts, post)
		}
	}

	return posts, nil
}

func (s *latestMessagesService) GetCount(ctx context.Context, listName string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("latest.get_count", time.Since(start), true)
	}()

	if listName == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "list name cannot be empty")
	}

	key := s.buildKey(listName)

	count, err := s.client.GetClient().LLen(ctx, key).Result()
	if err != nil {
		s.logger.Error("获取消息数量失败",
			logger.String("list", listName),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get count failed")
	}

	return count, nil
}

func (s *latestMessagesService) Clear(ctx context.Context, listName string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("latest.clear", time.Since(start), true)
	}()

	if listName == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "list name cannot be empty")
	}

	key := s.buildKey(listName)

	err := s.client.GetClient().Del(ctx, key).Err()
	if err != nil {
		s.logger.Error("清空列表失败",
			logger.String("list", listName),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "clear list failed")
	}

	s.logger.Info("清空列表成功", logger.String("list", listName))
	return nil
}

func (s *latestMessagesService) RemovePost(ctx context.Context, listName string, postID string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("latest.remove_post", time.Since(start), true)
	}()

	if listName == "" || postID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "list name and post ID cannot be empty")
	}

	key := s.buildKey(listName)

	// 需要遍历查找包含该 postID 的成员
	script := `
		local items = redis.call('LRANGE', KEYS[1], 0, -1)
		for i, item in ipairs(items) do
			if string.find(item, ARGV[1]) then
				redis.call('LREM', KEYS[1], 1, item)
				return 1
			end
		end
		return 0
	`

	result, err := s.client.GetClient().Eval(ctx, script, []string{key}, fmt.Sprintf("\"id\":\"%s\"", postID)).Result()
	if err != nil {
		s.logger.Error("移除消息失败",
			logger.String("list", listName),
			logger.String("post_id", postID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "remove post failed")
	}

	if result.(int64) == 0 {
		return rediserr.New(rediserr.ErrCodeNotFound, "post not found")
	}

	s.logger.Info("移除消息成功",
		logger.String("list", listName),
		logger.String("post_id", postID))

	return nil
}

func (s *latestMessagesService) Trim(ctx context.Context, listName string, maxSize int) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("latest.trim", time.Since(start), true)
	}()

	if listName == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "list name cannot be empty")
	}

	if maxSize <= 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "max size must be positive")
	}

	key := s.buildKey(listName)

	err := s.client.GetClient().LTrim(ctx, key, 0, int64(maxSize-1)).Err()
	if err != nil {
		s.logger.Error("修剪列表失败",
			logger.String("list", listName),
			logger.Int("max_size", maxSize),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "trim list failed")
	}

	s.logger.Debug("修剪列表成功",
		logger.String("list", listName),
		logger.Int("max_size", maxSize))

	return nil
}

func (s *latestMessagesService) GetPage(ctx context.Context, listName string, page, pageSize int) ([]*LatestPost, int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("latest.get_page", time.Since(start), true)
	}()

	if listName == "" {
		return nil, 0, rediserr.New(rediserr.ErrCodeInvalidInput, "list name cannot be empty")
	}

	if page <= 0 || pageSize <= 0 {
		return nil, 0, rediserr.New(rediserr.ErrCodeInvalidInput, "page and page size must be positive")
	}

	// 获取总数
	total, err := s.GetCount(ctx, listName)
	if err != nil {
		return nil, 0, err
	}

	// 计算范围
	startIdx := int64((page - 1) * pageSize)
	endIdx := startIdx + int64(pageSize) - 1

	if startIdx >= total {
		return []*LatestPost{}, total, nil
	}

	posts, err := s.GetRange(ctx, listName, startIdx, endIdx)
	if err != nil {
		return nil, 0, err
	}

	return posts, total, nil
}

func (s *latestMessagesService) BatchAdd(ctx context.Context, listName string, posts []*LatestPost, maxSize int) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("latest.batch_add", time.Since(start), true)
	}()

	if listName == "" || len(posts) == 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "list name and posts cannot be empty")
	}

	if maxSize <= 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "max size must be positive")
	}

	key := s.buildKey(listName)

	// 序列化所有消息
	values := make([]interface{}, len(posts))
	for i, post := range posts {
		data, err := s.serializePost(post)
		if err != nil {
			return err
		}
		values[i] = data
	}

	// 使用 Pipeline
	pipe := s.client.GetClient().Pipeline()
	pipe.LPush(ctx, key, values...)
	pipe.LTrim(ctx, key, 0, int64(maxSize-1))

	_, err := pipe.Exec(ctx)
	if err != nil {
		s.logger.Error("批量添加消息失败",
			logger.String("list", listName),
			logger.Int("count", len(posts)),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "batch add failed")
	}

	s.logger.Info("批量添加消息成功",
		logger.String("list", listName),
		logger.Int("count", len(posts)),
		logger.Int("max_size", maxSize))

	return nil
}
