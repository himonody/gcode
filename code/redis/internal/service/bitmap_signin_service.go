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

// SignInService 签到服务接口
// 基于 Redis Bitmap 实现的签到统计
// 适用场景：
// - 用户签到打卡
// - 每日活跃统计
// - 连续签到奖励
// - 月度/年度签到统计
// - 出勤记录
type SignInService interface {
	// SignIn 用户签到
	// date: 签到日期，如果为 nil 则使用当前日期
	SignIn(ctx context.Context, userID string, date *time.Time) error

	// CheckSignIn 检查用户是否已签到
	CheckSignIn(ctx context.Context, userID string, date *time.Time) (bool, error)

	// GetMonthSignInCount 获取用户本月签到天数
	GetMonthSignInCount(ctx context.Context, userID string, year int, month int) (int64, error)

	// GetYearSignInCount 获取用户本年签到天数
	GetYearSignInCount(ctx context.Context, userID string, year int) (int64, error)

	// GetContinuousSignInDays 获取连续签到天数
	// 从今天往前计算连续签到的天数
	GetContinuousSignInDays(ctx context.Context, userID string) (int, error)

	// GetMonthSignInDetails 获取用户本月签到详情
	// 返回每天的签到状态（true/false）
	GetMonthSignInDetails(ctx context.Context, userID string, year int, month int) (map[int]bool, error)

	// GetFirstSignInDate 获取用户首次签到日期
	GetFirstSignInDate(ctx context.Context, userID string, year int, month int) (*time.Time, error)

	// GetSignInDates 获取指定时间范围内的签到日期列表
	GetSignInDates(ctx context.Context, userID string, startDate, endDate time.Time) ([]time.Time, error)

	// CancelSignIn 取消签到（补签或纠错）
	CancelSignIn(ctx context.Context, userID string, date *time.Time) error

	// GetSignInRate 获取签到率
	// 返回签到天数/总天数的百分比
	GetSignInRate(ctx context.Context, userID string, year int, month int) (float64, error)
}

type signInService struct {
	client  client.Client
	logger  logger.Logger
	metrics metrics.Metrics
	prefix  string
}

// NewSignInService 创建签到服务
func NewSignInService(client client.Client, log logger.Logger, m metrics.Metrics, prefix string) SignInService {
	if prefix == "" {
		prefix = "signin"
	}
	return &signInService{
		client:  client,
		logger:  log,
		metrics: m,
		prefix:  prefix,
	}
}

// buildKey 构建签到键
// 格式: signin:user:{userID}:{year}:{month}
func (s *signInService) buildKey(userID string, year int, month int) string {
	return fmt.Sprintf("%s:user:%s:%d:%02d", s.prefix, userID, year, month)
}

// getDayOffset 获取日期在月份中的偏移量（从0开始）
func (s *signInService) getDayOffset(date time.Time) int64 {
	return int64(date.Day() - 1)
}

func (s *signInService) SignIn(ctx context.Context, userID string, date *time.Time) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("signin.sign_in", time.Since(start), true)
	}()

	if userID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	signDate := time.Now()
	if date != nil {
		signDate = *date
	}

	key := s.buildKey(userID, signDate.Year(), int(signDate.Month()))
	offset := s.getDayOffset(signDate)

	// 设置对应位为 1
	err := s.client.GetClient().SetBit(ctx, key, offset, 1).Err()
	if err != nil {
		s.logger.Error("签到失败",
			logger.String("user_id", userID),
			logger.String("date", signDate.Format("2006-01-02")),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "sign in failed")
	}

	s.logger.Info("用户签到成功",
		logger.String("user_id", userID),
		logger.String("date", signDate.Format("2006-01-02")))

	return nil
}

func (s *signInService) CheckSignIn(ctx context.Context, userID string, date *time.Time) (bool, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("signin.check_sign_in", time.Since(start), true)
	}()

	if userID == "" {
		return false, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	checkDate := time.Now()
	if date != nil {
		checkDate = *date
	}

	key := s.buildKey(userID, checkDate.Year(), int(checkDate.Month()))
	offset := s.getDayOffset(checkDate)

	bit, err := s.client.GetClient().GetBit(ctx, key, offset).Result()
	if err != nil {
		s.logger.Error("检查签到状态失败",
			logger.String("user_id", userID),
			logger.String("date", checkDate.Format("2006-01-02")),
			logger.ErrorField(err))
		return false, rediserr.Wrap(err, rediserr.ErrCodeInternal, "check sign in failed")
	}

	return bit == 1, nil
}

func (s *signInService) GetMonthSignInCount(ctx context.Context, userID string, year int, month int) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("signin.get_month_count", time.Since(start), true)
	}()

	if userID == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	key := s.buildKey(userID, year, month)

	count, err := s.client.GetClient().BitCount(ctx, key, nil).Result()
	if err != nil {
		s.logger.Error("获取月度签到天数失败",
			logger.String("user_id", userID),
			logger.Int("year", year),
			logger.Int("month", month),
			logger.ErrorField(err))
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get month count failed")
	}

	return count, nil
}

func (s *signInService) GetYearSignInCount(ctx context.Context, userID string, year int) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("signin.get_year_count", time.Since(start), true)
	}()

	if userID == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	var totalCount int64
	for month := 1; month <= 12; month++ {
		count, err := s.GetMonthSignInCount(ctx, userID, year, month)
		if err != nil {
			return 0, err
		}
		totalCount += count
	}

	return totalCount, nil
}

func (s *signInService) GetContinuousSignInDays(ctx context.Context, userID string) (int, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("signin.get_continuous_days", time.Since(start), true)
	}()

	if userID == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	continuousDays := 0
	currentDate := time.Now()

	// 从今天往前检查
	for i := 0; i < 365; i++ { // 最多检查一年
		checkDate := currentDate.AddDate(0, 0, -i)

		isSigned, err := s.CheckSignIn(ctx, userID, &checkDate)
		if err != nil {
			return 0, err
		}

		if !isSigned {
			break
		}

		continuousDays++
	}

	return continuousDays, nil
}

func (s *signInService) GetMonthSignInDetails(ctx context.Context, userID string, year int, month int) (map[int]bool, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("signin.get_month_details", time.Since(start), true)
	}()

	if userID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	// 获取该月的天数
	firstDay := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.Local)
	lastDay := firstDay.AddDate(0, 1, -1)
	daysInMonth := lastDay.Day()

	details := make(map[int]bool, daysInMonth)

	for day := 1; day <= daysInMonth; day++ {
		checkDate := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local)
		isSigned, err := s.CheckSignIn(ctx, userID, &checkDate)
		if err != nil {
			return nil, err
		}
		details[day] = isSigned
	}

	return details, nil
}

func (s *signInService) GetFirstSignInDate(ctx context.Context, userID string, year int, month int) (*time.Time, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("signin.get_first_date", time.Since(start), true)
	}()

	if userID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	firstDay := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.Local)
	lastDay := firstDay.AddDate(0, 1, -1)
	daysInMonth := lastDay.Day()

	for day := 1; day <= daysInMonth; day++ {
		checkDate := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local)
		isSigned, err := s.CheckSignIn(ctx, userID, &checkDate)
		if err != nil {
			return nil, err
		}
		if isSigned {
			return &checkDate, nil
		}
	}

	return nil, nil
}

func (s *signInService) GetSignInDates(ctx context.Context, userID string, startDate, endDate time.Time) ([]time.Time, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("signin.get_signin_dates", time.Since(start), true)
	}()

	if userID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	if endDate.Before(startDate) {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "end date must be after start date")
	}

	var signInDates []time.Time
	currentDate := startDate

	for !currentDate.After(endDate) {
		isSigned, err := s.CheckSignIn(ctx, userID, &currentDate)
		if err != nil {
			return nil, err
		}

		if isSigned {
			signInDates = append(signInDates, currentDate)
		}

		currentDate = currentDate.AddDate(0, 0, 1)
	}

	return signInDates, nil
}

func (s *signInService) CancelSignIn(ctx context.Context, userID string, date *time.Time) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("signin.cancel_sign_in", time.Since(start), true)
	}()

	if userID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	cancelDate := time.Now()
	if date != nil {
		cancelDate = *date
	}

	key := s.buildKey(userID, cancelDate.Year(), int(cancelDate.Month()))
	offset := s.getDayOffset(cancelDate)

	// 设置对应位为 0
	err := s.client.GetClient().SetBit(ctx, key, offset, 0).Err()
	if err != nil {
		s.logger.Error("取消签到失败",
			logger.String("user_id", userID),
			logger.String("date", cancelDate.Format("2006-01-02")),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "cancel sign in failed")
	}

	s.logger.Info("取消签到成功",
		logger.String("user_id", userID),
		logger.String("date", cancelDate.Format("2006-01-02")))

	return nil
}

func (s *signInService) GetSignInRate(ctx context.Context, userID string, year int, month int) (float64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("signin.get_signin_rate", time.Since(start), true)
	}()

	if userID == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	// 获取签到天数
	signInCount, err := s.GetMonthSignInCount(ctx, userID, year, month)
	if err != nil {
		return 0, err
	}

	// 获取该月总天数
	firstDay := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.Local)
	lastDay := firstDay.AddDate(0, 1, -1)
	totalDays := float64(lastDay.Day())

	if totalDays == 0 {
		return 0, nil
	}

	rate := float64(signInCount) / totalDays * 100

	return rate, nil
}
