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

// CartItem 购物车商品项
type CartItem struct {
	ProductID string  `json:"product_id"`
	Quantity  int     `json:"quantity"`
	Price     float64 `json:"price"`
	Selected  bool    `json:"selected"`
	AddedAt   int64   `json:"added_at"`
}

// ShoppingCartService 购物车服务接口
// 基于 Redis Hash 实现的购物车管理
// 适用场景：
// - 电商购物车
// - 临时订单
// - 商品收藏夹
// - 比价清单
type ShoppingCartService interface {
	// AddItem 添加商品到购物车
	// 如果商品已存在，增加数量
	AddItem(ctx context.Context, userID string, productID string, quantity int) error

	// RemoveItem 从购物车移除商品
	RemoveItem(ctx context.Context, userID string, productID string) error

	// UpdateQuantity 更新商品数量
	// 如果数量为 0，则删除商品
	UpdateQuantity(ctx context.Context, userID string, productID string, quantity int) error

	// GetQuantity 获取商品数量
	GetQuantity(ctx context.Context, userID string, productID string) (int, error)

	// GetAll 获取购物车所有商品
	// 返回 productID -> quantity 的映射
	GetAll(ctx context.Context, userID string) (map[string]int, error)

	// GetCount 获取购物车商品种类数
	GetCount(ctx context.Context, userID string) (int64, error)

	// Clear 清空购物车
	Clear(ctx context.Context, userID string) error

	// IncrQuantity 增加商品数量
	// delta: 可以为负数（减少数量）
	IncrQuantity(ctx context.Context, userID string, productID string, delta int) (int64, error)

	// Exists 检查商品是否在购物车中
	Exists(ctx context.Context, userID string, productID string) (bool, error)

	// MergeCart 合并购物车
	// 将 fromUserID 的购物车合并到 toUserID
	// 常用于游客转正式用户场景
	MergeCart(ctx context.Context, fromUserID string, toUserID string) error

	// GetTotalQuantity 获取购物车商品总数量
	GetTotalQuantity(ctx context.Context, userID string) (int64, error)

	// BatchAdd 批量添加商品
	BatchAdd(ctx context.Context, userID string, items map[string]int) error

	// BatchRemove 批量移除商品
	BatchRemove(ctx context.Context, userID string, productIDs []string) error

	// SetExpire 设置购物车过期时间
	// 用于临时购物车自动清理
	SetExpire(ctx context.Context, userID string, ttl time.Duration) error
}

type shoppingCartService struct {
	client  client.Client
	logger  logger.Logger
	metrics metrics.Metrics
	prefix  string
}

// NewShoppingCartService 创建购物车服务
func NewShoppingCartService(client client.Client, log logger.Logger, m metrics.Metrics, prefix string) ShoppingCartService {
	if prefix == "" {
		prefix = "cart"
	}
	return &shoppingCartService{
		client:  client,
		logger:  log,
		metrics: m,
		prefix:  prefix,
	}
}

func (s *shoppingCartService) buildKey(userID string) string {
	return fmt.Sprintf("%s:%s", s.prefix, userID)
}

func (s *shoppingCartService) AddItem(ctx context.Context, userID string, productID string, quantity int) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("cart.add_item", time.Since(start), true)
	}()

	if userID == "" || productID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID and product ID cannot be empty")
	}

	if quantity <= 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "quantity must be positive")
	}

	key := s.buildKey(userID)

	// 获取当前数量
	currentQty, err := s.client.GetClient().HGet(ctx, key, productID).Int()
	if err != nil && err != redis.Nil {
		s.logger.Error("获取商品数量失败",
			logger.String("user_id", userID),
			logger.String("product_id", productID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "get quantity failed")
	}

	// 增加数量
	newQty := currentQty + quantity
	err = s.client.GetClient().HSet(ctx, key, productID, newQty).Err()
	if err != nil {
		s.logger.Error("添加商品失败",
			logger.String("user_id", userID),
			logger.String("product_id", productID),
			logger.Int("quantity", quantity),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "add item failed")
	}

	s.logger.Info("添加商品到购物车",
		logger.String("user_id", userID),
		logger.String("product_id", productID),
		logger.Int("quantity", quantity),
		logger.Int("new_quantity", newQty))

	return nil
}

func (s *shoppingCartService) RemoveItem(ctx context.Context, userID string, productID string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("cart.remove_item", time.Since(start), true)
	}()

	if userID == "" || productID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID and product ID cannot be empty")
	}

	key := s.buildKey(userID)

	err := s.client.GetClient().HDel(ctx, key, productID).Err()
	if err != nil {
		s.logger.Error("移除商品失败",
			logger.String("user_id", userID),
			logger.String("product_id", productID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "remove item failed")
	}

	s.logger.Info("从购物车移除商品",
		logger.String("user_id", userID),
		logger.String("product_id", productID))

	return nil
}

func (s *shoppingCartService) UpdateQuantity(ctx context.Context, userID string, productID string, quantity int) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("cart.update_quantity", time.Since(start), true)
	}()

	if userID == "" || productID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID and product ID cannot be empty")
	}

	// 如果数量为 0，删除商品
	if quantity <= 0 {
		return s.RemoveItem(ctx, userID, productID)
	}

	key := s.buildKey(userID)

	err := s.client.GetClient().HSet(ctx, key, productID, quantity).Err()
	if err != nil {
		s.logger.Error("更新商品数量失败",
			logger.String("user_id", userID),
			logger.String("product_id", productID),
			logger.Int("quantity", quantity),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "update quantity failed")
	}

	s.logger.Debug("更新商品数量",
		logger.String("user_id", userID),
		logger.String("product_id", productID),
		logger.Int("quantity", quantity))

	return nil
}

func (s *shoppingCartService) GetQuantity(ctx context.Context, userID string, productID string) (int, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("cart.get_quantity", time.Since(start), true)
	}()

	if userID == "" || productID == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID and product ID cannot be empty")
	}

	key := s.buildKey(userID)

	quantity, err := s.client.GetClient().HGet(ctx, key, productID).Int()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		s.logger.Error("获取商品数量失败",
			logger.String("user_id", userID),
			logger.String("product_id", productID),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get quantity failed")
	}

	return quantity, nil
}

func (s *shoppingCartService) GetAll(ctx context.Context, userID string) (map[string]int, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("cart.get_all", time.Since(start), true)
	}()

	if userID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	key := s.buildKey(userID)

	result, err := s.client.GetClient().HGetAll(ctx, key).Result()
	if err != nil {
		s.logger.Error("获取购物车失败",
			logger.String("user_id", userID),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get cart failed")
	}

	cart := make(map[string]int, len(result))
	for productID, qtyStr := range result {
		qty, _ := strconv.Atoi(qtyStr)
		cart[productID] = qty
	}

	return cart, nil
}

func (s *shoppingCartService) GetCount(ctx context.Context, userID string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("cart.get_count", time.Since(start), true)
	}()

	if userID == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	key := s.buildKey(userID)

	count, err := s.client.GetClient().HLen(ctx, key).Result()
	if err != nil {
		s.logger.Error("获取购物车商品数失败",
			logger.String("user_id", userID),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get count failed")
	}

	return count, nil
}

func (s *shoppingCartService) Clear(ctx context.Context, userID string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("cart.clear", time.Since(start), true)
	}()

	if userID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	key := s.buildKey(userID)

	err := s.client.GetClient().Del(ctx, key).Err()
	if err != nil {
		s.logger.Error("清空购物车失败",
			logger.String("user_id", userID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "clear cart failed")
	}

	s.logger.Info("清空购物车", logger.String("user_id", userID))
	return nil
}

func (s *shoppingCartService) IncrQuantity(ctx context.Context, userID string, productID string, delta int) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("cart.incr_quantity", time.Since(start), true)
	}()

	if userID == "" || productID == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID and product ID cannot be empty")
	}

	key := s.buildKey(userID)

	newQty, err := s.client.GetClient().HIncrBy(ctx, key, productID, int64(delta)).Result()
	if err != nil {
		s.logger.Error("增加商品数量失败",
			logger.String("user_id", userID),
			logger.String("product_id", productID),
			logger.Int("delta", delta),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "incr quantity failed")
	}

	// 如果数量变为 0 或负数，删除商品
	if newQty <= 0 {
		s.client.GetClient().HDel(ctx, key, productID)
		return 0, nil
	}

	s.logger.Debug("商品数量变更",
		logger.String("user_id", userID),
		logger.String("product_id", productID),
		logger.Int("delta", delta),
		logger.Int64("new_quantity", newQty))

	return newQty, nil
}

func (s *shoppingCartService) Exists(ctx context.Context, userID string, productID string) (bool, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("cart.exists", time.Since(start), true)
	}()

	if userID == "" || productID == "" {
		return false, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID and product ID cannot be empty")
	}

	key := s.buildKey(userID)

	exists, err := s.client.GetClient().HExists(ctx, key, productID).Result()
	if err != nil {
		return false, rediserr.Wrap(err, rediserr.ErrCodeInternal, "check exists failed")
	}

	return exists, nil
}

func (s *shoppingCartService) MergeCart(ctx context.Context, fromUserID string, toUserID string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("cart.merge", time.Since(start), true)
	}()

	if fromUserID == "" || toUserID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user IDs cannot be empty")
	}

	fromKey := s.buildKey(fromUserID)
	toKey := s.buildKey(toUserID)

	// 获取源购物车
	items, err := s.client.GetClient().HGetAll(ctx, fromKey).Result()
	if err != nil {
		s.logger.Error("获取源购物车失败",
			logger.String("from_user_id", fromUserID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "get source cart failed")
	}

	if len(items) == 0 {
		return nil
	}

	// 合并到目标购物车
	pipe := s.client.GetClient().Pipeline()
	for productID, qtyStr := range items {
		qty, _ := strconv.Atoi(qtyStr)
		pipe.HIncrBy(ctx, toKey, productID, int64(qty))
	}

	_, err = pipe.Exec(ctx)
	if err != nil {
		s.logger.Error("合并购物车失败",
			logger.String("from_user_id", fromUserID),
			logger.String("to_user_id", toUserID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "merge cart failed")
	}

	// 删除源购物车
	s.client.GetClient().Del(ctx, fromKey)

	s.logger.Info("购物车合并成功",
		logger.String("from_user_id", fromUserID),
		logger.String("to_user_id", toUserID),
		logger.Int("item_count", len(items)))

	return nil
}

func (s *shoppingCartService) GetTotalQuantity(ctx context.Context, userID string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("cart.get_total_quantity", time.Since(start), true)
	}()

	if userID == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	items, err := s.GetAll(ctx, userID)
	if err != nil {
		return 0, err
	}

	var total int64
	for _, qty := range items {
		total += int64(qty)
	}

	return total, nil
}

func (s *shoppingCartService) BatchAdd(ctx context.Context, userID string, items map[string]int) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("cart.batch_add", time.Since(start), true)
	}()

	if userID == "" || len(items) == 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID and items cannot be empty")
	}

	key := s.buildKey(userID)

	// 转换为 interface{} 类型
	fields := make(map[string]interface{}, len(items))
	for productID, qty := range items {
		if qty > 0 {
			fields[productID] = qty
		}
	}

	err := s.client.GetClient().HSet(ctx, key, fields).Err()
	if err != nil {
		s.logger.Error("批量添加商品失败",
			logger.String("user_id", userID),
			logger.Int("item_count", len(items)),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "batch add failed")
	}

	s.logger.Info("批量添加商品成功",
		logger.String("user_id", userID),
		logger.Int("item_count", len(items)))

	return nil
}

func (s *shoppingCartService) BatchRemove(ctx context.Context, userID string, productIDs []string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("cart.batch_remove", time.Since(start), true)
	}()

	if userID == "" || len(productIDs) == 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID and product IDs cannot be empty")
	}

	key := s.buildKey(userID)

	err := s.client.GetClient().HDel(ctx, key, productIDs...).Err()
	if err != nil {
		s.logger.Error("批量移除商品失败",
			logger.String("user_id", userID),
			logger.Int("product_count", len(productIDs)),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "batch remove failed")
	}

	s.logger.Info("批量移除商品成功",
		logger.String("user_id", userID),
		logger.Int("product_count", len(productIDs)))

	return nil
}

func (s *shoppingCartService) SetExpire(ctx context.Context, userID string, ttl time.Duration) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("cart.set_expire", time.Since(start), true)
	}()

	if userID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	if ttl <= 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "TTL must be positive")
	}

	key := s.buildKey(userID)

	err := s.client.GetClient().Expire(ctx, key, ttl).Err()
	if err != nil {
		s.logger.Error("设置购物车过期时间失败",
			logger.String("user_id", userID),
			logger.Duration("ttl", ttl),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "set expire failed")
	}

	s.logger.Debug("设置购物车过期时间",
		logger.String("user_id", userID),
		logger.Duration("ttl", ttl))

	return nil
}
