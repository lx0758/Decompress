// 本文件是 format 的外部测试包，验证智能输出布局与预扫描契约。
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

// TestSmartOutputLayout 验证智能输出布局规则。
//
// 规则：
//   - 归档顶层只有一个条目 → 平铺，产物直接落在输出根目录
//   - 归档顶层有多个条目   → 建以归档名命名的子目录
//   - 归档没有扩展名       → 目录名为 "文件名.d"
//   - 归档内没有任何文件   → 不产出
func TestSmartOutputLayout(t *testing.T) {
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
			name:        "单文件平铺",
			archiveName: "one.zip",
			files:       map[string]string{"a.txt": "hello"},
			wantPaths:   []string{"a.txt"},
			// 不应建目录
			wantAbsent: []string{"one/a.txt"},
		},
		{
			name:        "单个文件夹内含文件平铺",
			archiveName: "one.zip",
			files:       map[string]string{"sub/b.txt": "hi"},
			wantPaths:   []string{"sub/b.txt"},
			wantAbsent:  []string{"one/sub/b.txt"},
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
