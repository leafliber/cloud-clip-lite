package auth

import (
	"strings"
	"testing"
	"time"
)

func TestJWTManager_GenerateAndParse(t *testing.T) {
	secret := "test-secret-key-at-least-32-bytes-long!!"
	mgr := NewJWTManager(secret, 15*time.Minute)

	userID := int64(42)
	username := "testuser"
	role := "user"

	token, err := mgr.GenerateAccessToken(userID, username, role)
	if err != nil {
		t.Fatalf("GenerateAccessToken 失败: %v", err)
	}

	// token 应有三段（header.payload.signature）
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT 应有 3 段，实际 %d", len(parts))
	}

	claims, err := mgr.ParseAccessToken(token)
	if err != nil {
		t.Fatalf("ParseAccessToken 失败: %v", err)
	}

	if claims.UserID != userID {
		t.Errorf("UserID = %d, 期望 %d", claims.UserID, userID)
	}
	if claims.Username != username {
		t.Errorf("Username = %s, 期望 %s", claims.Username, username)
	}
	if claims.Role != role {
		t.Errorf("Role = %s, 期望 %s", claims.Role, role)
	}
}

func TestJWTManager_ExpiredToken(t *testing.T) {
	secret := "test-secret-key-at-least-32-bytes-long!!"
	// 极短 TTL，立即过期
	mgr := NewJWTManager(secret, -1*time.Second)

	token, err := mgr.GenerateAccessToken(1, "user", "user")
	if err != nil {
		t.Fatalf("GenerateAccessToken 失败: %v", err)
	}

	_, err = mgr.ParseAccessToken(token)
	if err == nil {
		t.Fatal("过期 token 应解析失败")
	}
}

func TestJWTManager_InvalidSecret(t *testing.T) {
	mgr1 := NewJWTManager("secret-one-at-least-32-bytes-long!!!", 15*time.Minute)
	mgr2 := NewJWTManager("secret-two-at-least-32-bytes-long!!!", 15*time.Minute)

	token, err := mgr1.GenerateAccessToken(1, "user", "user")
	if err != nil {
		t.Fatalf("GenerateAccessToken 失败: %v", err)
	}

	// 用不同密钥解析应失败
	_, err = mgr2.ParseAccessToken(token)
	if err == nil {
		t.Fatal("不同密钥应解析失败")
	}
}

func TestJWTManager_MalformedToken(t *testing.T) {
	mgr := NewJWTManager("test-secret-key-at-least-32-bytes-long!!", 15*time.Minute)

	tests := []string{
		"",
		"not-a-token",
		"a.b.c",
		"eyJhbGciOiJIUzI1NiJ9.invalid.signature",
	}

	for _, token := range tests {
		_, err := mgr.ParseAccessToken(token)
		if err == nil {
			t.Fatalf("格式非法的 token 应解析失败: %s", token)
		}
	}
}

func TestJWTManager_AccessTTL(t *testing.T) {
	ttl := 30 * time.Minute
	mgr := NewJWTManager("test-secret-key-at-least-32-bytes-long!!", ttl)
	if mgr.AccessTTL() != ttl {
		t.Errorf("AccessTTL = %v, 期望 %v", mgr.AccessTTL(), ttl)
	}
}
