// 本文件是 format 的外部测试包，验证输出布局规则与预扫描契约。
//
// 这些用例通过 core.Engine 驱动真实 ZIP 来观察最终产物布局，
// 只依赖 format 的公开接口（Registry / Compression），因此使用
// 外部测试包而非内部测试包。
package format_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/liux/decompress/internal/core"
	"github.com/liux/decompress/internal/format"
	"github.com/liux/decompress/internal/testutil"
)

// TestTopLevelAlwaysUsesArchiveDir 验证顶层输入的落盘规则。
//
// 用户直接指定的归档一律落在以归档名命名的目录里，不看归档内有几个
// 条目——日志包的文件名里往往带着机器名与时间戳，落到目录名上才不会
// 在解压后丢失。
//
//   - "one.zip"        → out/one/
//   - "many.zip"       → out/many/
//   - "data"（无后缀） → out/data.d/
//   - 空归档           → 不产出
func TestTopLevelAlwaysUsesArchiveDir(t *testing.T) {
	if !testutil.HaveTool("zip") {
		t.Skip("缺少 zip，跳过")
	}

	cases := []struct {
		name string
		// archiveName 是归档文件名（含或不含扩展名）。
		archiveName string
		// files 是归档内的文件内容。
		files map[string]string
		// wantPaths 是期望存在的产物（相对输出根目录）。
		wantPaths []string
		// wantAbsent 是期望不存在的产物。
		wantAbsent []string
	}{
		{
			name:        "单文件也建目录",
			archiveName: "one.zip",
			files:       map[string]string{"a.txt": "hello"},
			wantPaths:   []string{"one/a.txt"},
			// 不应平铺到输出根目录
			wantAbsent: []string{"a.txt"},
		},
		{
			name:        "单个文件夹内含文件也建目录",
			archiveName: "one.zip",
			files:       map[string]string{"sub/b.txt": "hi"},
			wantPaths:   []string{"one/sub/b.txt"},
			wantAbsent:  []string{"sub/b.txt"},
		},
		{
			name:        "多文件建目录",
			archiveName: "many.zip",
			files:       map[string]string{"f1.txt": "1", "f2.txt": "2"},
			wantPaths:   []string{"many/f1.txt", "many/f2.txt"},
			// 不应平铺
			wantAbsent: []string{"f1.txt"},
		},
		{
			name:        "多个文件夹建目录",
			archiveName: "dirs.zip",
			files:       map[string]string{"d1/a.txt": "x", "d2/b.txt": "y"},
			wantPaths:   []string{"dirs/d1/a.txt", "dirs/d2/b.txt"},
		},
		{
			name:        "单文件加单个文件夹建目录",
			archiveName: "pkg.zip",
			files:       map[string]string{"a.txt": "a", "sub/b.txt": "b"},
			wantPaths:   []string{"pkg/a.txt", "pkg/sub/b.txt"},
		},
		{
			name:        "无扩展名建d后缀目录",
			archiveName: "data",
			files:       map[string]string{"f1.txt": "1", "f2.txt": "2"},
			wantPaths:   []string{"data.d/f1.txt", "data.d/f2.txt"},
		},
		{
			name:        "空归档不产出",
			archiveName: "empty.zip",
			files:       map[string]string{},
			wantPaths:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, tc.archiveName)
			testutil.BuildZip(t, src, tc.files)

			outDir := filepath.Join(dir, "out")
			opts := core.DefaultOptions()
			opts.OutputDir = outDir

			summary, err := core.NewEngine(format.Default()).Extract(context.Background(), []string{src}, opts)
			if err != nil {
				t.Fatalf("解压失败：%v", err)
			}
			if summary.Failed > 0 {
				t.Fatalf("解压失败：%v", summary.Files[0].Err)
			}

			for _, rel := range tc.wantPaths {
				full := filepath.Join(outDir, filepath.FromSlash(rel))
				if _, err := os.Stat(full); err != nil {
					t.Errorf("期望产物不存在：%s（实际产物 %v）", full, testutil.ListTree(t, outDir))
				}
			}
			for _, rel := range tc.wantAbsent {
				full := filepath.Join(outDir, filepath.FromSlash(rel))
				if _, err := os.Stat(full); err == nil {
					t.Errorf("不应存在的产物：%s", full)
				}
			}
		})
	}
}

// TestTopLevelTarGzDirName 验证顶层 tar.gz 的目录名不含多余的扩展名。
//
// 旧版行为：GZIP 作为顶层输入会建目录，目录名只去掉最后一个扩展名，
// 于是 log_20261102_102646.tar.gz 解出 log_20261102_102646.tar/，
// 多出一层无意义的 ".tar"。
//
// 现在单流格式恒为平铺：顶层 tar.gz 先平铺出同名 .tar，
// 该 .tar 再按归档规则落位，目录名为 log_20261102_102646/。
func TestTopLevelTarGzDirName(t *testing.T) {
	if !testutil.HaveTool("tar") || !testutil.HaveTool("gzip") {
		t.Skip("缺少 tar/gzip，跳过")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 构造 log_20261102_102646.tar.gz。
	tarPath := filepath.Join(dir, "log_20261102_102646.tar")
	testutil.MustRun(t, "tar", "-cf", tarPath, "-C", src, "a.txt", "b.txt")
	gzPath := tarPath + ".gz"
	testutil.MustRun(t, "gzip", "-kf", tarPath)

	outDir := filepath.Join(dir, "out")
	opts := core.DefaultOptions()
	opts.OutputDir = outDir

	summary, err := core.NewEngine(format.Default()).Extract(context.Background(), []string{gzPath}, opts)
	if err != nil {
		t.Fatalf("解压失败：%v", err)
	}
	if summary.Failed > 0 {
		t.Fatalf("解压失败：%v", summary.Files[0].Err)
	}

	// 目录名应去掉全部压缩后缀。
	for _, rel := range []string{"log_20261102_102646/a.txt", "log_20261102_102646/b.txt"} {
		full := filepath.Join(outDir, filepath.FromSlash(rel))
		if _, err := os.Stat(full); err != nil {
			t.Errorf("期望产物不存在：%s（实际产物 %v）", full, testutil.ListTree(t, outDir))
		}
	}

	// 不应残留带多余扩展名的目录。
	for _, rel := range []string{"log_20261102_102646.tar", "log_20261102_102646.tar/a.txt"} {
		full := filepath.Join(outDir, filepath.FromSlash(rel))
		if _, err := os.Stat(full); err == nil {
			t.Errorf("不应存在的产物：%s（实际产物 %v）", full, testutil.ListTree(t, outDir))
		}
	}
}

// TestTopLevelTarGzSingleEntryUsesDir 验证顶层 tar.gz 即使只有一个顶层条目
// 也建目录。
//
// 顶层解压链上的归档类格式一律建以归档名命名的目录，不按条目数决定
// 平铺与否。否则 log2.tar.gz（内容只有一个 sdcard/ 目录）会解出
// sdcard/，文件名里的信息就丢了。
func TestTopLevelTarGzSingleEntryUsesDir(t *testing.T) {
	if !testutil.HaveTool("tar") || !testutil.HaveTool("gzip") {
		t.Skip("缺少 tar/gzip，跳过")
	}

	dir := t.TempDir()

	// 内容只有一个顶层目录。
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "sdcard"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sdcard", "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}

	tarPath := filepath.Join(dir, "log2.tar")
	testutil.MustRun(t, "tar", "-cf", tarPath, "-C", src, "sdcard")
	gzPath := tarPath + ".gz"
	testutil.MustRun(t, "gzip", "-kf", tarPath)

	outDir := filepath.Join(dir, "out")
	opts := core.DefaultOptions()
	opts.OutputDir = outDir

	summary, err := core.NewEngine(format.Default()).Extract(context.Background(), []string{gzPath}, opts)
	if err != nil {
		t.Fatalf("解压失败：%v", err)
	}
	if summary.Failed > 0 {
		t.Fatalf("解压失败：%v", summary.Files[0].Err)
	}

	// 链上的 tar 层强制建目录，目录名为去掉全部压缩后缀的名字。
	want := filepath.Join(outDir, "log2", "sdcard", "a.txt")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("期望产物不存在：%s（实际产物 %v）", want, testutil.ListTree(t, outDir))
	}

	// 不应平铺出 sdcard/ 目录。
	if _, err := os.Stat(filepath.Join(outDir, "sdcard")); err == nil {
		t.Errorf("链上的归档不应平铺（实际产物 %v）", testutil.ListTree(t, outDir))
	}
}

// TestNestedArchiveKeepsSmartLayout 验证嵌套子包沿用按条目数布局的旧规则。
//
// 顶层输入改了规则，嵌套衍生的子压缩包不受影响：子包内只有一个顶层条目
// 时平铺，多个时建以子包名命名的目录。
func TestNestedArchiveKeepsSmartLayout(t *testing.T) {
	if !testutil.HaveTool("zip") {
		t.Skip("缺少 zip，跳过")
	}

	dir := t.TempDir()

	// 子包：多个条目 → 解出时建同名目录。
	multi := filepath.Join(dir, "multi.zip")
	testutil.BuildZip(t, multi, map[string]string{"a.txt": "a", "b.txt": "b"})

	// 子包：单个条目 → 解出时平铺。
	single := filepath.Join(dir, "single.zip")
	testutil.BuildZip(t, single, map[string]string{"only.txt": "only"})

	// 外层包：顶层只有 child/ 一个条目 → 平铺（子包维持旧规则）。
	outer := filepath.Join(dir, "outer.zip")
	testutil.BuildZipFromFiles(t, outer, map[string]string{
		"child/multi.zip":  multi,
		"child/single.zip": single,
	})

	outDir := filepath.Join(dir, "out")
	opts := core.DefaultOptions()
	opts.OutputDir = outDir

	summary, err := core.NewEngine(format.Default()).Extract(context.Background(), []string{outer}, opts)
	if err != nil {
		t.Fatalf("解压失败：%v", err)
	}
	if summary.Failed > 0 {
		t.Fatalf("解压失败：%v", summary.Files[0].Err)
	}

	// 顶层 outer.zip 建目录；子包按旧规则落位。
	for _, rel := range []string{"outer/child/multi/a.txt", "outer/child/multi/b.txt", "outer/child/only.txt"} {
		full := filepath.Join(outDir, filepath.FromSlash(rel))
		if _, err := os.Stat(full); err != nil {
			t.Errorf("期望产物不存在：%s（实际产物 %v）", full, testutil.ListTree(t, outDir))
		}
	}
}

// TestTopLevelSingleStreamIsFlat 验证顶层单流输入直接输出文件。
//
// 单流格式（GZIP/BZIP2/XZ/ZSTD）内部没有条目概念，解压后恒产出一个
// 文件，因此任何时候都不建目录——顶层输入也不例外。
//
//   - "main_log.gz" → out/main_log
//   - 无后缀的 "data" → out/data.d
func TestTopLevelSingleStreamIsFlat(t *testing.T) {
	dir := t.TempDir()

	// 有压缩后缀：输出文件去掉后缀。
	withSuffix := filepath.Join(dir, "main_log.gz")
	testutil.BuildGzip(t, withSuffix, "log content")

	// 无压缩后缀：追加 ".d"。
	noSuffix := filepath.Join(dir, "data")
	testutil.BuildGzip(t, noSuffix, "raw content")

	outDir := filepath.Join(dir, "out")
	opts := core.DefaultOptions()
	opts.OutputDir = outDir

	summary, err := core.NewEngine(format.Default()).Extract(
		context.Background(), []string{withSuffix, noSuffix}, opts)
	if err != nil {
		t.Fatalf("解压失败：%v", err)
	}
	if summary.Failed > 0 {
		t.Fatalf("解压失败：%v", summary.Files[0].Err)
	}

	// 产物应直接落在输出根目录，且不带目录层级。
	for _, rel := range []string{"main_log", "data.d"} {
		full := filepath.Join(outDir, rel)
		info, err := os.Stat(full)
		if err != nil {
			t.Errorf("期望产物不存在：%s（实际产物 %v）", full, testutil.ListTree(t, outDir))
			continue
		}
		if info.IsDir() {
			t.Errorf("单流产物不应是目录：%s", full)
		}
	}

	// 不应存在以归档名命名的目录。
	for _, rel := range []string{"main_log.gz", "data"} {
		full := filepath.Join(outDir, rel)
		if _, err := os.Stat(full); err == nil {
			t.Errorf("单流输入不应产生目录：%s（实际产物 %v）", full, testutil.ListTree(t, outDir))
		}
	}
}

// TestNestedSingleStreamIsFlat 验证嵌套子压缩包中的单流文件不建目录。
//
// 子压缩包若是单流类型，解出来的文件直接落在它所在的目录里，
// 不额外套一层以包名命名的目录。
func TestNestedSingleStreamIsFlat(t *testing.T) {
	if !testutil.HaveTool("zip") {
		t.Skip("缺少 zip，跳过")
	}

	dir := t.TempDir()

	// 内层单流文件。
	inner := filepath.Join(dir, "inner.gz")
	testutil.BuildGzip(t, inner, "inner content")

	// 外层包：顶层只有 inner.gz 一个条目。
	outer := filepath.Join(dir, "outer.zip")
	testutil.BuildZipFromFiles(t, outer, map[string]string{"inner.gz": inner})

	outDir := filepath.Join(dir, "out")
	opts := core.DefaultOptions()
	opts.OutputDir = outDir

	summary, err := core.NewEngine(format.Default()).Extract(context.Background(), []string{outer}, opts)
	if err != nil {
		t.Fatalf("解压失败：%v", err)
	}
	if summary.Failed > 0 {
		t.Fatalf("解压失败：%v", summary.Files[0].Err)
	}

	// 顶层 outer.zip 建目录，单流子包平铺在其内。
	want := filepath.Join(outDir, "outer", "inner")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("期望产物不存在：%s（实际产物 %v）", want, testutil.ListTree(t, outDir))
	}

	// 不应为单流子包额外建目录。
	for _, rel := range []string{"outer/inner.gz", "outer/inner.gz/inner"} {
		full := filepath.Join(outDir, filepath.FromSlash(rel))
		if _, err := os.Stat(full); err == nil {
			t.Errorf("单流子包不应产生目录：%s（实际产物 %v）", full, testutil.ListTree(t, outDir))
		}
	}
}

// TestScanDoesNotExtract 验证预扫描不解压任何内容。
//
// 这是智能输出的性能前提：必须能在解压前就知道归档内容。
func TestScanDoesNotExtract(t *testing.T) {
	if !testutil.HaveTool("zip") {
		t.Skip("缺少 zip，跳过")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "a.zip")
	testutil.BuildZip(t, src, map[string]string{"a.txt": "hello", "b.txt": "world"})

	// 记录扫描前后的目录内容，确认扫描没有产生任何文件。
	before := testutil.ListTree(t, dir)

	ext := format.Default().Get(core.FormatZip)
	if ext == nil {
		t.Fatal("没有 ZIP 实现")
	}

	entries, err := ext.Scan(src)
	if err != nil {
		t.Fatalf("预扫描失败：%v", err)
	}
	if len(entries) != 2 {
		t.Errorf("期望 2 个条目，实际 %d", len(entries))
	}

	after := testutil.ListTree(t, dir)
	if len(before) != len(after) {
		t.Errorf("预扫描不应产生文件：前 %v，后 %v", before, after)
	}
}
