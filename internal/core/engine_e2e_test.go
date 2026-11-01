// 本文件是 core 的外部测试包，用真实归档验证调度引擎的端到端行为。
//
// 必须使用 package core_test（而非 package core）：这些测试需要 import
// format 来装配真实格式实现，而 format 本身 import core。若写成内部测试包
// 会形成 import cycle；外部测试包编译为独立包，core_test → format → core
// 不构成环。
package core_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/liux/decompress/internal/core"
	"github.com/liux/decompress/internal/format"
	"github.com/liux/decompress/internal/testutil"
)

// TestEndToEndAllFormats 用真实归档跑通全部格式。
func TestEndToEndAllFormats(t *testing.T) {
	if !testutil.HaveTool("tar") || !testutil.HaveTool("gzip") || !testutil.HaveTool("bzip2") ||
		!testutil.HaveTool("xz") || !testutil.HaveTool("zstd") || !testutil.HaveTool("7z") {
		t.Skip("缺少构造样本所需的外部工具，跳过")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	archives := []string{"t.tar", "t.tar.gz", "t.tar.bz2", "t.tar.xz", "t.tar.zst", "t.zip", "t.7z"}
	for _, name := range archives {
		path := filepath.Join(dir, name)
		switch {
		case name == "t.tar":
			testutil.MustRun(t, "tar", "-cf", path, "-C", src, ".")
		case name == "t.zip", name == "t.7z":
			testutil.MustRun(t, "7z", "a", path, filepath.Join(src, "a.txt"), filepath.Join(src, "sub"))
		case name == "t.tar.gz":
			testutil.MustRun(t, "tar", "-cf", filepath.Join(dir, "tmp.tar"), "-C", src, ".")
			testutil.MustRun(t, "gzip", "-kf", filepath.Join(dir, "tmp.tar"))
			os.Rename(filepath.Join(dir, "tmp.tar.gz"), path)
		case name == "t.tar.bz2":
			testutil.MustRun(t, "tar", "-cf", filepath.Join(dir, "tmp.tar"), "-C", src, ".")
			testutil.MustRun(t, "bzip2", "-kf", filepath.Join(dir, "tmp.tar"))
			os.Rename(filepath.Join(dir, "tmp.tar.bz2"), path)
		case name == "t.tar.xz":
			testutil.MustRun(t, "tar", "-cf", filepath.Join(dir, "tmp.tar"), "-C", src, ".")
			testutil.MustRun(t, "xz", "-kf", filepath.Join(dir, "tmp.tar"))
			os.Rename(filepath.Join(dir, "tmp.tar.xz"), path)
		case name == "t.tar.zst":
			testutil.MustRun(t, "tar", "-cf", filepath.Join(dir, "tmp.tar"), "-C", src, ".")
			testutil.MustRun(t, "zstd", "-q", "-f", filepath.Join(dir, "tmp.tar"), "-o", path)
		}
	}

	engine := core.NewEngine(format.Default())

	for _, name := range archives {
		t.Run(name, func(t *testing.T) {
			src := filepath.Join(dir, name)
			outDir := t.TempDir()

			opts := core.DefaultOptions()
			opts.OutputDir = outDir

			summary, err := engine.Extract(context.Background(), []string{src}, opts)
			if err != nil {
				t.Fatalf("解压失败：%v", err)
			}
			if summary.Failed > 0 {
				t.Fatalf("有 %d 个文件失败：%v", summary.Failed, summary.Files[0].Err)
			}
			if summary.Succeeded == 0 {
				t.Fatal("没有成功解压任何文件")
			}
			if summary.TotalBytes == 0 {
				t.Error("解出字节数为 0")
			}
			t.Logf("结果：%+v", summary.Files[0])
		})
	}
}

// TestEndToEndNestedArchive 验证嵌套压缩被逐层解开。
//
// 构造 a.tar.gz → a.tar → 目录 的三层结构，
// 引擎应当自动解开两层并最终得到目录。
func TestEndToEndNestedArchive(t *testing.T) {
	if !testutil.HaveTool("tar") || !testutil.HaveTool("gzip") {
		t.Skip("缺少 tar/gzip，跳过")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "deep.txt"), []byte("deep content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 第一层：src → inner.tar
	innerTar := filepath.Join(dir, "inner.tar")
	testutil.MustRun(t, "tar", "-cf", innerTar, "-C", src, "deep.txt")

	// 第二层：inner.tar → outer.tar.gz
	outerTar := filepath.Join(dir, "outer.tar")
	testutil.MustRun(t, "tar", "-cf", outerTar, "-C", dir, "inner.tar")
	testutil.MustRun(t, "gzip", "-kf", outerTar)
	outerGz := filepath.Join(dir, "outer.tar.gz")

	outDir := t.TempDir()
	engine := core.NewEngine(format.Default())

	opts := core.DefaultOptions()
	opts.OutputDir = outDir

	summary, err := engine.Extract(context.Background(), []string{outerGz}, opts)
	if err != nil {
		t.Fatalf("解压失败：%v", err)
	}
	if summary.Failed > 0 {
		t.Fatalf("解压失败：%v", summary.Files[0].Err)
	}

	// 最终应当能找到 deep.txt（可能在多层子目录中）。
	found := testutil.FindFile(t, outDir, "deep.txt")
	if found == "" {
		t.Errorf("未找到 deep.txt，产物：%v", testutil.ListTree(t, outDir))
	} else {
		t.Logf("找到 deep.txt：%s", found)
	}

	// 源压缩包应被保留（KeepArchive=true）。
	if _, err := os.Stat(outerGz); err != nil {
		t.Errorf("源压缩包应被保留：%v", err)
	}
}

// TestEndToEndDirectoryTree 验证目录递归处理。
func TestEndToEndDirectoryTree(t *testing.T) {
	if !testutil.HaveTool("7z") {
		t.Skip("缺少 7z，跳过")
	}

	dir := t.TempDir()
	for _, sub := range []string{"d1", "d2/deep"} {
		full := filepath.Join(dir, sub)
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}
		content := filepath.Join(full, "f.txt")
		if err := os.WriteFile(content, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		archive := filepath.Join(full, "a.zip")
		testutil.MustRun(t, "7z", "a", archive, content)
	}

	outDir := t.TempDir()
	engine := core.NewEngine(format.Default())

	opts := core.DefaultOptions()
	opts.OutputDir = outDir
	opts.Recursive = true

	summary, err := engine.Extract(context.Background(), []string{dir}, opts)
	if err != nil {
		t.Fatalf("解压失败：%v", err)
	}
	if summary.TotalFiles != 2 {
		t.Errorf("期望处理 2 个压缩文件，实际 %d", summary.TotalFiles)
	}
	if summary.Failed != 0 {
		t.Errorf("期望 0 个失败，实际 %d", summary.Failed)
	}
}

// TestEndToEndRar 验证 RAR 端到端解压。
func TestEndToEndRar(t *testing.T) {
	src := "/tmp/opencode/rarfile-4.5/test/files/rar3-subdirs.rar"
	if _, err := os.Stat(src); err != nil {
		t.Skip("RAR 样本不存在，跳过")
	}

	outDir := t.TempDir()
	engine := core.NewEngine(format.Default())

	opts := core.DefaultOptions()
	opts.OutputDir = outDir

	summary, err := engine.Extract(context.Background(), []string{src}, opts)
	if err != nil {
		t.Fatalf("解压失败：%v", err)
	}
	if summary.Failed > 0 {
		t.Fatalf("解压失败：%v", summary.Files[0].Err)
	}
	if summary.Files[0].Format != core.FormatRar {
		t.Errorf("格式识别错误：%v", summary.Files[0].Format)
	}
}
