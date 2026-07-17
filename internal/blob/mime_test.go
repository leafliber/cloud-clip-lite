package blob

import (
	"bytes"
	"testing"
)

func TestDetectMIME_PNG(t *testing.T) {
	header := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	mime := DetectMIME(header)
	if mime != "image/png" {
		t.Errorf("DetectMIME = %s, 期望 image/png", mime)
	}
}

func TestDetectMIME_JPEG(t *testing.T) {
	header := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	mime := DetectMIME(header)
	if mime != "image/jpeg" {
		t.Errorf("DetectMIME = %s, 期望 image/jpeg", mime)
	}
}

func TestDetectMIME_PDF(t *testing.T) {
	header := []byte{0x25, 0x50, 0x44, 0x46, 0x2D, 0x31, 0x2E}
	mime := DetectMIME(header)
	if mime != "application/pdf" {
		t.Errorf("DetectMIME = %s, 期望 application/pdf", mime)
	}
}

func TestDetectMIME_Unknown(t *testing.T) {
	header := []byte{0x00, 0x01, 0x02, 0x03}
	mime := DetectMIME(header)
	if mime != "application/octet-stream" {
		t.Errorf("DetectMIME = %s, 期望 application/octet-stream", mime)
	}
}

func TestDetectMIME_ShortHeader(t *testing.T) {
	header := []byte{0x89}
	mime := DetectMIME(header)
	if mime != "application/octet-stream" {
		t.Errorf("DetectMIME = %s, 期望 application/octet-stream", mime)
	}
}

func TestValidateMIME_Match(t *testing.T) {
	header := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	detected, ok := ValidateMIME("image/png", header)
	if !ok {
		t.Error("应匹配")
	}
	if detected != "image/png" {
		t.Errorf("detected = %s, 期望 image/png", detected)
	}
}

func TestValidateMIME_Mismatch(t *testing.T) {
	header := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	// 声明为 JPEG 但实际是 PNG
	detected, ok := ValidateMIME("image/jpeg", header)
	if ok {
		t.Error("不应匹配")
	}
	if detected != "image/png" {
		t.Errorf("detected = %s, 期望 image/png", detected)
	}
}

func TestValidateMIME_TextFallback(t *testing.T) {
	// 文本文件没有 magic bytes，应信任声明
	header := []byte("hello world")
	detected, ok := ValidateMIME("text/plain", header)
	if !ok {
		t.Error("文本应信任声明")
	}
	if detected != "text/plain" {
		t.Errorf("detected = %s, 期望 text/plain", detected)
	}
}

func TestIsMIMEAllowed(t *testing.T) {
	tests := []struct {
		mime    string
		allowed bool
	}{
		{"text/plain", true},
		{"image/png", true},
		{"image/jpeg", true},
		{"application/pdf", true},
		{"application/zip", true},
		{"application/octet-stream", true},
		{"text/html", false},
		{"application/javascript", false},
		{"application/x-executable", false},
	}
	for _, tt := range tests {
		if got := IsMIMEAllowed(tt.mime); got != tt.allowed {
			t.Errorf("IsMIMEAllowed(%s) = %v, 期望 %v", tt.mime, got, tt.allowed)
		}
	}
}

func TestIsImageType(t *testing.T) {
	if !IsImageType("image/png") {
		t.Error("image/png 应为图片")
	}
	if !IsImageType("image/jpeg") {
		t.Error("image/jpeg 应为图片")
	}
	if IsImageType("text/plain") {
		t.Error("text/plain 不应为图片")
	}
	if IsImageType("application/pdf") {
		t.Error("application/pdf 不应为图片")
	}
}

func TestMIMEByExtension(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"photo.png", "image/png"},
		{"photo.jpg", "image/jpeg"},
		{"photo.jpeg", "image/jpeg"},
		{"doc.pdf", "application/pdf"},
		{"archive.zip", "application/zip"},
		{"unknown.xyz", "application/octet-stream"},
		{"noext", "application/octet-stream"},
	}
	for _, tt := range tests {
		if got := MIMEByExtension(tt.filename); got != tt.expected {
			t.Errorf("MIMEByExtension(%s) = %s, 期望 %s", tt.filename, got, tt.expected)
		}
	}
}

func TestReadHeader(t *testing.T) {
	// 正常读取
	data := bytes.Repeat([]byte("A"), 1024)
	header, err := ReadHeader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ReadHeader 失败: %v", err)
	}
	if len(header) != 512 {
		t.Errorf("header 长度 = %d, 期望 512", len(header))
	}

	// 短数据
	shortData := []byte("hi")
	header, err = ReadHeader(bytes.NewReader(shortData))
	if err != nil {
		t.Fatalf("ReadHeader 失败: %v", err)
	}
	if len(header) != 2 {
		t.Errorf("header 长度 = %d, 期望 2", len(header))
	}
}

func TestSniffContentType(t *testing.T) {
	// PNG
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	mime := SniffContentType(pngHeader)
	if mime != "image/png" {
		t.Errorf("SniffContentType = %s, 期望 image/png", mime)
	}

	// 纯文本
	textHeader := []byte("hello world")
	mime = SniffContentType(textHeader)
	if mime != "text/plain; charset=utf-8" {
		t.Errorf("SniffContentType = %s, 期望 text/plain; charset=utf-8", mime)
	}
}

func TestDetectMIME_RIFF(t *testing.T) {
	// RIFF 容器按 offset 8-12 的格式标签区分
	webp := []byte("RIFF\x24\x00\x00\x00WEBPVP8 ")
	if mime := DetectMIME(webp); mime != "image/webp" {
		t.Errorf("DetectMIME(WEBP) = %s, 期望 image/webp", mime)
	}

	wav := []byte("RIFF\x24\x00\x00\x00WAVEfmt ")
	if mime := DetectMIME(wav); mime != "audio/wav" {
		t.Errorf("DetectMIME(WAVE) = %s, 期望 audio/wav", mime)
	}

	// 未知 RIFF 子格式
	avi := []byte("RIFF\x24\x00\x00\x00AVI LIST")
	if mime := DetectMIME(avi); mime != "application/octet-stream" {
		t.Errorf("DetectMIME(AVI) = %s, 期望 application/octet-stream", mime)
	}
}

func TestValidateMIME_WavDeclared(t *testing.T) {
	// WAV 内容声明 audio/wav 应通过（此前 wav 魔数是死代码）
	wav := []byte("RIFF\x24\x00\x00\x00WAVEfmt ")
	detected, ok := ValidateMIME("audio/wav", wav)
	if !ok {
		t.Error("WAV 内容声明 audio/wav 应匹配")
	}
	if detected != "audio/wav" {
		t.Errorf("detected = %s, 期望 audio/wav", detected)
	}

	// WebP 内容声明 audio/wav 应拒绝
	webp := []byte("RIFF\x24\x00\x00\x00WEBPVP8 ")
	if _, ok := ValidateMIME("audio/wav", webp); ok {
		t.Error("WebP 内容声明 audio/wav 不应匹配")
	}
}

func TestValidateMIME_HTMLDisguised(t *testing.T) {
	html := []byte("<html><head><title>x</title></head><body>evil</body></html>")

	// HTML 无魔数，声明成白名单类型（图片/纯文本）应被二次嗅探拦截
	for _, declared := range []string{"image/png", "text/plain", "application/octet-stream"} {
		detected, ok := ValidateMIME(declared, html)
		if ok {
			t.Errorf("HTML 内容声明 %s 应被拒绝", declared)
		}
		if detected != "text/html" {
			t.Errorf("detected = %s, 期望 text/html", detected)
		}
	}
}

func TestValidateMIME_BinaryFallback(t *testing.T) {
	// 未知二进制内容仍信任声明
	binary := []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE}
	if _, ok := ValidateMIME("application/octet-stream", binary); !ok {
		t.Error("未知二进制应信任声明")
	}
}

func TestMIMEByExtension_Uppercase(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"PHOTO.PNG", "image/png"},
		{"Doc.PDF", "application/pdf"},
		{"ARCHIVE.TAR.GZ", "application/x-gzip"},
	}
	for _, tt := range tests {
		if got := MIMEByExtension(tt.filename); got != tt.expected {
			t.Errorf("MIMEByExtension(%s) = %s, 期望 %s", tt.filename, got, tt.expected)
		}
	}
}
