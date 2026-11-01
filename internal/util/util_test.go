package util

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStripExtension 验证只去掉最后一个扩展名。
func TestStripExtension(t *testing.T) {
	cases := map[string]string{
		"a.zip":     "a",
		"a.tar.gz":  "a.tar",
		"data":      "data",
		"a.b.c.tar": "a.b.c",
	}
	for in, want := range cases {
		if got := StripExtension(in); got != want {
			t.Errorf("StripExtension(%q) = %q，期望 %q", in, got, want)
		}
	}

	// 纯后缀文件名（如 ".gz"）的结果是空串：filepath.Ext 把整个名字
	// 视作扩展名。调用方（archiveDirName）已有空值回退到原名的处理。
	if got := StripExtension(".gz"); got != "" {
		t.Errorf("StripExtension(%q) 应为空串，实际 %q", ".gz", got)
	}
}

// TestSamePath 验证路径同一性判断。
func TestSamePath(t *testing.T) {
	dir := t.TempDir()
	if !SamePath(dir, dir) {
		t.Error("相同路径应判定为同一位置")
	}
	// 带尾部分隔符的等价路径。
	if !SamePath(dir+"/", dir) {
		t.Error("尾部分隔符不应影响判断")
	}
	if SamePath(dir, filepath.Join(dir, "sub")) {
		t.Error("父目录与子路径不应判定为同一位置")
	}
}

// TestDirPerm 验证目录权限推导。
func TestDirPerm(t *testing.T) {
	if got := DirPerm(0); got != 0o755 {
		t.Errorf("零值应给出默认权限，实际 %o", got)
	}
	if got := DirPerm(0o644); got&0o700 != 0o700 {
		t.Errorf("目录必须补上可执行位，实际 %o", got)
	}
}

// TestEnsureDir 验证目录创建（含多级）。
func TestEnsureDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b", "c")
	if err := EnsureDir(dir); err != nil {
		t.Fatalf("创建多级目录失败：%v", err)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("目录未创建：%v", err)
	}
	// 已存在时不应报错。
	if err := EnsureDir(dir); err != nil {
		t.Errorf("已存在的目录不应报错：%v", err)
	}
}

// TestCreateOutputOverwritePolicy 验证覆盖策略。
func TestCreateOutputOverwritePolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	// 目标不存在：直接创建。
	f, err := CreateOutput(path, false)
	if err != nil {
		t.Fatalf("首次创建失败：%v", err)
	}
	f.Close()

	// 不允许覆盖：应报 os.ErrExist。
	_, err = CreateOutput(path, false)
	if !errors.Is(err, os.ErrExist) {
		t.Errorf("不允许覆盖时应返回 ErrExist，实际 %v", err)
	}

	// 允许覆盖：成功。
	f, err = CreateOutput(path, true)
	if err != nil {
		t.Fatalf("允许覆盖时不应报错：%v", err)
	}
	f.Close()
}

// TestUniqueOutputPath 验证冲突路径的序号递增。
func TestUniqueOutputPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 允许覆盖时保持原路径。
	if got := UniqueOutputPath(path, true); got != path {
		t.Errorf("允许覆盖时不应改名，实际 %q", got)
	}

	// 不允许覆盖：追加 .1。
	got := UniqueOutputPath(path, false)
	if got != filepath.Join(dir, "out.1.txt") {
		t.Errorf("期望 out.1.txt，实际 %q", got)
	}

	// .1 也存在时继续递增到 .2。
	if err := os.WriteFile(filepath.Join(dir, "out.1.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := UniqueOutputPath(path, false); got != filepath.Join(dir, "out.2.txt") {
		t.Errorf("期望 out.2.txt，实际 %q", got)
	}

	// 无扩展名的文件也要能正确追加序号。
	plain := filepath.Join(dir, "data")
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := UniqueOutputPath(plain, false); got != filepath.Join(dir, "data.1") {
		t.Errorf("期望 data.1，实际 %q", got)
	}
}

// TestCopyWithContext 验证流式复制的字节数与内容。
func TestCopyWithContext(t *testing.T) {
	data := bytes.Repeat([]byte("abcdefgh"), 4096)
	var dst bytes.Buffer

	n, err := CopyWithContext(context.Background(), &dst, bytes.NewReader(data), 0)
	if err != nil {
		t.Fatalf("复制失败：%v", err)
	}
	if n != int64(len(data)) {
		t.Errorf("期望复制 %d 字节，实际 %d", len(data), n)
	}
	if !bytes.Equal(dst.Bytes(), data) {
		t.Error("复制内容不一致")
	}
}

// TestCopyWithContextSizeLimit 验证大小上限生效。
func TestCopyWithContextSizeLimit(t *testing.T) {
	data := bytes.Repeat([]byte("a"), 1024)
	var dst bytes.Buffer

	n, err := CopyWithContext(context.Background(), &dst, bytes.NewReader(data), 100)
	if !errors.Is(err, ErrSizeLimit) {
		t.Errorf("应返回 ErrSizeLimit，实际 %v", err)
	}
	if n != 100 {
		t.Errorf("超限时应写出 100 字节，实际 %d", n)
	}
	if dst.Len() != 100 {
		t.Errorf("目标应只收到 100 字节，实际 %d", dst.Len())
	}
}

// TestCopyWithContextCancel 验证上下文取消生效。
func TestCopyWithContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var dst bytes.Buffer
	_, err := CopyWithContext(ctx, &dst, bytes.NewReader([]byte("data")), 0)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("已取消的上下文应返回 Canceled，实际 %v", err)
	}
}

// TestDecodeGBK 验证编码回退行为。
func TestDecodeGBK(t *testing.T) {
	// 空串直接返回 false。
	if _, ok := DecodeGBK(""); ok {
		t.Error("空串不应转换")
	}

	// 合法 UTF-8 不应被转换。
	if _, ok := DecodeGBK("hello"); ok {
		t.Error("合法 UTF-8 不应转换")
	}
	if _, ok := DecodeGBK("中文"); ok {
		t.Error("合法 UTF-8 中文不应转换")
	}

	// GBK 编码的 "中文"（0xD6D0 0xCEC4）应被正确解码。
	gbk := string([]byte{0xD6, 0xD0, 0xCE, 0xC4})
	got, ok := DecodeGBK(gbk)
	if !ok {
		t.Fatal("GBK 字节应被成功解码")
	}
	if got != "中文" {
		t.Errorf("期望 中文，实际 %q", got)
	}

	// 非法字节序列应返回原始值且不报成功。
	junk := string([]byte{0xFF, 0xFE, 0xFD})
	if got, ok := DecodeGBK(junk); ok {
		t.Errorf("非法序列不应报告成功，实际 %q", got)
	} else if !strings.Contains(got, "\xff") {
		t.Errorf("解码失败时应返回原始值，实际 %q", got)
	}
}
