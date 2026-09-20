// Package securestore 将凭据保存在 SQLite、YAML 与浏览器存储之外。
// 桌面端认证仅用本包保存远端访问令牌与刷新令牌。
package securestore

import "errors"

var (
	// ErrNotFound 表示当前操作系统用户尚未保存该凭据。
	ErrNotFound = errors.New("secure credential not found")
	// ErrUnsupported 表示当前构建没有原生安全存储后端。
	ErrUnsupported = errors.New("secure credential storage is not supported on this platform")
)

// Store 特意只暴露不透明字节存储。调用方需自行序列化令牌包，绝不能将其用于普通
// 应用数据。
type Store interface {
	Load(key string) ([]byte, error)
	Save(key string, value []byte) error
	Delete(key string) error
}
