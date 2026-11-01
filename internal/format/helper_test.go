package format

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// createMaliciousZip 构造一个包含路径穿越条目的 ZIP。
//
// 条目名为 "../../escaped.txt"，解压时若不做防护就会写到目标目录之外。
func createMaliciousZip(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	entry, err := zw.Create("../../escaped.txt")
	if err != nil {
		return err
	}
	if _, err := entry.Write(bytes.Repeat([]byte("PWNED"), 100)); err != nil {
		return err
	}
	return zw.Close()
}

// collectFiles 递归收集目录下所有文件，返回相对路径到绝对路径的映射。
func collectFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	result := make(map[string]string)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result[rel] = path
		return nil
	})
	if err != nil {
		t.Fatalf("遍历目录失败：%v", err)
	}
	return result
}

// sameContent 比较两个文件的内容是否一致。
func sameContent(t *testing.T, a, b string) bool {
	t.Helper()
	dataA, err := os.ReadFile(a)
	if err != nil {
		t.Fatalf("读取失败 %s：%v", a, err)
	}
	dataB, err := os.ReadFile(b)
	if err != nil {
		t.Fatalf("读取失败 %s：%v", b, err)
	}
	return bytes.Equal(dataA, dataB)
}
