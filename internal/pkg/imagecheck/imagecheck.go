// Package imagecheck 对图片文件头做 Magic Number 校验。
// 应用层（Flutter）负责压缩/格式转换，COS SDK 仅做人参校验，
// 后端在此做二次校验，防止伪造 Content-Type 上传任意文件。
package imagecheck

import "bytes"

// MIME 类型白名单（与 PRD 保持一致：仅 jpeg/png/webp）。
const (
	MIMEJPEG = "image/jpeg"
	MIMEPNG  = "image/png"
	MIMEWebP = "image/webp"
)

// AllowedMimeType 判断 mime_type 是否为白名单内类型。
func AllowedMimeType(mime string) bool {
	_, ok := ExtFromMime(mime)
	return ok
}

// ExtFromMime 返回 mime_type 对应的强制扩展名（带点），非白名单返回 false。
// 该映射在 presign 阶段强制决定 file_key 后缀，客户端文件名不可干预。
func ExtFromMime(mime string) (string, bool) {
	switch mime {
	case MIMEJPEG:
		return ".jpg", true
	case MIMEPNG:
		return ".png", true
	case MIMEWebP:
		return ".webp", true
	default:
		return "", false
	}
}

// Check 根据声明的 mime_type 校验文件头（Magic Number）。
func Check(mime string, header []byte) bool {
	switch mime {
	case MIMEJPEG:
		return CheckJPEG(header)
	case MIMEPNG:
		return CheckPNG(header)
	case MIMEWebP:
		return CheckWebP(header)
	default:
		return false
	}
}

// CheckJPEG 校验 JPEG 前 3 字节为 FF D8 FF。
func CheckJPEG(b []byte) bool {
	return len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF
}

// CheckPNG 校验 PNG 前 8 字节为 89 50 4E 47 0D 0A 1A 0A。
func CheckPNG(b []byte) bool {
	sig := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	return len(b) >= len(sig) && bytes.Equal(b[:len(sig)], sig)
}

// CheckWebP 校验 WebP：前 4 字节为 "RIFF"，第 8-11 字节为 "WEBP"。
func CheckWebP(b []byte) bool {
	if len(b) < 12 {
		return false
	}
	return b[0] == 'R' && b[1] == 'I' && b[2] == 'F' && b[3] == 'F' &&
		b[8] == 'W' && b[9] == 'E' && b[10] == 'B' && b[11] == 'P'
}
