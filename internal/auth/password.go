package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2Params Argon2id 哈希参数
type Argon2Params struct {
	Memory      uint32 // KiB
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultArgon2Params 默认参数（对应 OWASP 推荐基线）
func DefaultArgon2Params() Argon2Params {
	return Argon2Params{
		Memory:      65536, // 64 MB
		Iterations:  3,
		Parallelism: 2,
		SaltLength:  16,
		KeyLength:   32,
	}
}

// PasswordHasher 密码哈希器
type PasswordHasher struct {
	params Argon2Params
}

// NewPasswordHasher 创建密码哈希器
func NewPasswordHasher(p Argon2Params) *PasswordHasher {
	return &PasswordHasher{params: p}
}

// Hash 使用 Argon2id 哈希密码，返回编码字符串
// 格式：$argon2id$v=19$m=65536,t=3,p=2$<base64-salt>$<base64-hash>
func (h *PasswordHasher) Hash(password string) (string, error) {
	salt := make([]byte, h.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成盐失败: %w", err)
	}

	hash := argon2.IDKey(
		[]byte(password), salt,
		h.params.Iterations, h.params.Memory, h.params.Parallelism, h.params.KeyLength,
	)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		h.params.Memory, h.params.Iterations, h.params.Parallelism,
		b64Salt, b64Hash,
	), nil
}

// Verify 校验密码是否匹配哈希
func (h *PasswordHasher) Verify(password, encodedHash string) (bool, error) {
	// 兼容不同参数的哈希：从 encodedHash 中解析参数
	p, salt, hash, err := decodeArgon2Hash(encodedHash)
	if err != nil {
		return false, err
	}

	otherHash := argon2.IDKey(
		[]byte(password), salt,
		p.Iterations, p.Memory, p.Parallelism, p.KeyLength,
	)

	if subtle.ConstantTimeCompare(hash, otherHash) == 1 {
		return true, nil
	}
	return false, nil
}

// decodeArgon2Hash 从编码字符串解析参数、盐和哈希
func decodeArgon2Hash(encodedHash string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return Argon2Params{}, nil, nil, errors.New("哈希格式非法")
	}

	var algorithm string
	_, err := fmt.Sscanf(parts[1], "%s", &algorithm)
	if err != nil {
		return Argon2Params{}, nil, nil, err
	}
	if algorithm != "argon2id" {
		return Argon2Params{}, nil, nil, errors.New("不支持的哈希算法: " + algorithm)
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return Argon2Params{}, nil, nil, err
	}
	if version != argon2.Version {
		return Argon2Params{}, nil, nil, fmt.Errorf("不支持的 argon2 版本: %d", version)
	}

	p := Argon2Params{}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Iterations, &p.Parallelism); err != nil {
		return Argon2Params{}, nil, nil, err
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Argon2Params{}, nil, nil, err
	}
	p.SaltLength = uint32(len(salt))

	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return Argon2Params{}, nil, nil, err
	}
	p.KeyLength = uint32(len(hash))

	return p, salt, hash, nil
}
