package system

import (
	"InkFlow/global"
	"context"
	"errors"
	"fmt"
	"strings"

	model "InkFlow/model/system"
	response "InkFlow/model/system/response"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// SysUserService owns global user-directory operations.
type SysUserService struct{}

// RegisterLocal creates an unassigned local user and a login session.
func (s *SysAuthService) RegisterLocal(ctx context.Context, username, password string) (*response.SysAuthResult, error) {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 {
		return nil, errors.New("用户名长度必须为 3 到 64 个字符")
	}
	if err := validateLocalPassword(username, password); err != nil {
		return nil, err
	}

	db := global.GVA_DB
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash local password: %w", err)
	}
	user := &model.SysUser{Username: username, LocalPasswordHash: string(hash), Status: model.UserStatusActive}
	if err := db.WithContext(ctx).Create(user).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, errors.New("用户名已存在")
		}
		return nil, err
	}
	// 注册不创建组织或角色关系；所有者在前端按需分配组织与角色。
	return s.completePrimaryAuthentication(ctx, user)
}

// LoginLocal 验证本地账号密码并创建登录会话。
func (s *SysAuthService) LoginLocal(ctx context.Context, username, password string) (*response.SysAuthResult, error) {
	return s.BeginLocalLogin(ctx, username, password)
}
