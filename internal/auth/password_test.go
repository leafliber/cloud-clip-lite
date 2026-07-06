package auth

import (
	"strings"
	"testing"
)

func TestPasswordHasher_HashAndVerify(t *testing.T) {
	h := NewPasswordHasher(DefaultArgon2Params())

	tests := []struct {
		name     string
		password string
	}{
		{"简单密码", "123456"},
		{"复杂密码", "MyStr0ngP@ssw0rd!2026"},
		{"中文密码", "你好世界123"},
		{"长密码", strings.Repeat("a", 256)},
		{"空密码", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hashed, err := h.Hash(tt.password)
			if err != nil {
				t.Fatalf("Hash 失败: %v", err)
			}

			// 哈希不应等于明文
			if hashed == tt.password {
				t.Fatal("哈希值不应等于明文密码")
			}

			// 哈希应包含 argon2id 前缀
			if !strings.HasPrefix(hashed, "$argon2id$") {
				t.Fatalf("哈希格式错误: %s", hashed)
			}

			// 正确密码应验证通过
			ok, err := h.Verify(tt.password, hashed)
			if err != nil {
				t.Fatalf("Verify 失败: %v", err)
			}
			if !ok {
				t.Fatal("正确密码验证失败")
			}
		})
	}
}

func TestPasswordHasher_WrongPassword(t *testing.T) {
	h := NewPasswordHasher(DefaultArgon2Params())

	hashed, err := h.Hash("correct-password")
	if err != nil {
		t.Fatalf("Hash 失败: %v", err)
	}

	// 错误密码应验证失败
	ok, err := h.Verify("wrong-password", hashed)
	if err != nil {
		t.Fatalf("Verify 不应返回错误: %v", err)
	}
	if ok {
		t.Fatal("错误密码不应验证通过")
	}
}

func TestPasswordHasher_DifferentSalts(t *testing.T) {
	h := NewPasswordHasher(DefaultArgon2Params())

	password := "same-password"
	hash1, _ := h.Hash(password)
	hash2, _ := h.Hash(password)

	// 同一密码两次哈希应不同（不同盐）
	if hash1 == hash2 {
		t.Fatal("同一密码两次哈希应产生不同结果（不同盐）")
	}

	// 两个哈希都应验证通过
	ok1, _ := h.Verify(password, hash1)
	ok2, _ := h.Verify(password, hash2)
	if !ok1 || !ok2 {
		t.Fatal("两个哈希都应验证通过")
	}
}

func TestPasswordHasher_InvalidHashFormat(t *testing.T) {
	h := NewPasswordHasher(DefaultArgon2Params())

	tests := []struct {
		name string
		hash string
	}{
		{"空字符串", ""},
		{"格式错误", "not-a-hash"},
		{"缺少部分", "$argon2id$v=19"},
		{"错误算法", "$argon2d$v=19$m=65536,t=3,p=2$abc$def"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.Verify("password", tt.hash)
			if err == nil {
				t.Fatal("格式非法时应返回错误")
			}
		})
	}
}
