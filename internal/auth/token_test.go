package auth

import (
	"strings"
	"testing"
)

func TestGenerateAPIToken(t *testing.T) {
	token, err := GenerateAPIToken()
	if err != nil {
		t.Fatalf("GenerateAPIToken 失败: %v", err)
	}

	// 应有 cb_ 前缀
	if !strings.HasPrefix(token, "cb_") {
		t.Fatalf("API Token 应有 cb_ 前缀: %s", token)
	}

	// 明文部分应足够长（cb_ + 64 hex chars）
	if len(token) < 60 {
		t.Fatalf("API Token 长度不足: %d", len(token))
	}
}

func TestGenerateAPIToken_Uniqueness(t *testing.T) {
	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token, err := GenerateAPIToken()
		if err != nil {
			t.Fatalf("GenerateAPIToken 失败: %v", err)
		}
		if tokens[token] {
			t.Fatalf("生成了重复的 API Token: %s", token)
		}
		tokens[token] = true
	}
}

func TestHashToken(t *testing.T) {
	token := "cb_testtoken123"
	hashed := HashToken(token)

	// 哈希不应等于明文
	if hashed == token {
		t.Fatal("哈希不应等于明文")
	}

	// 相同输入应产生相同哈希
	hashed2 := HashToken(token)
	if hashed != hashed2 {
		t.Fatal("相同 token 应产生相同哈希")
	}
}

func TestVerifyToken(t *testing.T) {
	token := "cb_mysecrettoken"
	hashed := HashToken(token)

	// 正确 token 验证通过
	if !VerifyToken(token, hashed) {
		t.Fatal("正确 token 应验证通过")
	}

	// 错误 token 验证失败
	if VerifyToken("cb_wrongtoken", hashed) {
		t.Fatal("错误 token 不应验证通过")
	}

	// 空字符串
	if VerifyToken("", hashed) {
		t.Fatal("空 token 不应验证通过")
	}
}

func TestGenerateInviteCode(t *testing.T) {
	code, err := GenerateInviteCode()
	if err != nil {
		t.Fatalf("GenerateInviteCode 失败: %v", err)
	}

	// 应为 8 位
	if len(code) != 8 {
		t.Fatalf("邀请码长度应为 8，实际 %d", len(code))
	}

	// 应为大写字母和数字（无易混淆字符 I/O/0/1）
	for _, c := range code {
		if !strings.ContainsRune("ABCDEFGHJKLMNPQRSTUVWXYZ23456789", c) {
			t.Fatalf("邀请码包含非法字符: %c", c)
		}
	}
}

func TestGenerateInviteCode_Uniqueness(t *testing.T) {
	codes := make(map[string]bool)
	for i := 0; i < 100; i++ {
		code, err := GenerateInviteCode()
		if err != nil {
			t.Fatalf("GenerateInviteCode 失败: %v", err)
		}
		if codes[code] {
			t.Fatalf("生成了重复的邀请码: %s", code)
		}
		codes[code] = true
	}
}

func TestGenerateRefreshToken(t *testing.T) {
	token, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken 失败: %v", err)
	}

	// 应有 rt_ 前缀
	if !strings.HasPrefix(token, "rt_") {
		t.Fatalf("Refresh Token 应有 rt_ 前缀: %s", token)
	}

	// 应足够长
	if len(token) < 60 {
		t.Fatalf("Refresh Token 长度不足: %d", len(token))
	}

	// 唯一性
	token2, _ := GenerateRefreshToken()
	if token == token2 {
		t.Fatal("两次生成的 Refresh Token 不应相同")
	}
}
