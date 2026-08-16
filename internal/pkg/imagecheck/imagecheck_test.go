package imagecheck

import "testing"

func TestExtFromMime(t *testing.T) {
	cases := []struct {
		mime string
		ext  string
		ok   bool
	}{
		{MIMEJPEG, ".jpg", true},
		{MIMEPNG, ".png", true},
		{MIMEWebP, ".webp", true},
		{"image/gif", "", false},
		{"image/bmp", "", false},
		{"application/octet-stream", "", false},
		{"IMAGE/JPEG", "", false}, // 白名单大小写敏感
		{"", "", false},
	}
	for _, c := range cases {
		ext, ok := ExtFromMime(c.mime)
		if ext != c.ext || ok != c.ok {
			t.Errorf("ExtFromMime(%q) = (%q, %v), want (%q, %v)", c.mime, ext, ok, c.ext, c.ok)
		}
	}
}

func TestAllowedMimeType(t *testing.T) {
	for _, m := range []string{MIMEJPEG, MIMEPNG, MIMEWebP} {
		if !AllowedMimeType(m) {
			t.Errorf("AllowedMimeType(%q) = false, want true", m)
		}
	}
	for _, m := range []string{"image/gif", "image/bmp", "", "image/png;charset=utf-8", "image/webp2"} {
		if AllowedMimeType(m) {
			t.Errorf("AllowedMimeType(%q) = true, want false", m)
		}
	}
}

func TestCheckJPEG(t *testing.T) {
	if !CheckJPEG([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}) {
		t.Fatal("valid JPEG header rejected")
	}
	bad := [][]byte{
		{},                       // 空
		{0xFF, 0xD8},             // 截断
		{0xFF, 0xD9, 0xFF},       // 结尾标记非开头
		{0xFF, 0xD8, 0x00},       // 第三字节错误
		{0x89, 0x50, 0x4E, 0x47}, // PNG 头冒充
	}
	for i, b := range bad {
		if CheckJPEG(b) {
			t.Errorf("case %d: unexpected JPEG accept: %v", i, b)
		}
	}
}

func TestCheckPNG(t *testing.T) {
	valid := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00}
	if !CheckPNG(valid) {
		t.Fatal("valid PNG signature rejected")
	}
	bad := [][]byte{
		{},
		{0x89, 0x50, 0x4E}, // 截断
		{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0B}, // 末字节错误
		{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46},
	}
	for i, b := range bad {
		if CheckPNG(b) {
			t.Errorf("case %d: unexpected PNG accept: %v", i, b)
		}
	}
}

func TestCheckWebP(t *testing.T) {
	// RIFF + 4 字节大小 + WEBP
	valid := []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P', 0x56, 0x50, 0x38}
	if !CheckWebP(valid) {
		t.Fatal("valid WebP header rejected")
	}
	bad := [][]byte{
		{},
		{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'W', 'E'},           // 截断 <12
		{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'W', 'X', 'B', 'P'}, // 标记错误
		{'R', 'I', 'F', 'X', 0x00, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P'}, // 容器类型错误
		{'r', 'i', 'f', 'f', 0x00, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P'}, // 大小写敏感
		{'A', 'A', 'A', 'A', 0x00, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P'},
	}
	for i, b := range bad {
		if CheckWebP(b) {
			t.Errorf("case %d: unexpected WebP accept: %v", i, b)
		}
	}
}

func TestCheckDispatch(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	webp := []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P'}

	// 声明与内容一致 → 通过
	for _, c := range []struct {
		mime string
		hdr  []byte
	}{{MIMEPNG, png}, {MIMEJPEG, jpeg}, {MIMEWebP, webp}} {
		if !Check(c.mime, c.hdr) {
			t.Errorf("Check(%s) = false, want true", c.mime)
		}
	}
	// 声明与内容不一致 → 拒绝
	if Check(MIMEJPEG, png) {
		t.Error("PNG content declared as JPEG should be rejected")
	}
	if Check(MIMEPNG, jpeg) {
		t.Error("JPEG content declared as PNG should be rejected")
	}
	// 未知 mime → 拒绝
	if Check("image/gif", webp) {
		t.Error("unknown mime should be rejected")
	}
}
