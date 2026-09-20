package officialdoc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"InkFlow/global"
	model "InkFlow/model/officialdoc"
	"InkFlow/utils/storage"

	"go.uber.org/zap"
)

func knowledgeObjectStorage() (storage.ObjectStorage, error) {
	if global.GVA_OBJECT_STORAGE == nil {
		return nil, fmt.Errorf("知识库对象存储不可用: %w", storage.ErrNotConfigured)
	}
	return global.GVA_OBJECT_STORAGE, nil
}

func knowledgeNow() time.Time { return time.Now() }

func knowledgeSignedURLExpiration() (time.Duration, error) {
	raw := strings.TrimSpace(global.GVA_CONFIG.OSS.SignedURLExpire)
	if raw == "" {
		raw = "10m"
	}
	expiration, err := time.ParseDuration(raw)
	if err != nil || expiration <= 0 {
		return 0, fmt.Errorf("oss.signed-url-expire 必须是正的 Go duration（例如 10m）")
	}
	return expiration, nil
}

func markKnowledgeDocumentProcessingFailed(ctx context.Context, document *model.KnowledgeDocument, cause error) (*model.KnowledgeDocument, error) {
	if document == nil {
		return nil, cause
	}
	reason := strings.TrimSpace(cause.Error())
	if len([]rune(reason)) > 2000 {
		reason = string([]rune(reason)[:2000])
	}
	document.Status = "processing_failed"
	document.FailureReason = reason
	if err := global.GVA_DB.WithContext(ctx).Model(document).Updates(map[string]any{"status": document.Status, "failure_reason": reason, "indexed_at": nil}).Error; err != nil {
		return document, fmt.Errorf("记录文档处理失败原因失败: %w", err)
	}
	return document, nil
}

// rollbackKnowledgeObjects is only used for objects that have no durable DB
// reference yet (or for partial embedded-image uploads). A failed cleanup is
// logged with object identity so it can be retried by operations staff.
func rollbackKnowledgeObjects(ctx context.Context, objectStore storage.ObjectStorage, document *model.KnowledgeDocument, keys []string, stage string) {
	if objectStore == nil {
		return
	}
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if err := objectStore.Delete(ctx, key); err != nil {
			logKnowledgeStorageFailure("rollback", stage, document, key, err)
		}
	}
}

func logKnowledgeStorageFailure(operation, stage string, document *model.KnowledgeDocument, key string, err error) {
	if global.GVA_LOG == nil {
		return
	}
	fields := []zap.Field{zap.String("operation", operation), zap.String("stage", stage), zap.String("object_key", key), zap.Error(err)}
	if document != nil {
		fields = append(fields, zap.Uint("document_id", document.ID), zap.Uint("tenant_id", document.TenantID), zap.Uint("organization_id", document.OrganizationID))
	}
	global.GVA_LOG.Error("knowledge object storage operation failed", fields...)
}

func markKnowledgeDocumentDeletionFailed(ctx context.Context, document *model.KnowledgeDocument, cause error) {
	if document == nil || global.GVA_DB == nil {
		return
	}
	reason := "删除补偿待重试：" + strings.TrimSpace(cause.Error())
	if len([]rune(reason)) > 2000 {
		reason = string([]rune(reason)[:2000])
	}
	if err := global.GVA_DB.WithContext(ctx).Model(document).Updates(map[string]any{"status": "delete_failed", "failure_reason": reason}).Error; err != nil && !errors.Is(err, context.Canceled) {
		logKnowledgeStorageFailure("mark_delete_failed", "database", document, document.ObjectKey, err)
	}
}
