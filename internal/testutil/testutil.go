// Package testutil 提供测试专用的辅助能力。
//
// 本包仅供 _test.go 文件使用，不会被链接进任何发布产物。之所以独立成包，
// 是因为 core 与 format 两个包的测试都需要构造归档、调用外部工具、
// 检索产物目录树，重复实现会让测试代码本身成为维护负担。
//
// 注意：本包 import 了 testing，因此不可被非测试代码引用。
package testutil

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// MustRun 执行外部命令并在失败时终止测试。
func MustRun(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("执行 %s %v 失败：%v\n%s", name, args, err, out)
	}
}

// HaveTool 检查外部工具是否存在。
func HaveTool(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// BuildZip 在 path 处构造一个包含指定文件内容的 ZIP。
//
// files 的键是归档内的相对路径，值为文件内容。
// 用 Go 原生 zip 库构造，不依赖外部命令，可处理空归档与无扩展名归档。
func BuildZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for name, content := range files {
		entry, err := zw.Create(name)
		if err != nil {
			t.Fatalf("创建条目失败 %s：%v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("写入条目失败 %s：%v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 ZIP 失败：%v", err)
	}
}

// BuildZipFromFiles 把已有的文件打包成 ZIP。
//
// entries 的键是归档内的相对路径，值为源文件路径。
func BuildZipFromFiles(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	build := t.TempDir()
	for name, src := range entries {
		full := filepath.Join(build, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("读取失败 %s：%v", src, err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cmd := exec.Command("zip", "-qr", path, ".")
	cmd.Dir = build
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("构造 ZIP 失败：%v\n%s", err, out)
	}
}

// MustWrite 写入文件内容并返回路径。
func MustWrite(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// FindFile 在目录树中递归查找指定文件名的文件。
func FindFile(t *testing.T, root, name string) string {
	t.Helper()
	var found string
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.Name() == name && found == "" {
			found = path
		}
		return nil
	})
	return found
}

// ListTree 列出目录树的所有相对路径，用于失败诊断。
func ListTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel != "." {
			out = append(out, rel)
		}
		return nil
	})
	return out
}
