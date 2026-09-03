package officialdoc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"InkFlow/global"
	commonResponse "InkFlow/model/common/response"
	model "InkFlow/model/officialdoc"
	systemModel "InkFlow/model/system"
	"InkFlow/utils/storage"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type fakeObjectStorage struct {
	uploadErr error
	deleteErr error
	signErr   error
	signedURL string
	objects   map[string][]byte
	deleted   []string
	signedKey string
	signedFor time.Duration
}

func (fake *fakeObjectStorage) Upload(ctx context.Context, key string, reader io.Reader, _ int64, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if fake.uploadErr != nil {
		return fake.uploadErr
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if fake.objects == nil {
		fake.objects = map[string][]byte{}
	}
	fake.objects[key] = data
	return nil
}

func (fake *fakeObjectStorage) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fake.deleted = append(fake.deleted, key)
	if fake.deleteErr != nil {
		return fake.deleteErr
	}
	delete(fake.objects, key)
	return nil
}

func (fake *fakeObjectStorage) Exists(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	_, exists := fake.objects[key]
	return exists, nil
}

func (fake *fakeObjectStorage) SignedGetURL(ctx context.Context, key string, expiration time.Duration) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	fake.signedKey = key
	fake.signedFor = expiration
	if fake.signErr != nil {
		return "", fake.signErr
	}
	return fake.signedURL, nil
}

func TestKnowledgeImportReturnsUploadFailureWithoutDatabaseRecord(t *testing.T) {
	db := setupKnowledgeStorageTest(t)
	seedKnowledgeOrganization(t, db, 1, 10, 100)
	global.GVA_OBJECT_STORAGE = &fakeObjectStorage{uploadErr: errors.New("oss unavailable")}

	_, err := (&KnowledgeDocumentService{}).Import(context.Background(), 1, 10, 100, multipartFile(t, "notice.md", []byte("# 通知\n\n正文")))
	if err == nil || !strings.Contains(err.Error(), "上传知识库原文件到 OSS 失败") {
		t.Fatalf("Import() error = %v, want OSS upload failure", err)
	}
	var count int64
	if err := db.Model(&model.KnowledgeDocument{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("documents after failed upload = %d, want 0", count)
	}
}

func TestKnowledgeDownloadSignsObjectForAuthorizedMember(t *testing.T) {
	db := setupKnowledgeStorageTest(t)
	seedKnowledgeOrganization(t, db, 1, 10, 100)
	key := testKnowledgeKey(t, 10)
	document := model.KnowledgeDocument{TenantID: 1, OrganizationID: 10, CreatedBy: 100, Name: "通知", OriginalName: "通知.md", ContentType: "text/markdown", ObjectKey: key, SHA256: strings.Repeat("a", 64), Status: "ready"}
	if err := db.Create(&document).Error; err != nil {
		t.Fatal(err)
	}
	fake := &fakeObjectStorage{signedURL: "https://oss.example/signed"}
	global.GVA_OBJECT_STORAGE = fake

	result, err := (&KnowledgeSearchService{}).DownloadDocument(context.Background(), 1, document.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if result.URL != fake.signedURL || fake.signedKey != key || fake.signedFor != 10*time.Minute {
		t.Fatalf("download result/storage call = %#v, key=%q, expiration=%s", result, fake.signedKey, fake.signedFor)
	}
}

func TestKnowledgeDownloadRejectsMemberFromAnotherOrganization(t *testing.T) {
	db := setupKnowledgeStorageTest(t)
	seedKnowledgeOrganization(t, db, 1, 10, 100)
	if err := db.Create(&systemModel.SysOrganization{TenantID: 1, Name: "其他组织", Code: "org-11"}).Error; err != nil {
		t.Fatal(err)
	}
	document := model.KnowledgeDocument{TenantID: 1, OrganizationID: 11, CreatedBy: 101, Name: "隔离", OriginalName: "隔离.md", ObjectKey: testKnowledgeKey(t, 11), SHA256: strings.Repeat("b", 64), Status: "ready"}
	if err := db.Create(&document).Error; err != nil {
		t.Fatal(err)
	}
	fake := &fakeObjectStorage{signedURL: "https://oss.example/should-not-be-signed"}
	global.GVA_OBJECT_STORAGE = fake

	_, err := (&KnowledgeSearchService{}).DownloadDocument(context.Background(), 1, document.ID, 100)
	if !errors.Is(err, commonResponse.ErrForbidden) {
		t.Fatalf("DownloadDocument() error = %v, want forbidden", err)
	}
	if fake.signedKey != "" {
		t.Fatalf("signed object across organization boundary: %q", fake.signedKey)
	}
}

func TestKnowledgeDeleteKeepsDatabaseRecordWhenOSSDeleteFails(t *testing.T) {
	db := setupKnowledgeStorageTest(t)
	seedKnowledgeOrganization(t, db, 1, 10, 100)
	document := model.KnowledgeDocument{TenantID: 1, OrganizationID: 10, CreatedBy: 100, Name: "通知", OriginalName: "通知.md", ObjectKey: testKnowledgeKey(t, 10), SHA256: strings.Repeat("c", 64), Status: "ready"}
	if err := db.Create(&document).Error; err != nil {
		t.Fatal(err)
	}
	fake := &fakeObjectStorage{deleteErr: errors.New("permission denied by OSS")}
	global.GVA_OBJECT_STORAGE = fake

	err := (&KnowledgeSearchService{}).DeleteDocument(context.Background(), 1, document.ID, 100)
	if err == nil || !strings.Contains(err.Error(), "删除 OSS 文档对象失败") {
		t.Fatalf("DeleteDocument() error = %v, want OSS deletion failure", err)
	}
	var stored model.KnowledgeDocument
	if err := db.First(&stored, document.ID).Error; err != nil {
		t.Fatalf("database record should remain for retry: %v", err)
	}
	if stored.Status != "delete_failed" || len(fake.deleted) != 1 {
		t.Fatalf("status=%q deleted=%v, want delete_failed and one OSS attempt", stored.Status, fake.deleted)
	}
}

func setupKnowledgeStorageTest(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:knowledge-storage-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&systemModel.SysOrganization{}, &systemModel.SysMembership{}, &model.KnowledgeDocument{}, &model.KnowledgeChunk{}, &model.KnowledgeImage{}); err != nil {
		t.Fatal(err)
	}
	previousDB, previousStorage, previousConfig, previousVector := global.GVA_DB, global.GVA_OBJECT_STORAGE, global.GVA_CONFIG, global.GVA_VECTOR_STORE
	global.GVA_DB = db
	global.GVA_CONFIG.OSS.SignedURLExpire = "10m"
	global.GVA_VECTOR_STORE = nil
	t.Cleanup(func() {
		global.GVA_DB = previousDB
		global.GVA_OBJECT_STORAGE = previousStorage
		global.GVA_CONFIG = previousConfig
		global.GVA_VECTOR_STORE = previousVector
	})
	return db
}

func seedKnowledgeOrganization(t *testing.T, db *gorm.DB, tenantID, organizationID, userID uint) {
	t.Helper()
	organization := systemModel.SysOrganization{TenantID: tenantID, Name: "测试组织", Code: fmt.Sprintf("org-%d", organizationID)}
	organization.ID = organizationID
	if err := db.Create(&organization).Error; err != nil {
		t.Fatal(err)
	}
	membership := systemModel.SysMembership{TenantID: tenantID, OrganizationID: organizationID, UserID: userID, RoleID: 1, Status: systemModel.UserStatusActive}
	if err := db.Create(&membership).Error; err != nil {
		t.Fatal(err)
	}
}

func testKnowledgeKey(t *testing.T, organizationID uint) string {
	t.Helper()
	key, err := storage.BuildKnowledgeObjectKey(organizationID, "notice.md", time.Date(2026, time.September, 2, 0, 0, 0, 0, time.UTC), "550e8400-e29b-41d4-a716-446655440000")
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func multipartFile(t *testing.T, filename string, data []byte) *multipart.FileHeader {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	header.Set("Content-Type", "text/markdown")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	form, err := multipart.NewReader(&body, writer.Boundary()).ReadForm(int64(body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = form.RemoveAll() })
	return form.File["file"][0]
}
