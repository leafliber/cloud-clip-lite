package blob

import (
	"bytes"
	"io"
	"net/http"
	"strings"
)

// 常见文件类型的 magic bytes
// 注意：RIFF 容器（WEBP/WAVE/AVI）不在此表，由 DetectMIME 单独按格式标签区分
var magicSignatures = []struct {
	mimeType string
	signature []byte
	offset   int
}{
	{"image/png", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, 0},
	{"image/jpeg", []byte{0xFF, 0xD8, 0xFF}, 0},
	{"image/gif", []byte{0x47, 0x49, 0x46, 0x38}, 0},
	{"image/bmp", []byte{0x42, 0x4D}, 0},
	{"image/svg+xml", []byte("<svg"), 0},
	{"application/pdf", []byte{0x25, 0x50, 0x44, 0x46}, 0}, // %PDF
	{"application/zip", []byte{0x50, 0x4B, 0x03, 0x04}, 0}, // PK..
	{"application/x-gzip", []byte{0x1F, 0x8B}, 0},
	{"application/x-tar", []byte{0x75, 0x73, 0x74, 0x61, 0x72}, 257},
	{"audio/mpeg", []byte{0x49, 0x44, 0x33}, 0}, // ID3
	{"video/mp4", []byte{0x66, 0x74, 0x79, 0x70}, 4}, // ftyp at offset 4
}

// allowedMIMETypes 允许的 MIME 类型集合
var allowedMIMETypes = map[string]bool{
	"text/plain":              true,
	"image/png":               true,
	"image/jpeg":              true,
	"image/gif":               true,
	"image/webp":              true,
	"image/bmp":               true,
	"image/svg+xml":           true,
	"application/pdf":         true,
	"application/zip":         true,
	"application/x-gzip":      true,
	"application/x-tar":       true,
	"application/octet-stream": true, // 未知类型的兜底
	"audio/mpeg":              true,
	"audio/wav":               true,
	"video/mp4":               true,
}

// DetectMIME 从文件内容头部检测真实 MIME 类型
func DetectMIME(header []byte) string {
	// RIFF 容器需看 offset 8-12 的格式标签区分 WEBP/WAVE 等
	if len(header) >= 12 && bytes.Equal(header[:4], []byte("RIFF")) {
		switch string(header[8:12]) {
		case "WEBP":
			return "image/webp"
		case "WAVE":
			return "audio/wav"
		}
		return "application/octet-stream"
	}

	for _, sig := range magicSignatures {
		if len(header) >= sig.offset+len(sig.signature) {
			if bytes.Equal(header[sig.offset:sig.offset+len(sig.signature)], sig.signature) {
				return sig.mimeType
			}
		}
	}
	return "application/octet-stream"
}

// IsMIMEAllowed 检查 MIME 类型是否在允许列表中
func IsMIMEAllowed(mimeType string) bool {
	return allowedMIMETypes[mimeType]
}

// ValidateMIME 校验声明的 MIME 类型与文件内容是否匹配
// 返回检测到的真实 MIME 类型，以及是否匹配
func ValidateMIME(declaredMIME string, header []byte) (detectedMIME string, ok bool) {
	detected := DetectMIME(header)

	// SVG 和纯文本难以通过 magic bytes 区分，需结合二次嗅探判断
	if detected == "application/octet-stream" {
		// 无魔数内容用 net/http 二次嗅探，拦截伪装成合法类型的 HTML 等危险内容
		sniffed := http.DetectContentType(header)
		if i := strings.IndexByte(sniffed, ';'); i >= 0 {
			sniffed = strings.TrimSpace(sniffed[:i])
		}
		if sniffed == "text/html" {
			return sniffed, false
		}
		// 纯文本/未知二进制：信任声明
		return declaredMIME, true
	}

	return detected, detected == declaredMIME
}

// ReadHeader 读取文件头部（最多 512 字节）用于 MIME 检测
func ReadHeader(r io.Reader) ([]byte, error) {
	header := make([]byte, 512)
	n, err := io.ReadFull(r, header)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, err
	}
	return header[:n], nil
}

// MIMEByExtension 根据文件扩展名推断 MIME 类型
func MIMEByExtension(filename string) string {
	ext := ""
	for i := len(filename) - 1; i >= 0; i-- {
		if filename[i] == '.' {
			ext = filename[i:]
			break
		}
	}
	ext = strings.ToLower(ext)
	switch ext {
	case ".txt":
		return "text/plain"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".svg":
		return "image/svg+xml"
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	case ".gz":
		return "application/x-gzip"
	case ".tar":
		return "application/x-tar"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".mp4":
		return "video/mp4"
	default:
		return "application/octet-stream"
	}
}

// IsImageType 判断 MIME 类型是否为图片
func IsImageType(mimeType string) bool {
	return len(mimeType) > 6 && mimeType[:6] == "image/"
}

// SniffContentType 使用 http.DetectContentType 检测 MIME
func SniffContentType(header []byte) string {
	return http.DetectContentType(header)
}
