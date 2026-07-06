package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/leaf/cloud-clip-lite/internal/auth"
	"github.com/leaf/cloud-clip-lite/internal/store"
)

// loginAndGetToken 辅助：创建用户并登录获取 JWT
func loginAndGetToken(t *testing.T, handler http.Handler, st *store.Store, username, password string) string {
	t.Helper()
	ctx := context.Background()
	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash(password)
	st.CreateUser(ctx, &store.User{Username: username, PasswordHash: hash})

	rec := doRequest(handler, "POST", "/api/auth/login", loginRequest{
		Username: username,
		Password: password,
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("登录失败: %d, %s", rec.Code, rec.Body.String())
	}
	resp := parseJSON(t, rec)
	return resp["access_token"].(string)
}

// --- 文本上传测试 ---

func TestClip_CreateText(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	token := loginAndGetToken(t, handler, st, "clipuser1", "password123")

	rec := doRequest(handler, "POST", "/api/clip", createTextRequest{
		Type: "text",
		Text: "hello clipboard!",
		Meta: map[string]any{"source": "test"},
	}, token)

	if rec.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d, 期望 201, body: %s", rec.Code, rec.Body.String())
	}

	resp := parseJSON(t, rec)
	if resp["type"] != "text" {
		t.Errorf("type = %v, 期望 text", resp["type"])
	}
	if resp["text"] != "hello clipboard!" {
		t.Errorf("text = %v", resp["text"])
	}
	if resp["sha256"] == nil {
		t.Error("应返回 sha256")
	}
	// 验证 SHA256 正确
	expectedHash := sha256.Sum256([]byte("hello clipboard!"))
	expectedSha := hex.EncodeToString(expectedHash[:])
	if resp["sha256"] != expectedSha {
		t.Errorf("sha256 = %v, 期望 %s", resp["sha256"], expectedSha)
	}
	if resp["size"].(float64) != 16 {
		t.Errorf("size = %v, 期望 16", resp["size"])
	}
	if resp["expires_at"] == nil {
		t.Error("应返回 expires_at")
	}
}

func TestClip_CreateText_Empty(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	token := loginAndGetToken(t, handler, st, "clipempty", "password123")

	rec := doRequest(handler, "POST", "/api/clip", createTextRequest{
		Type: "text",
		Text: "",
	}, token)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d, 期望 400", rec.Code)
	}
}

func TestClip_CreateText_TooLarge(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	ctx := context.Background()
	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("password123")
	u, _ := st.CreateUser(ctx, &store.User{Username: "bigtext", PasswordHash: hash, MaxItemSize: 10})

	jwtMgr := auth.NewJWTManager("test-secret-at-least-32-bytes-long!!!", 15*time.Minute)
	token, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	bigText := strings.Repeat("x", 100)
	rec := doRequest(handler, "POST", "/api/clip", createTextRequest{
		Type: "text",
		Text: bigText,
	}, token)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("状态码 = %d, 期望 413", rec.Code)
	}
}

// --- 文件上传测试 ---

// createMultipartRequest 创建 multipart 文件上传请求
// contentType 为空时使用 application/octet-stream
func createMultipartRequest(handler http.Handler, method, path, token, fieldName, filename string, content []byte, contentType string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	if contentType != "" {
		// 带 Content-Type 的文件字段
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, filename))
		h.Set("Content-Type", contentType)
		fw, _ := mw.CreatePart(h)
		fw.Write(content)
	} else {
		fw, _ := mw.CreateFormFile(fieldName, filename)
		fw.Write(content)
	}
	mw.Close()

	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestClip_CreateFile(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	token := loginAndGetToken(t, handler, st, "fileuser", "password123")

	content := []byte("fake image data for testing")
	rec := createMultipartRequest(handler, "POST", "/api/clip", token, "file", "test.png", content, "image/png")

	if rec.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d, 期望 201, body: %s", rec.Code, rec.Body.String())
	}

	resp := parseJSON(t, rec)
	if resp["type"] != "image" {
		t.Errorf("type = %v, 期望 image", resp["type"])
	}
	if int64(resp["size"].(float64)) != int64(len(content)) {
		t.Errorf("size = %v, 期望 %d", resp["size"], len(content))
	}
	if resp["has_blob"] != true {
		t.Error("应返回 has_blob=true")
	}
	if resp["sha256"] == nil {
		t.Error("应返回 sha256")
	}
	// 验证 SHA256
	expectedHash := sha256.Sum256(content)
	expectedSha := hex.EncodeToString(expectedHash[:])
	if resp["sha256"] != expectedSha {
		t.Errorf("sha256 = %v, 期望 %s", resp["sha256"], expectedSha)
	}
	// 验证 meta 中的 filename
	meta, _ := resp["meta"].(map[string]any)
	if meta["filename"] != "test.png" {
		t.Errorf("filename = %v, 期望 test.png", meta["filename"])
	}
}

func TestClip_CreateFile_TooLarge(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	ctx := context.Background()
	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("password123")
	u, _ := st.CreateUser(ctx, &store.User{Username: "bigfile", PasswordHash: hash, MaxItemSize: 10})

	jwtMgr := auth.NewJWTManager("test-secret-at-least-32-bytes-long!!!", 15*time.Minute)
	token, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	bigContent := make([]byte, 100)
	rec := createMultipartRequest(handler, "POST", "/api/clip", token, "file", "big.bin", bigContent, "application/octet-stream")

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("状态码 = %d, 期望 413", rec.Code)
	}
}

// --- 列表测试 ---

func TestClip_List(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	token := loginAndGetToken(t, handler, st, "listclip", "password123")

	// 创建 3 条文本
	for i := 0; i < 3; i++ {
		doRequest(handler, "POST", "/api/clip", createTextRequest{
			Type: "text",
			Text: fmt.Sprintf("item-%d", i),
		}, token)
	}

	// 查列表
	rec := doRequest(handler, "GET", "/api/clip?limit=2", nil, token)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}

	resp := parseJSON(t, rec)
	items, _ := resp["items"].([]any)
	if len(items) != 2 {
		t.Errorf("返回 %d 条, 期望 2", len(items))
	}
	// cursor 应为最后一条的 ID
	if resp["cursor"].(float64) == 0 {
		t.Error("cursor 不应为 0")
	}
}

func TestClip_List_TypeFilter(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	token := loginAndGetToken(t, handler, st, "filterclip", "password123")

	// 创建 2 文本 + 1 文件
	doRequest(handler, "POST", "/api/clip", createTextRequest{Type: "text", Text: "t1"}, token)
	doRequest(handler, "POST", "/api/clip", createTextRequest{Type: "text", Text: "t2"}, token)
	createMultipartRequest(handler, "POST", "/api/clip", token, "file", "img.png", []byte("img"), "image/png")

	// 只查 text
	rec := doRequest(handler, "GET", "/api/clip?type=text", nil, token)
	resp := parseJSON(t, rec)
	items, _ := resp["items"].([]any)
	if len(items) != 2 {
		t.Errorf("text 过滤返回 %d 条, 期望 2", len(items))
	}

	// 只查 image
	rec = doRequest(handler, "GET", "/api/clip?type=image", nil, token)
	resp = parseJSON(t, rec)
	items, _ = resp["items"].([]any)
	if len(items) != 1 {
		t.Errorf("image 过滤返回 %d 条, 期望 1", len(items))
	}
}

func TestClip_List_CursorPagination(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	token := loginAndGetToken(t, handler, st, "pageclip", "password123")

	for i := 0; i < 5; i++ {
		doRequest(handler, "POST", "/api/clip", createTextRequest{Type: "text", Text: fmt.Sprintf("p%d", i)}, token)
	}

	// 第一页 2 条
	rec := doRequest(handler, "GET", "/api/clip?limit=2", nil, token)
	resp := parseJSON(t, rec)
	items, _ := resp["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("第一页返回 %d 条, 期望 2", len(items))
	}
	cursor := int64(resp["cursor"].(float64))

	// 第二页
	rec = doRequest(handler, "GET", fmt.Sprintf("/api/clip?limit=2&before=%d", cursor), nil, token)
	resp = parseJSON(t, rec)
	items, _ = resp["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("第二页返回 %d 条, 期望 2", len(items))
	}
}

// --- 详情测试 ---

func TestClip_GetByID(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	token := loginAndGetToken(t, handler, st, "detailclip", "password123")

	// 创建一条
	rec := doRequest(handler, "POST", "/api/clip", createTextRequest{Type: "text", Text: "detail-me"}, token)
	createResp := parseJSON(t, rec)
	itemID := int64(createResp["id"].(float64))

	// 查询详情
	rec = doRequest(handler, "GET", fmt.Sprintf("/api/clip/%d", itemID), nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}

	resp := parseJSON(t, rec)
	if resp["text"] != "detail-me" {
		t.Errorf("text = %v, 期望 detail-me", resp["text"])
	}
}

func TestClip_GetByID_NotFound(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	token := loginAndGetToken(t, handler, st, "notfoundclip", "password123")

	rec := doRequest(handler, "GET", "/api/clip/99999", nil, token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, 期望 404", rec.Code)
	}
}

// --- 下载测试 ---

func TestClip_GetContent_Text(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	token := loginAndGetToken(t, handler, st, "dltext", "password123")

	rec := doRequest(handler, "POST", "/api/clip", createTextRequest{Type: "text", Text: "download me"}, token)
	createResp := parseJSON(t, rec)
	itemID := int64(createResp["id"].(float64))

	// 下载内容
	rec = doRequest(handler, "GET", fmt.Sprintf("/api/clip/%d/content", itemID), nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	if rec.Body.String() != "download me" {
		t.Errorf("body = %s, 期望 'download me'", rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %s", rec.Header().Get("Content-Type"))
	}
}

func TestClip_GetContent_File(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	token := loginAndGetToken(t, handler, st, "dlfile", "password123")

	content := []byte("binary file content here")
	rec := createMultipartRequest(handler, "POST", "/api/clip", token, "file", "doc.bin", content, "application/octet-stream")
	createResp := parseJSON(t, rec)
	itemID := int64(createResp["id"].(float64))

	// 下载
	rec = doRequest(handler, "GET", fmt.Sprintf("/api/clip/%d/content", itemID), nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	if !bytes.Equal(rec.Body.Bytes(), content) {
		t.Errorf("下载内容不匹配")
	}
	if rec.Header().Get("Content-Disposition") != "attachment" {
		t.Errorf("Content-Disposition = %s", rec.Header().Get("Content-Disposition"))
	}
}

// --- Latest 测试 ---

func TestClip_GetLatest(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	token := loginAndGetToken(t, handler, st, "latestclip", "password123")

	doRequest(handler, "POST", "/api/clip", createTextRequest{Type: "text", Text: "first"}, token)
	doRequest(handler, "POST", "/api/clip", createTextRequest{Type: "text", Text: "second"}, token)
	doRequest(handler, "POST", "/api/clip", createTextRequest{Type: "text", Text: "third"}, token)

	rec := doRequest(handler, "GET", "/api/clip/latest", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}

	resp := parseJSON(t, rec)
	if resp["text"] != "third" {
		t.Errorf("text = %v, 期望 third", resp["text"])
	}
}

func TestClip_GetLatest_Empty(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	token := loginAndGetToken(t, handler, st, "emptylatest", "password123")

	rec := doRequest(handler, "GET", "/api/clip/latest", nil, token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, 期望 404", rec.Code)
	}
}

// --- 删除测试 ---

func TestClip_Delete(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	token := loginAndGetToken(t, handler, st, "delclip", "password123")

	// 创建一条
	rec := doRequest(handler, "POST", "/api/clip", createTextRequest{Type: "text", Text: "todelete"}, token)
	createResp := parseJSON(t, rec)
	itemID := int64(createResp["id"].(float64))

	// 删除
	rec = doRequest(handler, "DELETE", fmt.Sprintf("/api/clip/%d", itemID), nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}

	// 删除后查不到
	rec = doRequest(handler, "GET", fmt.Sprintf("/api/clip/%d", itemID), nil, token)
	if rec.Code != http.StatusNotFound {
		t.Errorf("删除后应 404, 实际 %d", rec.Code)
	}
}

func TestClip_Delete_NotFound(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	token := loginAndGetToken(t, handler, st, "delnotfound", "password123")

	rec := doRequest(handler, "DELETE", "/api/clip/99999", nil, token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, 期望 404", rec.Code)
	}
}

func TestClip_Delete_File_CleansBlob(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	token := loginAndGetToken(t, handler, st, "delblob", "password123")

	content := []byte("blob to delete")
	rec := createMultipartRequest(handler, "POST", "/api/clip", token, "file", "todelete.bin", content, "application/octet-stream")
	createResp := parseJSON(t, rec)
	itemID := int64(createResp["id"].(float64))

	// 确认能下载
	rec = doRequest(handler, "GET", fmt.Sprintf("/api/clip/%d/content", itemID), nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("删除前应能下载, 状态码 = %d", rec.Code)
	}

	// 删除
	rec = doRequest(handler, "DELETE", fmt.Sprintf("/api/clip/%d", itemID), nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("删除失败: %d", rec.Code)
	}

	// 等待异步 blob 清理
	time.Sleep(100 * time.Millisecond)

	// 删除后下载应 404
	rec = doRequest(handler, "GET", fmt.Sprintf("/api/clip/%d/content", itemID), nil, token)
	if rec.Code != http.StatusNotFound {
		t.Errorf("删除后下载应 404, 实际 %d", rec.Code)
	}
}

// --- 鉴权测试 ---

func TestClip_Unauthorized(t *testing.T) {
	handler, _ := apiTestSetup(t, "open")

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/clip"},
		{http.MethodGet, "/api/clip"},
		{http.MethodGet, "/api/clip/latest"},
		{http.MethodGet, "/api/clip/1"},
		{http.MethodDelete, "/api/clip/1"},
	}

	for _, tt := range tests {
		rec := doRequest(handler, tt.method, tt.path, nil, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: 状态码 = %d, 期望 401", tt.method, tt.path, rec.Code)
		}
	}
}

// --- 配额测试 ---

func TestClip_QuotaExceeded(t *testing.T) {
	handler, st := apiTestSetup(t, "open")
	ctx := context.Background()
	hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params())
	hash, _ := hasher.Hash("password123")
	// 极小配额 100 字节
	u, _ := st.CreateUser(ctx, &store.User{Username: "quotauser", PasswordHash: hash, QuotaBytes: 100})

	jwtMgr := auth.NewJWTManager("test-secret-at-least-32-bytes-long!!!", 15*time.Minute)
	token, _ := jwtMgr.GenerateAccessToken(u.ID, u.Username, u.Role)

	// 先上传 80 字节
	text80 := strings.Repeat("x", 80)
	rec := doRequest(handler, "POST", "/api/clip", createTextRequest{Type: "text", Text: text80}, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("第一次上传应成功: %d, %s", rec.Code, rec.Body.String())
	}

	// 再上传 50 字节，总计 130 > 100
	text50 := strings.Repeat("y", 50)
	rec = doRequest(handler, "POST", "/api/clip", createTextRequest{Type: "text", Text: text50}, token)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("配额超限应 403, 状态码 = %d, body: %s", rec.Code, rec.Body.String())
	}
}

// 确保引用被使用
var _ = io.EOF
var _ = json.Marshal
