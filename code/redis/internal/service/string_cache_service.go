package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gcode/redis/internal/repository"
	rediserr "gcode/redis/pkg/errors"
	"gcode/redis/pkg/logger"
)

// StringCacheService 字符串缓存服务接口
// 提供基于 Redis String 数据结构的缓存功能
// 适用场景：
// - 对象缓存（用户信息、商品详情等）
// - 页面缓存
// - 接口响应缓存
// - 临时数据存储
type StringCacheService interface {
	// Set 设置缓存
	// key: 缓存键
	// value: 缓存值（支持任意类型，自动序列化）
	// ttl: 过期时间，0 表示永不过期
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error

	// Get 获取缓存
	// key: 缓存键
	// dest: 目标对象指针，用于反序列化
	Get(ctx context.Context, key string, dest interface{}) error

	// GetString 获取字符串缓存
	// 直接返回字符串，不进行反序列化
	GetString(ctx context.Context, key string) (string, error)

	// Delete 删除缓存
	// 支持批量删除多个键
	Delete(ctx context.Context, keys ...string) error

	// Exists 检查缓存是否存在
	// 返回存在的键数量
	Exists(ctx context.Context, keys ...string) (int64, error)

	// SetWithNX 仅当键不存在时设置
	// 返回是否设置成功
	// 常用于防止缓存击穿
	SetWithNX(ctx context.Context, key string, value interface{}, ttl time.Duration) (bool, error)

	// GetWithTTL 获取缓存及剩余过期时间
	// 返回值和剩余 TTL
	GetWithTTL(ctx context.Context, key string, dest interface{}) (time.Duration, error)

	// Refresh 刷新缓存过期时间
	// 不改变值，只更新 TTL
	Refresh(ctx context.Context, key string, ttl time.Duration) error

	// GetOrSet 获取缓存，如果不存在则设置
	// loader: 加载函数，当缓存不存在时调用
	// 常用于缓存穿透保护
	GetOrSet(ctx context.Context, key string, dest interface{}, ttl time.Duration, loader func() (interface{}, error)) error

	// MGet 批量获取缓存
	// 返回与 keys 对应的值列表，不存在的为 nil
	MGet(ctx context.Context, keys ...string) ([]interface{}, error)

	// MSet 批量设置缓存
	// pairs: 键值对，格式为 key1, value1, key2, value2, ...
	MSet(ctx context.Context, pairs map[string]interface{}, ttl time.Duration) error
}

type stringCacheService struct {
	repo   repository.CacheRepository
	logger logger.Logger
	prefix string // 键前缀，用于命名空间隔离
}

// NewStringCacheService 创建字符串缓存服务
// prefix: 键前缀，建议使用应用名或模块名，如 "myapp:cache"
func NewStringCacheService(repo repository.CacheRepository, log logger.Logger, prefix string) StringCacheService {
	if prefix == "" {
		prefix = "cache"
	}
	return &stringCacheService{
		repo:   repo,
		logger: log,
		prefix: prefix,
	}
}

// buildKey 构建完整的缓存键
// 添加前缀以实现命名空间隔离
func (s *stringCacheService) buildKey(key string) string {
	return fmt.Sprintf("%s:%s", s.prefix, key)
}

func (s *stringCacheService) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if key == "" {
		s.logger.Error("缓存键不能为空")
		return rediserr.New(rediserr.ErrCodeInvalidInput, "key cannot be empty")
	}

	fullKey := s.buildKey(key)

	if err := s.repo.Set(ctx, fullKey, value, ttl); err != nil {
		s.logger.Error("设置缓存失败",
			logger.String("key", key),
			logger.Duration("ttl", ttl),
			logger.ErrorField(err))
		return err
	}

	s.logger.Debug("缓存设置成功",
		logger.String("key", key),
		logger.Duration("ttl", ttl))

	return nil
}

func (s *stringCacheService) Get(ctx context.Context, key string, dest interface{}) error {
	if key == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "key cannot be empty")
	}

	if dest == nil {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "destination cannot be nil")
	}

	fullKey := s.buildKey(key)

	if err := s.repo.Get(ctx, fullKey, dest); err != nil {
		if rediserr.IsNotFound(err) {
			s.logger.Debug("缓存未命中", logger.String("key", key))
		} else {
			s.logger.Error("获取缓存失败", logger.String("key", key), logger.ErrorField(err))
		}
		return err
	}

	s.logger.Debug("缓存命中", logger.String("key", key))
	return nil
}

func (s *stringCacheService) GetString(ctx context.Context, key string) (string, error) {
	if key == "" {
		return "", rediserr.New(rediserr.ErrCodeInvalidInput, "key cannot be empty")
	}

	fullKey := s.buildKey(key)

	value, err := s.repo.GetString(ctx, fullKey)
	if err != nil {
		if rediserr.IsNotFound(err) {
			s.logger.Debug("缓存未命中", logger.String("key", key))
		} else {
			s.logger.Error("获取缓存失败", logger.String("key", key), logger.ErrorField(err))
		}
		return "", err
	}

	s.logger.Debug("缓存命中", logger.String("key", key))
	return value, nil
}

func (s *stringCacheService) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "no keys provided")
	}

	fullKeys := make([]string, len(keys))
	for i, key := range keys {
		fullKeys[i] = s.buildKey(key)
	}

	if err := s.repo.Delete(ctx, fullKeys...); err != nil {
		s.logger.Error("删除缓存失败",
			logger.Int("key_count", len(keys)),
			logger.ErrorField(err))
		return err
	}

	s.logger.Debug("缓存删除成功", logger.Int("key_count", len(keys)))
	return nil
}

func (s *stringCacheService) Exists(ctx context.Context, keys ...string) (int64, error) {
	if len(keys) == 0 {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "no keys provided")
	}

	fullKeys := make([]string, len(keys))
	for i, key := range keys {
		fullKeys[i] = s.buildKey(key)
	}

	count, err := s.repo.Exists(ctx, fullKeys...)
	if err != nil {
		s.logger.Error("检查缓存存在性失败", logger.ErrorField(err))
		return 0, err
	}

	return count, nil
}

func (s *stringCacheService) SetWithNX(ctx context.Context, key string, value interface{}, ttl time.Duration) (bool, error) {
	if key == "" {
		return false, rediserr.New(rediserr.ErrCodeInvalidInput, "key cannot be empty")
	}

	fullKey := s.buildKey(key)

	success, err := s.repo.SetNX(ctx, fullKey, value, ttl)
	if err != nil {
		s.logger.Error("SetNX 失败",
			logger.String("key", key),
			logger.ErrorField(err))
		return false, err
	}

	if success {
		s.logger.Debug("SetNX 成功", logger.String("key", key))
	} else {
		s.logger.Debug("SetNX 失败，键已存在", logger.String("key", key))
	}

	return success, nil
}

func (s *stringCacheService) GetWithTTL(ctx context.Context, key string, dest interface{}) (time.Duration, error) {
	if key == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "key cannot be empty")
	}

	if dest == nil {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "destination cannot be nil")
	}

	fullKey := s.buildKey(key)

	// 先获取值
	if err := s.repo.Get(ctx, fullKey, dest); err != nil {
		return 0, err
	}

	// 再获取 TTL
	ttl, err := s.repo.TTL(ctx, fullKey)
	if err != nil {
		s.logger.Warn("获取 TTL 失败", logger.String("key", key), logger.ErrorField(err))
		return 0, err
	}

	s.logger.Debug("获取缓存及 TTL 成功",
		logger.String("key", key),
		logger.Duration("ttl", ttl))

	return ttl, nil
}

func (s *stringCacheService) Refresh(ctx context.Context, key string, ttl time.Duration) error {
	if key == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "key cannot be empty")
	}

	fullKey := s.buildKey(key)

	if err := s.repo.Expire(ctx, fullKey, ttl); err != nil {
		s.logger.Error("刷新缓存过期时间失败",
			logger.String("key", key),
			logger.Duration("ttl", ttl),
			logger.ErrorField(err))
		return err
	}

	s.logger.Debug("缓存过期时间刷新成功",
		logger.String("key", key),
		logger.Duration("ttl", ttl))

	return nil
}

func (s *stringCacheService) GetOrSet(ctx context.Context, key string, dest interface{}, ttl time.Duration, loader func() (interface{}, error)) error {
	if key == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "key cannot be empty")
	}

	if dest == nil {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "destination cannot be nil")
	}

	if loader == nil {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "loader function cannot be nil")
	}

	// 先尝试从缓存获取
	err := s.Get(ctx, key, dest)
	if err == nil {
		s.logger.Debug("从缓存获取成功", logger.String("key", key))
		return nil
	}

	// 如果不是 NotFound 错误，直接返回
	if !rediserr.IsNotFound(err) {
		return err
	}

	s.logger.Debug("缓存未命中，执行加载函数", logger.String("key", key))

	// 执行加载函数
	value, err := loader()
	if err != nil {
		s.logger.Error("加载函数执行失败",
			logger.String("key", key),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "loader function failed")
	}

	// 设置缓存
	if err := s.Set(ctx, key, value, ttl); err != nil {
		s.logger.Warn("设置缓存失败，但返回加载的值",
			logger.String("key", key),
			logger.ErrorField(err))
	}

	// 将加载的值赋给 dest
	data, err := json.Marshal(value)
	if err != nil {
		return rediserr.Wrap(err, rediserr.ErrCodeSerialization, "failed to marshal loaded value")
	}

	if err := json.Unmarshal(data, dest); err != nil {
		return rediserr.Wrap(err, rediserr.ErrCodeSerialization, "failed to unmarshal loaded value")
	}

	s.logger.Debug("加载并缓存成功", logger.String("key", key))
	return nil
}

func (s *stringCacheService) MGet(ctx context.Context, keys ...string) ([]interface{}, error) {
	if len(keys) == 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "no keys provided")
	}

	fullKeys := make([]string, len(keys))
	for i, key := range keys {
		fullKeys[i] = s.buildKey(key)
	}

	values, err := s.repo.MGet(ctx, fullKeys...)
	if err != nil {
		s.logger.Error("批量获取缓存失败",
			logger.Int("key_count", len(keys)),
			logger.ErrorField(err))
		return nil, err
	}

	s.logger.Debug("批量获取缓存成功", logger.Int("key_count", len(keys)))
	return values, nil
}

func (s *stringCacheService) MSet(ctx context.Context, pairs map[string]interface{}, ttl time.Duration) error {
	if len(pairs) == 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "no pairs provided")
	}

	// 构建完整键的 pairs
	fullPairs := make(map[string]interface{}, len(pairs))
	for key, value := range pairs {
		fullPairs[s.buildKey(key)] = value
	}

	// 先批量设置值
	flatPairs := make([]interface{}, 0, len(fullPairs)*2)
	for key, value := range fullPairs {
		flatPairs = append(flatPairs, key, value)
	}

	if err := s.repo.MSet(ctx, flatPairs...); err != nil {
		s.logger.Error("批量设置缓存失败",
			logger.Int("pair_count", len(pairs)),
			logger.ErrorField(err))
		return err
	}

	// 如果设置了 TTL，需要逐个设置过期时间
	if ttl > 0 {
		for key := range fullPairs {
			if err := s.repo.Expire(ctx, key, ttl); err != nil {
				s.logger.Warn("设置过期时间失败",
					logger.String("key", key),
					logger.ErrorField(err))
			}
		}
	}

	s.logger.Debug("批量设置缓存成功",
		logger.Int("pair_count", len(pairs)),
		logger.Duration("ttl", ttl))

	return nil
}
