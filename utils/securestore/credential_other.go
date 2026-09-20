//go:build !windows

package securestore

// unsupportedStore 特意不提供文件回退实现：把账号令牌保存到普通文件会悄然破坏
// 桌面端安全边界。
type unsupportedStore struct{}

func New() Store { return unsupportedStore{} }

func (unsupportedStore) Load(string) ([]byte, error) { return nil, ErrUnsupported }
func (unsupportedStore) Save(string, []byte) error   { return ErrUnsupported }
func (unsupportedStore) Delete(string) error         { return ErrUnsupported }
