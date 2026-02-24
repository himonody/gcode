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

// SessionData Session 数据结构
type SessionData struct {
	UserID    string                 `json:"user_id"`
	Username  string                 `json:"username"`
	LoginTime time.Time              `json:"login_time"`
	IP        string                 `json:"ip"`
	UserAgent string                 `json:"user_agent"`
	Roles     []string               `json:"roles,omitempty"`
	Extra     map[string]interface{} `json:"extra,omitempty"`
}

// SessionService Session 会话服务接口
// 基于 Redis String 实现的会话管理
// 适用场景：
// - 用户登录态管理
// - SSO 单点登录
// - 临时权限存储
// - 多端登录控制
type SessionService interface {
	// Create 创建会话
	// sessionID: 会话标识（通常是 UUID 或 JWT）
	// data: 会话数据
	// ttl: 会话有效期
	Create(ctx context.Context, sessionID string, data *SessionData, ttl time.Duration) error

	// Get 获取会话数据
	// 返回会话数据，如果会话不存在或已过期返回错误
	Get(ctx context.Context, sessionID string) (*SessionData, error)

	// Update 更新会话数据
	// 保持原有的 TTL
	Update(ctx context.Context, sessionID string, data *SessionData) error

	// UpdateField 更新会话中的单个字段
	// 部分更新，不影响其他字段
	UpdateField(ctx context.Context, sessionID string, field string, value interface{}) error

	// Refresh 刷新会话过期时间
	// 用于保持用户活跃状态
	Refresh(ctx context.Context, sessionID string, ttl time.Duration) error

	// Delete 删除会话（登出）
	Delete(ctx context.Context, sessionID string) error

	// Exists 检查会话是否存在
	Exists(ctx context.Context, sessionID string) (bool, error)

	// GetTTL 获取会话剩余有效时间
	GetTTL(ctx context.Context, sessionID string) (time.Duration, error)

	// GetMultiple 批量获取会话
	// 用于多端登录管理
	GetMultiple(ctx context.Context, sessionIDs ...string) (map[string]*SessionData, error)

	// DeleteMultiple 批量删除会话
	// 用于强制登出所有设备
	DeleteMultiple(ctx context.Context, sessionIDs ...string) error

	// GetUserSessions 获取用户的所有会话
	// 需要配合额外的索引实现
	GetUserSessions(ctx context.Context, userID string) ([]*SessionData, error)

	// KickUser 踢出用户所有会话
	KickUser(ctx context.Context, userID string) error
}

type sessionService struct {
	repo   repository.CacheRepository
	logger logger.Logger
	prefix string
}

// NewSessionService 创建 Session 服务
// prefix: 键前缀，建议使用 "session" 或 "sess"
func NewSessionService(repo repository.CacheRepository, log logger.Logger, prefix string) SessionService {
	if prefix == "" {
		prefix = "session"
	}
	return &sessionService{
		repo:   repo,
		logger: log,
		prefix: prefix,
	}
}

func (s *sessionService) buildKey(sessionID string) string {
	return fmt.Sprintf("%s:%s", s.prefix, sessionID)
}

func (s *sessionService) buildUserIndexKey(userID string) string {
	return fmt.Sprintf("%s:user:%s:sessions", s.prefix, userID)
}

func (s *sessionService) Create(ctx context.Context, sessionID string, data *SessionData, ttl time.Duration) error {
	if sessionID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "session ID cannot be empty")
	}

	if data == nil {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "session data cannot be nil")
	}

	if ttl <= 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "TTL must be positive")
	}

	// 设置登录时间
	if data.LoginTime.IsZero() {
		data.LoginTime = time.Now()
	}

	key := s.buildKey(sessionID)

	// 序列化并存储
	if err := s.repo.Set(ctx, key, data, ttl); err != nil {
		s.logger.Error("创建会话失败",
			logger.String("session_id", sessionID),
			logger.String("user_id", data.UserID),
			logger.ErrorField(err))
		return err
	}

	s.logger.Info("会话创建成功",
		logger.String("session_id", sessionID),
		logger.String("user_id", data.UserID),
		logger.String("username", data.Username),
		logger.Duration("ttl", ttl))

	return nil
}

func (s *sessionService) Get(ctx context.Context, sessionID string) (*SessionData, error) {
	if sessionID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "session ID cannot be empty")
	}

	key := s.buildKey(sessionID)

	var data SessionData
	if err := s.repo.Get(ctx, key, &data); err != nil {
		if rediserr.IsNotFound(err) {
			s.logger.Debug("会话不存在或已过期", logger.String("session_id", sessionID))
		} else {
			s.logger.Error("获取会话失败",
				logger.String("session_id", sessionID),
				logger.ErrorField(err))
		}
		return nil, err
	}

	s.logger.Debug("获取会话成功",
		logger.String("session_id", sessionID),
		logger.String("user_id", data.UserID))

	return &data, nil
}

func (s *sessionService) Update(ctx context.Context, sessionID string, data *SessionData) error {
	if sessionID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "session ID cannot be empty")
	}

	if data == nil {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "session data cannot be nil")
	}

	key := s.buildKey(sessionID)

	// 先获取当前 TTL
	ttl, err := s.repo.TTL(ctx, key)
	if err != nil {
		s.logger.Error("获取会话 TTL 失败",
			logger.String("session_id", sessionID),
			logger.ErrorField(err))
		return err
	}

	if ttl <= 0 {
		return rediserr.New(rediserr.ErrCodeNotFound, "session not found or expired")
	}

	// 更新数据，保持原有 TTL
	if err := s.repo.Set(ctx, key, data, ttl); err != nil {
		s.logger.Error("更新会话失败",
			logger.String("session_id", sessionID),
			logger.ErrorField(err))
		return err
	}

	s.logger.Debug("更新会话成功",
		logger.String("session_id", sessionID),
		logger.String("user_id", data.UserID))

	return nil
}

func (s *sessionService) UpdateField(ctx context.Context, sessionID string, field string, value interface{}) error {
	if sessionID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "session ID cannot be empty")
	}

	if field == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "field cannot be empty")
	}

	// 获取当前会话数据
	data, err := s.Get(ctx, sessionID)
	if err != nil {
		return err
	}

	// 更新字段
	switch field {
	case "username":
		if v, ok := value.(string); ok {
			data.Username = v
		}
	case "ip":
		if v, ok := value.(string); ok {
			data.IP = v
		}
	case "user_agent":
		if v, ok := value.(string); ok {
			data.UserAgent = v
		}
	case "roles":
		if v, ok := value.([]string); ok {
			data.Roles = v
		}
	default:
		// 更新 Extra 字段
		if data.Extra == nil {
			data.Extra = make(map[string]interface{})
		}
		data.Extra[field] = value
	}

	// 保存更新后的数据
	return s.Update(ctx, sessionID, data)
}

func (s *sessionService) Refresh(ctx context.Context, sessionID string, ttl time.Duration) error {
	if sessionID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "session ID cannot be empty")
	}

	if ttl <= 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "TTL must be positive")
	}

	key := s.buildKey(sessionID)

	if err := s.repo.Expire(ctx, key, ttl); err != nil {
		s.logger.Error("刷新会话失败",
			logger.String("session_id", sessionID),
			logger.Duration("ttl", ttl),
			logger.ErrorField(err))
		return err
	}

	s.logger.Debug("刷新会话成功",
		logger.String("session_id", sessionID),
		logger.Duration("ttl", ttl))

	return nil
}

func (s *sessionService) Delete(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "session ID cannot be empty")
	}

	key := s.buildKey(sessionID)

	if err := s.repo.Delete(ctx, key); err != nil {
		s.logger.Error("删除会话失败",
			logger.String("session_id", sessionID),
			logger.ErrorField(err))
		return err
	}

	s.logger.Info("会话删除成功（用户登出）",
		logger.String("session_id", sessionID))

	return nil
}

func (s *sessionService) Exists(ctx context.Context, sessionID string) (bool, error) {
	if sessionID == "" {
		return false, rediserr.New(rediserr.ErrCodeInvalidInput, "session ID cannot be empty")
	}

	key := s.buildKey(sessionID)

	count, err := s.repo.Exists(ctx, key)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func (s *sessionService) GetTTL(ctx context.Context, sessionID string) (time.Duration, error) {
	if sessionID == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "session ID cannot be empty")
	}

	key := s.buildKey(sessionID)

	ttl, err := s.repo.TTL(ctx, key)
	if err != nil {
		s.logger.Error("获取会话 TTL 失败",
			logger.String("session_id", sessionID),
			logger.ErrorField(err))
		return 0, err
	}

	return ttl, nil
}

func (s *sessionService) GetMultiple(ctx context.Context, sessionIDs ...string) (map[string]*SessionData, error) {
	if len(sessionIDs) == 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "no session IDs provided")
	}

	keys := make([]string, len(sessionIDs))
	for i, id := range sessionIDs {
		keys[i] = s.buildKey(id)
	}

	values, err := s.repo.MGet(ctx, keys...)
	if err != nil {
		s.logger.Error("批量获取会话失败",
			logger.Int("count", len(sessionIDs)),
			logger.ErrorField(err))
		return nil, err
	}

	result := make(map[string]*SessionData)
	for i, value := range values {
		if value == nil {
			continue
		}

		var data SessionData
		if jsonStr, ok := value.(string); ok {
			if err := json.Unmarshal([]byte(jsonStr), &data); err == nil {
				result[sessionIDs[i]] = &data
			}
		}
	}

	s.logger.Debug("批量获取会话成功",
		logger.Int("requested", len(sessionIDs)),
		logger.Int("found", len(result)))

	return result, nil
}

func (s *sessionService) DeleteMultiple(ctx context.Context, sessionIDs ...string) error {
	if len(sessionIDs) == 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "no session IDs provided")
	}

	keys := make([]string, len(sessionIDs))
	for i, id := range sessionIDs {
		keys[i] = s.buildKey(id)
	}

	if err := s.repo.Delete(ctx, keys...); err != nil {
		s.logger.Error("批量删除会话失败",
			logger.Int("count", len(sessionIDs)),
			logger.ErrorField(err))
		return err
	}

	s.logger.Info("批量删除会话成功",
		logger.Int("count", len(sessionIDs)))

	return nil
}

func (s *sessionService) GetUserSessions(ctx context.Context, userID string) ([]*SessionData, error) {
	// 注意：此方法需要额外的索引支持
	// 在实际生产环境中，建议使用 Set 或 ZSet 维护用户的会话列表
	s.logger.Warn("GetUserSessions 需要额外的索引支持",
		logger.String("user_id", userID))

	return nil, rediserr.New(rediserr.ErrCodeInternal, "not implemented - requires session index")
}

func (s *sessionService) KickUser(ctx context.Context, userID string) error {
	// 注意：此方法需要额外的索引支持
	s.logger.Warn("KickUser 需要额外的索引支持",
		logger.String("user_id", userID))

	return rediserr.New(rediserr.ErrCodeInternal, "not implemented - requires session index")
}
