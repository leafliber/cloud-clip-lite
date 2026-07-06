package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// GenerateAPIToken 生成随机 API Token（明文，仅返回一次）
// 格式：cb_<48字节随机十六进制>
func GenerateAPIToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成 API Token 失败: %w", err)
	}
	return "cb_" + hex.EncodeToString(b), nil
}

// HashToken 对 Token 做 SHA-256 哈希（存储哈希值，不存明文）
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// VerifyToken 校验明文 Token 是否匹配哈希
// 使用恒定时间比较防止时序攻击
func VerifyToken(token, hashed string) bool {
	computed := HashToken(token)
	if len(computed) != len(hashed) {
		return false
	}
	// subtle.ConstantTimeCompare 在 crypto/subtle 中
	// 这里用简单实现避免额外 import，但逻辑等价
	result := 0
	for i := 0; i < len(computed); i++ {
		result |= int(computed[i] ^ hashed[i])
	}
	return result == 0
}

// GenerateInviteCode 生成邀请码（8 位大写字母+数字）
func GenerateInviteCode() (string, error) {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // 去除易混淆字符
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成邀请码失败: %w", err)
	}
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b), nil
}

// GenerateRefreshToken 生成 Refresh Token 明文
func GenerateRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成 Refresh Token 失败: %w", err)
	}
	return "rt_" + hex.EncodeToString(b), nil
}
