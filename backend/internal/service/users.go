package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/photo-nini/backend/internal/domain/fault"
	"github.com/photo-nini/backend/internal/model"
	"github.com/photo-nini/backend/internal/tenant"
)

// UserService 提供当前登录用户资料读取和局部更新，不接受请求体指定用户归属。
type UserService struct {
	users UserRepository
}

// NewUserService 使用与认证服务相同的 repository 接口构造用户资料服务。
func NewUserService(users UserRepository) *UserService {
	return &UserService{users: users}
}

// GetCurrent 仅按 access token 中的用户业务 UUID 查询资料。
func (service *UserService) GetCurrent(ctx context.Context, userID uuid.UUID) (model.User, error) {
	return service.users.FindUserByBusinessID(tenant.System(ctx), userID)
}

// ProfilePatch 保留字段是否出现的信息，以区分省略字段和显式 null 清除头像。
type ProfilePatch struct {
	NicknameSet bool
	Nickname    string
	AvatarSet   bool
	AvatarKey   *string
}

// UpdateCurrent 验证局部字段并只更新请求显式提供的昵称或头像 key。
func (service *UserService) UpdateCurrent(ctx context.Context, userID uuid.UUID, patch ProfilePatch) (model.User, error) {
	updates := make(map[string]any, 2)
	if patch.NicknameSet {
		nickname := strings.TrimSpace(patch.Nickname)
		if length := utf8.RuneCountInString(nickname); length < 1 || length > 64 {
			return model.User{}, fault.InvalidField("nickname", "昵称长度必须为 1 至 64 个字符")
		}
		updates["nickname"] = nickname
	}
	if patch.AvatarSet {
		if patch.AvatarKey != nil && strings.TrimSpace(*patch.AvatarKey) == "" {
			return model.User{}, fault.InvalidField("avatar_key", "头像 key 不能为空；清除头像请传 null")
		}
		if patch.AvatarKey != nil {
			if checker, ok := service.users.(interface {
				CanAccessFile(context.Context, string, uuid.UUID) (bool, error)
			}); ok {
				allowed, err := checker.CanAccessFile(tenant.Personal(ctx, userID), *patch.AvatarKey, userID)
				if err != nil {
					return model.User{}, err
				}
				if !allowed {
					return model.User{}, fault.NotFound()
				}
			}
		}
		updates["avatar_key"] = patch.AvatarKey
	}
	if len(updates) == 0 {
		return model.User{}, fault.InvalidRequest("至少提供 nickname 或 avatar_key 中的一个字段")
	}
	return service.users.UpdateUserProfile(tenant.System(ctx), userID, updates)
}
