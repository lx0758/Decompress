// 本文件验证调度引擎的队列与嵌套解压行为：子归档的落点、层级保持、
// 递归处理与派生归档清理。同属 core 的外部测试包，原因见 engine_e2e_test.go。
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

// TestNestedArchiveScenarios 验证归档内含子压缩包时的行为。
//
// 衍生出来的子压缩包解压完后会被自动删除，只保留解出的内容；
// 用户直接指定的源压缩包永远保留。
func TestNestedArchiveScenarios(t *testing.T) {
	if !testutil.HaveTool("zip") {
		t.Skip("缺少 zip，跳过")
	}

	dir := t.TempDir()

	// 内层子包：含一个文件。
	innerDir := filepath.Join(dir, "inner")
	childZip := filepath.Join(innerDir, "child.zip")
	testutil.BuildZip(t, childZip, map[string]string{"deep/data.txt": "payload"})

	// 外层包：只含子包。
	outerSingle := filepath.Join(dir, "outer_single.zip")
	testutil.BuildZipFromFiles(t, outerSingle, map[string]string{"child.zip": childZip})

	// 外层包：子包 + 普通文件。
	outerMixed := filepath.Join(dir, "outer_mixed.zip")
	testutil.BuildZipFromFiles(t, outerMixed, map[string]string{
		"child.zip": childZip,
		"plain.txt": testutil.MustWrite(t, filepath.Join(dir, "plain.txt"), "plain"),
	})

	t.Run("只含子压缩包", func(t *testing.T) {
		outDir := filepath.Join(dir, "out_single")
		opts := core.DefaultOptions()
		opts.OutputDir = outDir

		// 每个子测试使用独立的引擎，避免已处理记录互相影响。
		summary, err := core.NewEngine(format.Default()).Extract(context.Background(), []string{outerSingle}, opts)
		if err != nil {
			t.Fatalf("解压失败：%v", err)
		}
		if summary.Failed > 0 {
			t.Fatalf("解压失败：%v", summary.Files[0].Err)
		}

		// 最终内容必须存在。
		if testutil.FindFile(t, outDir, "data.txt") == "" {
			t.Errorf("未找到 data.txt，产物：%v", testutil.ListTree(t, outDir))
		}
		// 子压缩包应被清理，产物里不留中间态压缩包。
		if testutil.FindFile(t, outDir, "child.zip") != "" {
			t.Errorf("子压缩包应被清理，产物：%v", testutil.ListTree(t, outDir))
		}
	})

	t.Run("子压缩包加普通文件", func(t *testing.T) {
		outDir := filepath.Join(dir, "out_mixed")
		opts := core.DefaultOptions()
		opts.OutputDir = outDir

		summary, err := core.NewEngine(format.Default()).Extract(context.Background(), []string{outerMixed}, opts)
		if err != nil {
			t.Fatalf("解压失败：%v", err)
		}
		if summary.Failed > 0 {
			t.Fatalf("解压失败：%v", summary.Files[0].Err)
		}

		if testutil.FindFile(t, outDir, "data.txt") == "" {
			t.Errorf("未找到 data.txt，产物：%v", testutil.ListTree(t, outDir))
		}
		if testutil.FindFile(t, outDir, "plain.txt") == "" {
			t.Errorf("未找到 plain.txt，产物：%v", testutil.ListTree(t, outDir))
		}
		// 子压缩包应被清理，普通文件保留。
		if testutil.FindFile(t, outDir, "child.zip") != "" {
			t.Errorf("子压缩包应被清理，产物：%v", testutil.ListTree(t, outDir))
		}
	})

	t.Run("源压缩包永远保留", func(t *testing.T) {
		outDir := filepath.Join(dir, "out_source")
		opts := core.DefaultOptions()
		opts.OutputDir = outDir

		summary, err := core.NewEngine(format.Default()).Extract(context.Background(), []string{outerSingle}, opts)
		if err != nil {
			t.Fatalf("解压失败：%v", err)
		}
		if summary.Failed > 0 {
			t.Fatalf("解压失败：%v", summary.Files[0].Err)
		}

		// 用户直接指定的源文件不能被删掉，否则等于破坏用户数据。
		if _, err := os.Stat(outerSingle); err != nil {
			t.Errorf("源压缩包应被保留：%v", err)
		}
	})
}

// TestNestedOutputPreservesHierarchy 验证嵌套解压保持目录层级。
//
// 子归档解到它自己的父目录，内容不会脱离原有层级。
//
// 顶层 outer.zip 走新规则（一律建以归档名命名的目录），因此层级里
// 多了 outer/ 这一层；子包 child.zip 维持旧规则，多条目时建同名目录。
func TestNestedOutputPreservesHierarchy(t *testing.T) {
	if !testutil.HaveTool("zip") {
		t.Skip("缺少 zip，跳过")
	}

	dir := t.TempDir()

	// 子包：多个文件 → 解出时建同名目录。
	child := filepath.Join(dir, "child.zip")
	testutil.BuildZip(t, child, map[string]string{"a.txt": "a", "b.txt": "b"})

	// 外层包：顶层只有 src/ 一个条目。
	outer := filepath.Join(dir, "outer.zip")
	testutil.BuildZipFromFiles(t, outer, map[string]string{"src/child.zip": child})

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

	// 子包内容应留在 src/ 层级内，而不是散落到输出根目录。
	for _, rel := range []string{"outer/src/child/a.txt", "outer/src/child/b.txt"} {
		full := filepath.Join(outDir, filepath.FromSlash(rel))
		if _, err := os.Stat(full); err != nil {
			t.Errorf("期望产物不存在：%s（实际产物 %v）", full, testutil.ListTree(t, outDir))
		}
	}

	// 不应散落到根目录。
	scattered := filepath.Join(outDir, "child")
	if _, err := os.Stat(scattered); err == nil {
		t.Errorf("子包内容不应脱离层级散落：%v", testutil.ListTree(t, outDir))
	}
}

// TestQueueProcessesNestedArchives 验证队列会递归处理嵌套压缩包。
func TestQueueProcessesNestedArchives(t *testing.T) {
	if !testutil.HaveTool("zip") {
		t.Skip("缺少 zip，跳过")
	}

	dir := t.TempDir()

	// 最内层。
	inner := filepath.Join(dir, "inner.zip")
	testutil.BuildZip(t, inner, map[string]string{"payload.txt": "data"})

	// 中间层：含 inner.zip。
	middle := filepath.Join(dir, "middle.zip")
	testutil.BuildZipFromFiles(t, middle, map[string]string{"inner.zip": inner})

	// 外层：含 middle.zip。
	outer := filepath.Join(dir, "outer.zip")
	testutil.BuildZipFromFiles(t, outer, map[string]string{"middle.zip": middle})

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

	// 三层都应被处理。
	if summary.TotalFiles != 3 {
		t.Errorf("期望处理 3 个压缩包，实际 %d", summary.TotalFiles)
	}

	// 最终内容应存在。
	if testutil.FindFile(t, outDir, "payload.txt") == "" {
		t.Errorf("未找到 payload.txt，产物：%v", testutil.ListTree(t, outDir))
	}
}
