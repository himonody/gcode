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

// SocialGraphService 社交关系服务接口
// 基于 Redis Set 实现的社交关系图
// 适用场景：
// - 好友关系管理
// - 关注/粉丝系统
// - 共同好友推荐
// - 社交网络分析
// - 可能认识的人
type SocialGraphService interface {
	// AddFriend 添加好友
	// 双向关系：同时添加 A->B 和 B->A
	AddFriend(ctx context.Context, userID string, friendID string) error

	// AddFollowing 添加关注（单向）
	// userID 关注 targetID
	AddFollowing(ctx context.Context, userID string, targetID string) error

	// RemoveFriend 删除好友
	// 双向删除
	RemoveFriend(ctx context.Context, userID string, friendID string) error

	// RemoveFollowing 取消关注
	RemoveFollowing(ctx context.Context, userID string, targetID string) error

	// IsFriend 检查是否是好友
	IsFriend(ctx context.Context, userID string, friendID string) (bool, error)

	// IsFollowing 检查是否关注
	IsFollowing(ctx context.Context, userID string, targetID string) (bool, error)

	// GetFriends 获取好友列表
	GetFriends(ctx context.Context, userID string) ([]string, error)

	// GetFollowing 获取关注列表
	GetFollowing(ctx context.Context, userID string) ([]string, error)

	// GetFollowers 获取粉丝列表
	GetFollowers(ctx context.Context, userID string) ([]string, error)

	// GetFriendCount 获取好友数量
	GetFriendCount(ctx context.Context, userID string) (int64, error)

	// GetFollowingCount 获取关注数量
	GetFollowingCount(ctx context.Context, userID string) (int64, error)

	// GetFollowerCount 获取粉丝数量
	GetFollowerCount(ctx context.Context, userID string) (int64, error)

	// GetCommonFriends 获取共同好友
	GetCommonFriends(ctx context.Context, userID string, otherUserID string) ([]string, error)

	// GetCommonFriendsCount 获取共同好友数量
	GetCommonFriendsCount(ctx context.Context, userID string, otherUserID string) (int64, error)

	// GetCommonFriendsMultiple 获取多人的共同好友
	GetCommonFriendsMultiple(ctx context.Context, userIDs ...string) ([]string, error)

	// GetMutualFollowing 获取互相关注的用户
	// 返回既关注又被关注的用户
	GetMutualFollowing(ctx context.Context, userID string) ([]string, error)

	// GetUniqueFriends 获取独有好友
	// 返回 userID 有但 otherUserID 没有的好友
	GetUniqueFriends(ctx context.Context, userID string, otherUserID string) ([]string, error)

	// GetAllFriends 获取所有相关好友
	// 返回两个用户的所有好友（并集）
	GetAllFriends(ctx context.Context, userID string, otherUserID string) ([]string, error)

	// MayKnow 可能认识的人
	// 基于共同好友推荐
	MayKnow(ctx context.Context, userID string, limit int) ([]string, error)

	// GetSecondDegreeFriends 获取二度好友
	// 好友的好友
	GetSecondDegreeFriends(ctx context.Context, userID string) ([]string, error)

	// BatchAddFriends 批量添加好友
	BatchAddFriends(ctx context.Context, userID string, friendIDs []string) error
}

type socialGraphService struct {
	client  client.Client
	logger  logger.Logger
	metrics metrics.Metrics
	prefix  string
}

// NewSocialGraphService 创建社交关系服务
func NewSocialGraphService(client client.Client, log logger.Logger, m metrics.Metrics, prefix string) SocialGraphService {
	if prefix == "" {
		prefix = "social"
	}
	return &socialGraphService{
		client:  client,
		logger:  log,
		metrics: m,
		prefix:  prefix,
	}
}

func (s *socialGraphService) buildFriendsKey(userID string) string {
	return fmt.Sprintf("%s:friends:%s", s.prefix, userID)
}

func (s *socialGraphService) buildFollowingKey(userID string) string {
	return fmt.Sprintf("%s:following:%s", s.prefix, userID)
}

func (s *socialGraphService) buildFollowersKey(userID string) string {
	return fmt.Sprintf("%s:followers:%s", s.prefix, userID)
}

func (s *socialGraphService) AddFriend(ctx context.Context, userID string, friendID string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("social.add_friend", time.Since(start), true)
	}()

	if userID == "" || friendID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID and friend ID cannot be empty")
	}

	if userID == friendID {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "cannot add self as friend")
	}

	// 双向添加好友关系
	pipe := s.client.GetClient().Pipeline()
	pipe.SAdd(ctx, s.buildFriendsKey(userID), friendID)
	pipe.SAdd(ctx, s.buildFriendsKey(friendID), userID)

	_, err := pipe.Exec(ctx)
	if err != nil {
		s.logger.Error("添加好友失败",
			logger.String("user_id", userID),
			logger.String("friend_id", friendID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "add friend failed")
	}

	s.logger.Info("添加好友成功",
		logger.String("user_id", userID),
		logger.String("friend_id", friendID))

	return nil
}

func (s *socialGraphService) AddFollowing(ctx context.Context, userID string, targetID string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("social.add_following", time.Since(start), true)
	}()

	if userID == "" || targetID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID and target ID cannot be empty")
	}

	if userID == targetID {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "cannot follow self")
	}

	// 单向关注：添加到关注列表和粉丝列表
	pipe := s.client.GetClient().Pipeline()
	pipe.SAdd(ctx, s.buildFollowingKey(userID), targetID)
	pipe.SAdd(ctx, s.buildFollowersKey(targetID), userID)

	_, err := pipe.Exec(ctx)
	if err != nil {
		s.logger.Error("添加关注失败",
			logger.String("user_id", userID),
			logger.String("target_id", targetID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "add following failed")
	}

	s.logger.Info("添加关注成功",
		logger.String("user_id", userID),
		logger.String("target_id", targetID))

	return nil
}

func (s *socialGraphService) RemoveFriend(ctx context.Context, userID string, friendID string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("social.remove_friend", time.Since(start), true)
	}()

	if userID == "" || friendID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID and friend ID cannot be empty")
	}

	// 双向删除好友关系
	pipe := s.client.GetClient().Pipeline()
	pipe.SRem(ctx, s.buildFriendsKey(userID), friendID)
	pipe.SRem(ctx, s.buildFriendsKey(friendID), userID)

	_, err := pipe.Exec(ctx)
	if err != nil {
		s.logger.Error("删除好友失败",
			logger.String("user_id", userID),
			logger.String("friend_id", friendID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "remove friend failed")
	}

	s.logger.Info("删除好友成功",
		logger.String("user_id", userID),
		logger.String("friend_id", friendID))

	return nil
}

func (s *socialGraphService) RemoveFollowing(ctx context.Context, userID string, targetID string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("social.remove_following", time.Since(start), true)
	}()

	if userID == "" || targetID == "" {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID and target ID cannot be empty")
	}

	// 取消关注
	pipe := s.client.GetClient().Pipeline()
	pipe.SRem(ctx, s.buildFollowingKey(userID), targetID)
	pipe.SRem(ctx, s.buildFollowersKey(targetID), userID)

	_, err := pipe.Exec(ctx)
	if err != nil {
		s.logger.Error("取消关注失败",
			logger.String("user_id", userID),
			logger.String("target_id", targetID),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "remove following failed")
	}

	s.logger.Info("取消关注成功",
		logger.String("user_id", userID),
		logger.String("target_id", targetID))

	return nil
}

func (s *socialGraphService) IsFriend(ctx context.Context, userID string, friendID string) (bool, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("social.is_friend", time.Since(start), true)
	}()

	if userID == "" || friendID == "" {
		return false, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID and friend ID cannot be empty")
	}

	key := s.buildFriendsKey(userID)

	exists, err := s.client.GetClient().SIsMember(ctx, key, friendID).Result()
	if err != nil {
		return false, rediserr.Wrap(err, rediserr.ErrCodeInternal, "is friend failed")
	}

	return exists, nil
}

func (s *socialGraphService) IsFollowing(ctx context.Context, userID string, targetID string) (bool, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("social.is_following", time.Since(start), true)
	}()

	if userID == "" || targetID == "" {
		return false, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID and target ID cannot be empty")
	}

	key := s.buildFollowingKey(userID)

	exists, err := s.client.GetClient().SIsMember(ctx, key, targetID).Result()
	if err != nil {
		return false, rediserr.Wrap(err, rediserr.ErrCodeInternal, "is following failed")
	}

	return exists, nil
}

func (s *socialGraphService) GetFriends(ctx context.Context, userID string) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("social.get_friends", time.Since(start), true)
	}()

	if userID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	key := s.buildFriendsKey(userID)

	friends, err := s.client.GetClient().SMembers(ctx, key).Result()
	if err != nil {
		s.logger.Error("获取好友列表失败",
			logger.String("user_id", userID),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get friends failed")
	}

	return friends, nil
}

func (s *socialGraphService) GetFollowing(ctx context.Context, userID string) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("social.get_following", time.Since(start), true)
	}()

	if userID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	key := s.buildFollowingKey(userID)

	following, err := s.client.GetClient().SMembers(ctx, key).Result()
	if err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get following failed")
	}

	return following, nil
}

func (s *socialGraphService) GetFollowers(ctx context.Context, userID string) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("social.get_followers", time.Since(start), true)
	}()

	if userID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	key := s.buildFollowersKey(userID)

	followers, err := s.client.GetClient().SMembers(ctx, key).Result()
	if err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get followers failed")
	}

	return followers, nil
}

func (s *socialGraphService) GetFriendCount(ctx context.Context, userID string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("social.get_friend_count", time.Since(start), true)
	}()

	if userID == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	key := s.buildFriendsKey(userID)

	count, err := s.client.GetClient().SCard(ctx, key).Result()
	if err != nil {
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get friend count failed")
	}

	return count, nil
}

func (s *socialGraphService) GetFollowingCount(ctx context.Context, userID string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("social.get_following_count", time.Since(start), true)
	}()

	if userID == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	key := s.buildFollowingKey(userID)

	count, err := s.client.GetClient().SCard(ctx, key).Result()
	if err != nil {
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get following count failed")
	}

	return count, nil
}

func (s *socialGraphService) GetFollowerCount(ctx context.Context, userID string) (int64, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("social.get_follower_count", time.Since(start), true)
	}()

	if userID == "" {
		return 0, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	key := s.buildFollowersKey(userID)

	count, err := s.client.GetClient().SCard(ctx, key).Result()
	if err != nil {
		return 0, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get follower count failed")
	}

	return count, nil
}

func (s *socialGraphService) GetCommonFriends(ctx context.Context, userID string, otherUserID string) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("social.get_common_friends", time.Since(start), true)
	}()

	if userID == "" || otherUserID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "user IDs cannot be empty")
	}

	key1 := s.buildFriendsKey(userID)
	key2 := s.buildFriendsKey(otherUserID)

	commonFriends, err := s.client.GetClient().SInter(ctx, key1, key2).Result()
	if err != nil {
		s.logger.Error("获取共同好友失败",
			logger.String("user_id", userID),
			logger.String("other_user_id", otherUserID),
			logger.ErrorField(err))
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get common friends failed")
	}

	return commonFriends, nil
}

func (s *socialGraphService) GetCommonFriendsCount(ctx context.Context, userID string, otherUserID string) (int64, error) {
	commonFriends, err := s.GetCommonFriends(ctx, userID, otherUserID)
	if err != nil {
		return 0, err
	}
	return int64(len(commonFriends)), nil
}

func (s *socialGraphService) GetCommonFriendsMultiple(ctx context.Context, userIDs ...string) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("social.get_common_friends_multiple", time.Since(start), true)
	}()

	if len(userIDs) == 0 {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "user IDs cannot be empty")
	}

	keys := make([]string, len(userIDs))
	for i, id := range userIDs {
		keys[i] = s.buildFriendsKey(id)
	}

	commonFriends, err := s.client.GetClient().SInter(ctx, keys...).Result()
	if err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get common friends multiple failed")
	}

	return commonFriends, nil
}

func (s *socialGraphService) GetMutualFollowing(ctx context.Context, userID string) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("social.get_mutual_following", time.Since(start), true)
	}()

	if userID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	followingKey := s.buildFollowingKey(userID)
	followersKey := s.buildFollowersKey(userID)

	// 求交集：既关注又被关注
	mutual, err := s.client.GetClient().SInter(ctx, followingKey, followersKey).Result()
	if err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get mutual following failed")
	}

	return mutual, nil
}

func (s *socialGraphService) GetUniqueFriends(ctx context.Context, userID string, otherUserID string) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("social.get_unique_friends", time.Since(start), true)
	}()

	if userID == "" || otherUserID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "user IDs cannot be empty")
	}

	key1 := s.buildFriendsKey(userID)
	key2 := s.buildFriendsKey(otherUserID)

	// 求差集：userID 有但 otherUserID 没有的好友
	uniqueFriends, err := s.client.GetClient().SDiff(ctx, key1, key2).Result()
	if err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get unique friends failed")
	}

	return uniqueFriends, nil
}

func (s *socialGraphService) GetAllFriends(ctx context.Context, userID string, otherUserID string) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("social.get_all_friends", time.Since(start), true)
	}()

	if userID == "" || otherUserID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "user IDs cannot be empty")
	}

	key1 := s.buildFriendsKey(userID)
	key2 := s.buildFriendsKey(otherUserID)

	// 求并集
	allFriends, err := s.client.GetClient().SUnion(ctx, key1, key2).Result()
	if err != nil {
		return nil, rediserr.Wrap(err, rediserr.ErrCodeInternal, "get all friends failed")
	}

	return allFriends, nil
}

func (s *socialGraphService) MayKnow(ctx context.Context, userID string, limit int) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("social.may_know", time.Since(start), true)
	}()

	if userID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	// 获取用户的好友列表
	friends, err := s.GetFriends(ctx, userID)
	if err != nil {
		return nil, err
	}

	// 统计二度好友出现次数
	secondDegreeFriends := make(map[string]int)

	for _, friendID := range friends {
		// 获取好友的好友
		friendsOfFriend, err := s.GetFriends(ctx, friendID)
		if err != nil {
			continue
		}

		for _, fof := range friendsOfFriend {
			// 排除自己和已经是好友的人
			if fof == userID {
				continue
			}

			isFriend, _ := s.IsFriend(ctx, userID, fof)
			if isFriend {
				continue
			}

			secondDegreeFriends[fof]++
		}
	}

	// 按出现次数排序（简单实现：取前N个）
	recommendations := make([]string, 0, limit)
	for userID := range secondDegreeFriends {
		recommendations = append(recommendations, userID)
		if len(recommendations) >= limit {
			break
		}
	}

	return recommendations, nil
}

func (s *socialGraphService) GetSecondDegreeFriends(ctx context.Context, userID string) ([]string, error) {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("social.get_second_degree_friends", time.Since(start), true)
	}()

	if userID == "" {
		return nil, rediserr.New(rediserr.ErrCodeInvalidInput, "user ID cannot be empty")
	}

	friends, err := s.GetFriends(ctx, userID)
	if err != nil {
		return nil, err
	}

	secondDegree := make(map[string]bool)

	for _, friendID := range friends {
		friendsOfFriend, err := s.GetFriends(ctx, friendID)
		if err != nil {
			continue
		}

		for _, fof := range friendsOfFriend {
			if fof != userID {
				secondDegree[fof] = true
			}
		}
	}

	result := make([]string, 0, len(secondDegree))
	for id := range secondDegree {
		result = append(result, id)
	}

	return result, nil
}

func (s *socialGraphService) BatchAddFriends(ctx context.Context, userID string, friendIDs []string) error {
	start := time.Now()
	defer func() {
		s.metrics.RecordOperation("social.batch_add_friends", time.Since(start), true)
	}()

	if userID == "" || len(friendIDs) == 0 {
		return rediserr.New(rediserr.ErrCodeInvalidInput, "user ID and friend IDs cannot be empty")
	}

	pipe := s.client.GetClient().Pipeline()
	userKey := s.buildFriendsKey(userID)

	// 添加到用户的好友列表
	for _, friendID := range friendIDs {
		if friendID == userID {
			continue
		}
		pipe.SAdd(ctx, userKey, friendID)
		pipe.SAdd(ctx, s.buildFriendsKey(friendID), userID)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		s.logger.Error("批量添加好友失败",
			logger.String("user_id", userID),
			logger.Int("count", len(friendIDs)),
			logger.ErrorField(err))
		return rediserr.Wrap(err, rediserr.ErrCodeInternal, "batch add friends failed")
	}

	s.logger.Info("批量添加好友成功",
		logger.String("user_id", userID),
		logger.Int("count", len(friendIDs)))

	return nil
}
