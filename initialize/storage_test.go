package initialize

import (
	"path/filepath"
	"testing"

	"InkFlow/global"
	"InkFlow/utils/storage"

	"go.uber.org/zap"
)

func TestDesktopObjectStorageWithoutOSSUsesPrivateLocalDirectory(t *testing.T) {
	previousConfig, previousStorage, previousLogger := global.GVA_CONFIG, global.GVA_OBJECT_STORAGE, global.GVA_LOG
	t.Cleanup(func() {
		global.GVA_CONFIG, global.GVA_OBJECT_STORAGE, global.GVA_LOG = previousConfig, previousStorage, previousLogger
	})
	root := t.TempDir()
	global.GVA_CONFIG.System.Env = "desktop"
	global.GVA_CONFIG.System.DataDir = root
	global.GVA_CONFIG.OSS.AccessKeyID = ""
	global.GVA_CONFIG.OSS.AccessKeySecret = ""
	global.GVA_LOG = zap.NewNop()
	global.GVA_OBJECT_STORAGE = nil
	InitializeObjectStorage()
	if _, ok := global.GVA_OBJECT_STORAGE.(*storage.LocalStorage); !ok {
		t.Fatalf("desktop object storage = %T, want local", global.GVA_OBJECT_STORAGE)
	}
	if local, err := storage.NewLocal(filepath.Join(root, "knowledge-objects")); err != nil || local == nil {
		t.Fatalf("desktop storage directory unavailable: %v", err)
	}
}
