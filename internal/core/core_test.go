package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSanitizeEntryPath 验证路径穿越防护。
func TestSanitizeEntryPath(t *testing.T) {
	dest := "/tmp/out"

	safe := []string{
		"a.txt",
		"dir/a.txt",
		"dir/sub/a.txt",
		"./a.txt",
		"dir/./a.txt",
	}
	for _, name := range safe {
		got, err := SanitizeEntryPath(dest, name)
		if err != nil {
			t.Errorf("%q 应被接受，但报错：%v", name, err)
			continue
		}
		if !IsWithinDir(dest, got) {
			t.Errorf("%q 归一化后越界：%s", name, got)
		}
	}

	unsafe := []string{
		"../evil.txt",
		"../../evil.txt",
		"dir/../../evil.txt",
		"/etc/passwd",
		"C:\\Windows\\evil.exe",
		"..\\..\\evil.txt",
	}
	for _, name := range unsafe {
		got, err := SanitizeEntryPath(dest, name)
		if err == nil {
			t.Errorf("%q 应被拒绝，但得到 %s", name, got)
		}
	}
}

// TestSanitizeEntryPathKeepsWithin 验证带 ".." 但未越界的路径被正确归一化。
func TestSanitizeEntryPathKeepsWithin(t *testing.T) {
	dest := "/tmp/out"
	got, err := SanitizeEntryPath(dest, "dir/../a.txt")
	if err != nil {
		t.Fatalf("不应报错：%v", err)
	}
	if got != filepath.Join(dest, "a.txt") {
		t.Errorf("归一化结果错误：期望 %s，实际 %s", filepath.Join(dest, "a.txt"), got)
	}
}

// TestSanitizeEntryPathRootEntry 验证指向归档根的条目名。
func TestSanitizeEntryPathRootEntry(t *testing.T) {
	dest := "/tmp/out"
	got, err := SanitizeEntryPath(dest, "./")
	if err != nil {
		t.Fatalf("归档根条目不应报错：%v", err)
	}
	if got != dest {
		t.Errorf("归档根条目应解析为目标目录本身：%s", got)
	}
}

// TestIsWithinDir 验证目录边界判断。
func TestIsWithinDir(t *testing.T) {
	dir := "/tmp/out"

	inside := []string{"/tmp/out", "/tmp/out/a.txt", "/tmp/out/sub/deep.txt"}
	for _, p := range inside {
		if !IsWithinDir(dir, p) {
			t.Errorf("%s 应在目录内", p)
		}
	}

	outside := []string{"/tmp/outside.txt", "/tmp", "/other/a.txt"}
	for _, p := range outside {
		if IsWithinDir(dir, p) {
			t.Errorf("%s 应在目录外", p)
		}
	}
}

// TestRegistryDetectByMagic 验证魔数识别由注册表汇总各格式声明的规则驱动。
func TestRegistryDetectByMagic(t *testing.T) {
	reg := NewRegistry()
	reg.MustRegister(&fakeCompression{
		name:       "zip",
		extensions: []string{".zip"},
		magic:      []Magic{{Offset: 0, Pattern: []byte("PK\x03\x04")}},
	})

	dir := t.TempDir()
	archive := filepath.Join(dir, "a.zip")
	if err := os.WriteFile(archive, []byte("PK\x03\x04rest"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := reg.Detect(archive); got != "zip" {
		t.Errorf("魔数识别失败：期望 zip，实际 %q", got)
	}
}

// TestRegistryDetectFallbackToExtension 验证魔数失败时回退扩展名。
func TestRegistryDetectFallbackToExtension(t *testing.T) {
	reg := NewRegistry()
	reg.MustRegister(&fakeCompression{
		name:       "zip",
		extensions: []string{".zip"},
		magic:      []Magic{{Offset: 0, Pattern: []byte("PK\x03\x04")}},
	})

	dir := t.TempDir()
	archive := filepath.Join(dir, "a.zip")
	if err := os.WriteFile(archive, []byte("not a zip at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := reg.Detect(archive); got != "zip" {
		t.Errorf("扩展名回退失败：期望 zip，实际 %q", got)
	}
}

// TestRegistryDetectUnknown 验证无法识别时返回空串。
func TestRegistryDetectUnknown(t *testing.T) {
	reg := NewRegistry()
	reg.MustRegister(&fakeCompression{
		name:       "zip",
		extensions: []string{".zip"},
		magic:      []Magic{{Offset: 0, Pattern: []byte("PK\x03\x04")}},
	})

	dir := t.TempDir()
	plain := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(plain, []byte("just text"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := reg.Detect(plain); got != FormatUnknown {
		t.Errorf("普通文件不应被识别，实际 %q", got)
	}
}

// TestRegistryPriorityOrder 验证识别优先级即注册顺序。
func TestRegistryPriorityOrder(t *testing.T) {
	reg := NewRegistry()
	// 两个格式声明相同的魔数，先注册的应优先命中。
	reg.MustRegister(&fakeCompression{
		name:  "first",
		magic: []Magic{{Offset: 0, Pattern: []byte("XX")}},
	})
	reg.MustRegister(&fakeCompression{
		name:  "second",
		magic: []Magic{{Offset: 0, Pattern: []byte("XX")}},
	})

	dir := t.TempDir()
	f := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(f, []byte("XXyy"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := reg.Detect(f); got != "first" {
		t.Errorf("识别优先级应为注册顺序：期望 first，实际 %q", got)
	}
}

// TestRegistryAllOrder 验证 All 返回注册顺序。
func TestRegistryAllOrder(t *testing.T) {
	reg := NewRegistry()
	reg.MustRegister(&fakeCompression{name: "a", magic: []Magic{{Offset: 0, Pattern: []byte("AA")}}})
	reg.MustRegister(&fakeCompression{name: "b", magic: []Magic{{Offset: 0, Pattern: []byte("BB")}}})

	all := reg.All()
	if len(all) != 2 {
		t.Fatalf("期望 2 个格式，实际 %d", len(all))
	}
	if all[0].Name() != "a" || all[1].Name() != "b" {
		t.Errorf("注册顺序被改变：%s, %s", all[0].Name(), all[1].Name())
	}
}

// TestRegistryRejectEmptyName 验证空标识会被拒绝。
func TestRegistryRejectEmptyName(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(&fakeCompression{name: "", magic: []Magic{{Offset: 0, Pattern: []byte("AA")}}}); err == nil {
		t.Error("空标识注册应报错")
	}
}

// TestRegistryExtensions 验证扩展名汇总。
func TestRegistryExtensions(t *testing.T) {
	reg := NewRegistry()
	reg.MustRegister(&fakeCompression{
		name:       "zip",
		extensions: []string{".zip"},
		magic:      []Magic{{Offset: 0, Pattern: []byte("AA")}},
	})
	reg.MustRegister(&fakeCompression{
		name:       "tar",
		extensions: []string{".tar"},
		magic:      []Magic{{Offset: 0, Pattern: []byte("BB")}},
	})

	exts := reg.Extensions()
	if exts[".zip"] != "zip" {
		t.Errorf(".zip 应映射到 zip，实际 %q", exts[".zip"])
	}
	if exts[".tar"] != "tar" {
		t.Errorf(".tar 应映射到 tar，实际 %q", exts[".tar"])
	}
}

// TestRegistryIsSupportedSuffix 验证扩展名判断。
func TestRegistryIsSupportedSuffix(t *testing.T) {
	reg := NewRegistry()
	reg.MustRegister(&fakeCompression{
		name:       "zip",
		extensions: []string{".zip"},
		magic:      []Magic{{Offset: 0, Pattern: []byte("AA")}},
	})

	if !reg.IsSupportedSuffix("a.zip") {
		t.Error("a.zip 应被支持")
	}
	if reg.IsSupportedSuffix("a.txt") {
		t.Error("a.txt 不应被支持")
	}
}

// TestDisplayNameFallback 验证展示名回退到大写标识。
func TestDisplayNameFallback(t *testing.T) {
	if got := DisplayName(FormatUnknown); got != "UNKNOWN" {
		t.Errorf("未知格式展示名应为 UNKNOWN，实际 %q", got)
	}
	if got := DisplayName("apk"); got != "APK" {
		t.Errorf("未登记格式应使用大写标识，实际 %q", got)
	}
	if got := DisplayName(FormatZip); got != "ZIP" {
		t.Errorf("内置格式应使用登记名，实际 %q", got)
	}
}

// TestDisplayNameIsEmptyString 验证格式标识是字符串类型。
func TestDisplayNameIsEmptyString(t *testing.T) {
	if FormatUnknown != "" {
		t.Errorf("FormatUnknown 应为空串")
	}
	if !strings.HasPrefix(FormatZip, "") {
		t.Error("格式标识应可作为普通字符串使用")
	}
}
