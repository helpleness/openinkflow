package initialize

import (
	"errors"

	"InkFlow/global"
	"InkFlow/utils/storage"

	"go.uber.org/zap"
)

// InitializeObjectStorage creates one OSS client for the process. Missing OSS
// settings do not prevent a desktop/local server from starting, but uploads are
// explicitly rejected until private storage has been configured.
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
		global.GVA_LOG.Warn("OSS 未配置，知识库上传和原文件下载不可用", zap.Error(err))
		return
	}
	if err != nil {
		panic(err)
	}
	global.GVA_OBJECT_STORAGE = objectStorage
	global.GVA_LOG.Info("私有 OSS 对象存储已初始化", zap.String("bucket", configured.Bucket), zap.String("region", configured.Region), zap.String("endpoint", configured.Endpoint))
}
