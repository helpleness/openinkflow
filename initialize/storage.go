package initialize

import (
	"errors"
	"path/filepath"
	"strings"

	"InkFlow/global"
	"InkFlow/utils/storage"

	"go.uber.org/zap"
)

// InitializeObjectStorage uses configured OSS when available. Desktop installs
// without OSS credentials keep their knowledge objects beside the local DB.
func InitializeObjectStorage() {
	configured := global.GVA_CONFIG.OSS
	objectStorage, err := storage.NewOSS(storage.OSSConfig{
		Endpoint:        configured.Endpoint,
		Bucket:          configured.Bucket,
		Region:          configured.Region,
		AccessKeyID:     configured.AccessKeyID,
		AccessKeySecret: configured.AccessKeySecret,
	})
	if errors.Is(err, storage.ErrNotConfigured) {
		if strings.EqualFold(global.GVA_CONFIG.System.Env, "desktop") {
			root := filepath.Join(global.GVA_CONFIG.System.DataDir, "knowledge-objects")
			localStorage, localErr := storage.NewLocal(root)
			if localErr != nil {
				panic(localErr)
			}
			global.GVA_OBJECT_STORAGE = localStorage
			global.GVA_LOG.Info("桌面知识库文件存储已初始化", zap.String("path", root))
			return
		}
		global.GVA_LOG.Warn("OSS 未配置，知识库上传和原文件下载不可用", zap.Error(err))
		return
	}
	if err != nil {
		panic(err)
	}
	global.GVA_OBJECT_STORAGE = objectStorage
	global.GVA_LOG.Info("私有 OSS 对象存储已初始化", zap.String("bucket", configured.Bucket), zap.String("region", configured.Region), zap.String("endpoint", configured.Endpoint))
}
