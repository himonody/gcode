package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"gcode/redis/client"
	rediserr "gcode/redis/pkg/errors"
	"gcode/redis/pkg/logger"
	"gcode/redis/pkg/metrics"
)

// UserInfo 用户信息结构
type UserInfo struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone"`
	Nickname  string    `json:"nickname"`
	Avatar    string    `json:"avatar"`
	Gender    int       `json:"gender"`
	Birthday  string    `json:"birthday"`
	Balance   float64   `json:"balance"`
	Points    int64     `json:"points"`
	Level     int       `json:"level"`
	Status    int       `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UserInfoService 用户信息服务接口
// 基于 Redis Hash 实现的用户信息存储
// 适用场景：
// - 用户资料存储
// - 账户信息管理
// - 用户配置项
// - 字段级别的更新
type UserInfoService interface {
	// Save 保存用户信息
	Save(ctx context.Context, user *UserInfo) error

	// Get 获取用户信息
	Get(ctx context.Context, userID string) (*UserInfo, error)

	// GetField 获取用户单个字段
	GetField(ctx context.Context, userID string, field string) (string, error)

	// GetFields 获取用户多个字段
	GetFields(ctx context.Context, userID string, fields ...string) (map[string]string, error)

	// UpdateField 更新用户单个字段
	UpdateField(ctx context.Context, userID string, field string, value interface{}) error

	// UpdateFields 更新用户多个字段
	UpdateFields(ctx context.Context, userID string, fields map[string]interface{}) error

	// IncrBalance 增加用户余额
	// delta: 可以为负数（扣减余额）
	IncrBalance(ctx context.Context, userID string, delta float64) (float64, error)

	// IncrPoints 增加用户积分
	IncrPoints(ctx context.Context, userID string, delta int64) (int64, error)

	// Delete 删除用户信息
	Delete(ctx context.Context, userID string) error

	// Exists 检查用户是否存在
	Exists(ctx context.Context, userID string) (bool, error)

	// GetAll 获取用户所有字段
	GetAll(ctx context.Context, userID string) (map[string]string, error)

	// SetExpire 设置用户信息过期时间
	SetExpire(ctx context.Context, userID string, ttl time.Duration) error
}

type userInfoService struct {
	client  client.Client
	logger  logger.Logger
	metrics metrics.Metrics
	prefix  string
}

// NewUserInfoService 创建用户信息服务
func NewUserInfoService(client client.Client, log logger.Logger, m metrics.Metrics, prefix string) UserInfoService {
	if prefix == "" {
		prefix = "user:info"
	}
	return &userInfoService{
		client:  client,
		logger:  log,
		metrics: m,
		prefix:  prefix,
	}
}

func (s *userInfoService) buildKey(userID string) string {
	return fmt.Sprintf("%s:%s", s.prefix, userID)
}

func (s *userInfoService) Save(ctx context.Context, user *UserInfo) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("user.save", time.Since(start), true)
	}()

	if user == nil || user.ID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user or user ID cannot be empty")
	}

	key := s.buildKey(user.ID)

	// 构建字段映射
	fields := map[string]interface{}{
		"id":         user.ID,
		"username":   user.Username,
		"email":      user.Email,
		"phone":      user.Phone,
		"nickname":   user.Nickname,
		"avatar":     user.Avatar,
		"gender":     user.Gender,
		"birthday":   user.Birthday,
		"balance":    user.Balance,
		"points":     user.Points,
		"level":      user.Level,
		"status":     user.Status,
		"created_at": user.CreatedAt.Unix(),
		"updated_at": time.Now().Unix(),
	}

	err := s.client.GetClient().HSet(ctx, key, fields).Err()
	if err != nil {
		s.logger.Error("保存用户信息失败",
			logger.String("user_id", user.ID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "save user failed")
	}

	s.logger.Debug("保存用户信息成功",
		logger.String("user_id", user.ID),
		logger.String("username", user.Username))

	return nil
}

func (s *userInfoService) Get(ctx context.Context, userID string) (*UserInfo, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("user.get", time.Since(start), true)
	}()

	if userID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	key := s.buildKey(userID)

	result, err := s.client.GetClient().HGetAll(ctx, key).Result()
	if err != nil {
		s.logger.Error("获取用户信息失败",
			logger.String("user_id", userID),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get user failed")
	}

	if len(result) == 0 {
		s.logger.Debug("用户不存在", logger.String("user_id", userID))
		return nil, rediserr.New(rediserr.ErrCodeNotFound, "user not found")
	}

	// 解析字段
	user := &UserInfo{
		ID:       result["id"],
		Username: result["username"],
		Email:    result["email"],
		Phone:    result["phone"],
		Nickname: result["nickname"],
		Avatar:   result["avatar"],
		Birthday: result["birthday"],
	}

	if v, err := strconv.Atoi(result["gender"]); err == nil {
		user.Gender = v
	}
	if v, err := strconv.ParseFloat(result["balance"], 64); err == nil {
		user.Balance = v
	}
	if v, err := strconv.ParseInt(result["points"], 10, 64); err == nil {
		user.Points = v
	}
	if v, err := strconv.Atoi(result["level"]); err == nil {
		user.Level = v
	}
	if v, err := strconv.Atoi(result["status"]); err == nil {
		user.Status = v
	}
	if v, err := strconv.ParseInt(result["created_at"], 10, 64); err == nil {
		user.CreatedAt = time.Unix(v, 0)
	}
	if v, err := strconv.ParseInt(result["updated_at"], 10, 64); err == nil {
		user.UpdatedAt = time.Unix(v, 0)
	}

	return user, nil
}

func (s *userInfoService) GetField(ctx context.Context, userID string, field string) (string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("user.get_field", time.Since(start), true)
	}()

	if userID == "" || field == "" {
		return "", rediserr.New(rediserr.ErrCodeInvalidInput, "user ID and field cannot be empty")
	}

	key := s.buildKey(userID)

	value, err := s.client.GetClient().HGet(ctx, key, field).Result()
	if err == redis.Nil {
		return "", rediserr.New(rediserr.ErrCodeNotFound, "field not found")
	}
	if err != nil {
		s.logger.Error("获取用户字段失败",
			logger.String("user_id", userID),
			logger.String("field", field),
			logger.ErrorField(err))
		return "", rediserr.Wrap(err, rediserr.ErrCodeInternal, "get field failed")
	}

	return value, nil
}

func (s *userInfoService) GetFields(ctx context.Context, userID string, fields ...string) (map[string]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("user.get_fields", time.Since(start), true)
	}()

	if userID == "" || len(fields) == 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID and fields cannot be empty")
	}

	key := s.buildKey(userID)

	values, err := s.client.GetClient().HMGet(ctx, key, fields...).Result()
	if err != nil {
		s.logger.Error("获取用户多个字段失败",
			logger.String("user_id", userID),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get fields failed")
	}

	result := make(map[string]string)
	for i, field := range fields {
		if values[i] != nil {
			if v, ok := values[i].(string); ok {
				result[field] = v
			}
		}
	}

	return result, nil
}

func (s *userInfoService) UpdateField(ctx context.Context, userID string, field string, value interface{}) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("user.update_field", time.Since(start), true)
	}()

	if userID == "" || field == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID and field cannot be empty")
	}

	key := s.buildKey(userID)

	// 同时更新 updated_at
	fields := map[string]interface{}{
		field:        value,
		"updated_at": time.Now().Unix(),
	}

	err := s.client.GetClient().HSet(ctx, key, fields).Err()
	if err != nil {
		s.logger.Error("更新用户字段失败",
			logger.String("user_id", userID),
			logger.String("field", field),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "update field failed")
	}

	s.logger.Debug("更新用户字段成功",
		logger.String("user_id", userID),
		logger.String("field", field))

	return nil
}

func (s *userInfoService) UpdateFields(ctx context.Context, userID string, fields map[string]interface{}) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("user.update_fields", time.Since(start), true)
	}()

	if userID == "" || len(fields) == 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID and fields cannot be empty")
	}

	key := s.buildKey(userID)

	// 添加更新时间
	fields["updated_at"] = time.Now().Unix()

	err := s.client.GetClient().HSet(ctx, key, fields).Err()
	if err != nil {
		s.logger.Error("更新用户多个字段失败",
			logger.String("user_id", userID),
			logger.Int("field_count", len(fields)),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "update fields failed")
	}

	s.logger.Debug("更新用户多个字段成功",
		logger.String("user_id", userID),
		logger.Int("field_count", len(fields)))

	return nil
}

func (s *userInfoService) IncrBalance(ctx context.Context, userID string, delta float64) (float64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("user.incr_balance", time.Since(start), true)
	}()

	if userID == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	key := s.buildKey(userID)

	newBalance, err := s.client.GetClient().HIncrByFloat(ctx, key, "balance", delta).Result()
	if err != nil {
		s.logger.Error("增加用户余额失败",
			logger.String("user_id", userID),
			logger.Float64("delta", delta),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "incr balance failed")
	}

	// 更新时间
	s.client.GetClient().HSet(ctx, key, "updated_at", time.Now().Unix())

	s.logger.Info("用户余额变更",
		logger.String("user_id", userID),
		logger.Float64("delta", delta),
		logger.Float64("new_balance", newBalance))

	return newBalance, nil
}

func (s *userInfoService) IncrPoints(ctx context.Context, userID string, delta int64) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("user.incr_points", time.Since(start), true)
	}()

	if userID == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	key := s.buildKey(userID)

	newPoints, err := s.client.GetClient().HIncrBy(ctx, key, "points", delta).Result()
	if err != nil {
		s.logger.Error("增加用户积分失败",
			logger.String("user_id", userID),
			logger.Int64("delta", delta),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "incr points failed")
	}

	// 更新时间
	s.client.GetClient().HSet(ctx, key, "updated_at", time.Now().Unix())

	s.logger.Info("用户积分变更",
		logger.String("user_id", userID),
		logger.Int64("delta", delta),
		logger.Int64("new_points", newPoints))

	return newPoints, nil
}

func (s *userInfoService) Delete(ctx context.Context, userID string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("user.delete", time.Since(start), true)
	}()

	if userID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	key := s.buildKey(userID)

	err := s.client.GetClient().Del(ctx, key).Err()
	if err != nil {
		s.logger.Error("删除用户信息失败",
			logger.String("user_id", userID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "delete user failed")
	}

	s.logger.Info("删除用户信息成功", logger.String("user_id", userID))
	return nil
}

func (s *userInfoService) Exists(ctx context.Context, userID string) (bool, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("user.exists", time.Since(start), true)
	}()

	if userID == "" {
		return false, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	key := s.buildKey(userID)

	exists, err := s.client.GetClient().Exists(ctx, key).Result()
	if err != nil {
		return false, rediserr.Wrap(err, rediserr.ErrCodeInternal, "check exists failed")
	}

	return exists > 0, nil
}

func (s *userInfoService) GetAll(ctx context.Context, userID string) (map[string]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("user.get_all", time.Since(start), true)
	}()

	if userID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	key := s.buildKey(userID)

	result, err := s.client.GetClient().HGetAll(ctx, key).Result()
	if err != nil {
		s.logger.Error("获取用户所有字段失败",
			logger.String("user_id", userID),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get all fields failed")
	}

	if len(result) == 0 {
		return nil, rediserr.New(rediserr.ErrCodeNotFound, "user not found")
	}

	return result, nil
}

func (s *userInfoService) SetExpire(ctx context.Context, userID string, ttl time.Duration) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("user.set_expire", time.Since(start), true)
	}()

	if userID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	key := s.buildKey(userID)

	err := s.client.GetClient().Expire(ctx, key, ttl).Err()
	if err != nil {
		s.logger.Error("设置用户信息过期时间失败",
			logger.String("user_id", userID),
			logger.Duration("ttl", ttl),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "set expire failed")
	}

	return nil
}
