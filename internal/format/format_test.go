package format

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/liux/decompress/internal/core"
)

// fixtureDir 是测试样本目录。
const fixtureDir = "/tmp/opencode/mktest"

// testRegistry 返回内置注册表（含全部真实格式实现）。
func testRegistry() *core.Registry {
	return Default()
}

// doScan 对格式执行预扫描。
func doScan(t *testing.T, name, path string) []core.Entry {
	t.Helper()
	c := testRegistry().Get(name)
	if c == nil {
		t.Fatalf("没有 %s 实现", name)
	}
	entries, err := c.Scan(path)
	if err != nil {
		t.Fatalf("预扫描失败：%v", err)
	}
	return entries
}

// doExtract 对格式执行解压。
func doExtract(t *testing.T, name, src, dstDir string, opts core.Options) ([]string, int64, error) {
	t.Helper()
	c := testRegistry().Get(name)
	if c == nil {
		t.Fatalf("没有 %s 实现", name)
	}
	res, err := c.Extract(context.Background(), &core.ExtractRequest{
		Compression: c,
		SrcPath:     src,
		DstDir:      dstDir,
		Options:     opts,
	})
	if err != nil {
		return nil, 0, err
	}
	if res == nil {
		return nil, 0, nil
	}
	return res.OutPaths, res.Written, res.Err
}

// TestAllFormatsDecompress 验证 8 种格式都能正确解压。
func TestAllFormatsDecompress(t *testing.T) {
	cases := []struct {
		file   string
		format core.Format
		// expectFile 是相对"解压根目录"的期望产物路径。
		expectFile string
	}{
		{"t.zip", core.FormatZip, "t/a.txt"},
		{"t.tar", core.FormatTar, "t/a.txt"},
		{"t.tar.gz", core.FormatGzip, "t.tar"},
		{"t.tar.bz2", core.FormatBzip2, "t.tar"},
		{"t.tar.xz", core.FormatXz, "t.tar"},
		{"t.tar.zst", core.FormatZstd, "t.tar"},
		{"t.7z", core.Format7z, "t/a.txt"},
	}

	registry := testRegistry()

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			src := filepath.Join(fixtureDir, tc.file)
			if _, err := os.Stat(src); err != nil {
				t.Skipf("样本不存在，跳过：%s", src)
			}

			// 先确认格式识别正确。
			got := registry.Detect(src)
			if got != tc.format {
				t.Fatalf("格式识别错误：期望 %s，实际 %s", tc.format, got)
			}

			// Scan 必须能列出条目。
			entries := doScan(t, tc.format, src)
			if len(entries) == 0 {
				t.Errorf("预扫描条目数为 0")
			}
			t.Logf("预扫描得到 %d 个条目", len(entries))

			out := t.TempDir()
			outPaths, written, err := doExtract(t, tc.format, src, out, core.DefaultOptions())
			if err != nil {
				t.Fatalf("解压失败：%v", err)
			}
			if len(outPaths) == 0 {
				t.Errorf("解压产物为空")
			}
			if written == 0 {
				t.Errorf("写出字节数为 0")
			}
			t.Logf("产物=%v 字节=%d", outPaths, written)

			// 验证期望的文件确实存在。
			full := filepath.Join(out, filepath.FromSlash(tc.expectFile))
			if _, err := os.Stat(full); err != nil {
				t.Errorf("期望文件不存在：%s（产物 %v）", full, outPaths)
			}
		})
	}
}

// TestRarDecompress 验证 RAR 解压。
func TestRarDecompress(t *testing.T) {
	rarDir := "/tmp/opencode/rarfile-4.5/test/files"
	src := filepath.Join(rarDir, "rar3-subdirs.rar")
	if _, err := os.Stat(src); err != nil {
		t.Skip("RAR 样本不存在，跳过")
	}

	entries := doScan(t, core.FormatRar, src)
	if len(entries) != 10 {
		t.Errorf("期望 10 个条目，实际 %d", len(entries))
	}

	out := t.TempDir()
	outPaths, written, err := doExtract(t, core.FormatRar, src, out, core.DefaultOptions())
	if err != nil {
		t.Fatalf("解压失败：%v", err)
	}
	if len(outPaths) != 4 {
		t.Errorf("期望 4 个文件产物，实际 %d", len(outPaths))
	}
	if written == 0 {
		t.Error("写出字节数为 0")
	}
	t.Logf("产物=%v 字节=%d", outPaths, written)

	// 校验目录结构。
	for _, name := range []string{"sub/dir2/file2.txt", "sub/dir1/file1.txt"} {
		full := filepath.Join(out, filepath.FromSlash(name))
		if _, err := os.Stat(full); err != nil {
			t.Errorf("期望文件不存在：%s", full)
		}
	}
}

// TestRarEncrypted 验证加密 RAR 的密码处理。
func TestRarEncrypted(t *testing.T) {
	src := "/tmp/opencode/rarfile-4.5/test/files/rar5-psw.rar"
	if _, err := os.Stat(src); err != nil {
		t.Skip("加密 RAR 样本不存在，跳过")
	}

	// 无密码应报错。
	_, _, err := doExtract(t, core.FormatRar, src, t.TempDir(), core.DefaultOptions())
	if err == nil {
		t.Error("无密码时应当报错")
	} else {
		t.Logf("无密码错误信息：%v", err)
	}

	// 正确密码应成功。
	opts := core.DefaultOptions()
	opts.Password = "password"
	outPaths, _, err := doExtract(t, core.FormatRar, src, t.TempDir(), opts)
	if err != nil {
		t.Fatalf("正确密码解压失败：%v", err)
	}
	if len(outPaths) != 2 {
		t.Errorf("期望 2 个产物，实际 %d", len(outPaths))
	}
}

// TestRarMultiVolume 验证分卷 RAR。
func TestRarMultiVolume(t *testing.T) {
	src := "/tmp/opencode/rarfile-4.5/test/files/rar5-vols.part1.rar"
	if _, err := os.Stat(src); err != nil {
		t.Skip("分卷 RAR 样本不存在，跳过")
	}

	outPaths, written, err := doExtract(t, core.FormatRar, src, t.TempDir(), core.DefaultOptions())
	if err != nil {
		t.Fatalf("分卷解压失败：%v", err)
	}
	if len(outPaths) != 2 {
		t.Errorf("期望 2 个产物，实际 %d", len(outPaths))
	}
	if written == 0 {
		t.Error("写出字节数为 0")
	}
	t.Logf("产物=%v 字节=%d", outPaths, written)
}

// TestZipSlipProtection 验证 Zip Slip 漏洞已被修复。
func TestZipSlipProtection(t *testing.T) {
	dir := t.TempDir()
	malicious := filepath.Join(dir, "evil.zip")

	if err := createMaliciousZip(malicious); err != nil {
		t.Fatalf("构造恶意 ZIP 失败：%v", err)
	}

	out := t.TempDir()
	_, _, err := doExtract(t, core.FormatZip, malicious, out, core.DefaultOptions())
	if err == nil {
		t.Error("应当拒绝包含路径穿越的 ZIP")
	} else {
		t.Logf("正确拒绝了恶意条目：%v", err)
	}

	// 确认没有文件写到目标目录之外。
	escape := filepath.Join(filepath.Dir(out), "escaped.txt")
	if _, err := os.Stat(escape); err == nil {
		t.Errorf("恶意文件已逃逸到目标目录之外：%s", escape)
	}
}

// TestDetectByMagic 验证注册表的魔数识别覆盖全部内置格式。
func TestDetectByMagic(t *testing.T) {
	registry := testRegistry()

	cases := map[string]core.Format{
		"t.zip":     core.FormatZip,
		"t.tar":     core.FormatTar,
		"t.tar.gz":  core.FormatGzip,
		"t.tar.bz2": core.FormatBzip2,
		"t.tar.xz":  core.FormatXz,
		"t.tar.zst": core.FormatZstd,
		"t.7z":      core.Format7z,
	}

	for name, want := range cases {
		path := filepath.Join(fixtureDir, name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if got := registry.Detect(path); got != want {
			t.Errorf("%s 识别错误：期望 %s，实际 %s", name, want, got)
		}
	}
}

// TestDetectUnknown 验证普通文件不会被误判。
func TestDetectUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plain.txt")
	if err := os.WriteFile(path, []byte("hello world, not an archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := testRegistry().Detect(path); got != core.FormatUnknown {
		t.Errorf("普通文件不应被识别为压缩格式，实际 %s", got)
	}
}

// TestCompareWithReference 用 7z 作为参照，校验解压结果一致。
func TestCompareWithReference(t *testing.T) {
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("未安装 7z，跳过对照测试")
	}

	src := filepath.Join(fixtureDir, "t.zip")
	if _, err := os.Stat(src); err != nil {
		t.Skip("样本不存在")
	}

	// 用 7z 解压作为参照。
	refDir := t.TempDir()
	cmd := exec.Command("7z", "x", "-y", "-o"+refDir, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("7z 解压失败：%v\n%s", err, out)
	}

	// 用本实现解压。
	outPaths, _, err := doExtract(t, core.FormatZip, src, t.TempDir(), core.DefaultOptions())
	if err != nil {
		t.Fatalf("本实现解压失败：%v", err)
	}
	if len(outPaths) == 0 {
		t.Fatal("本实现没有产出文件")
	}

	ourFiles := make(map[string]string, len(outPaths))
	for _, p := range outPaths {
		ourFiles[filepath.Base(p)] = p
	}

	refFiles := collectFiles(t, refDir)
	if len(refFiles) != len(ourFiles) {
		t.Errorf("文件数量不一致：7z=%d 本实现=%d", len(refFiles), len(ourFiles))
	}

	for rel, refPath := range refFiles {
		ourPath, ok := ourFiles[filepath.Base(rel)]
		if !ok {
			t.Errorf("缺少文件：%s", rel)
			continue
		}
		if !sameContent(t, refPath, ourPath) {
			t.Errorf("文件内容不一致：%s", rel)
		}
	}
}

// TestEachFormatDeclaresItsTraits 验证各格式都正确声明了自身特征。
//
// 这是插件化的核心要求：格式自己声明魔数与扩展名，
// 注册表据此识别，无需在任何中心化配置中登记。
func TestEachFormatDeclaresItsTraits(t *testing.T) {
	registry := testRegistry()

	for _, c := range registry.All() {
		if c.Name() == "" {
			t.Error("存在未声明 Name 的格式")
		}
		if c.DisplayName() == "" {
			t.Errorf("%s 未声明 DisplayName", c.Name())
		}
		if len(c.Extensions()) == 0 {
			t.Errorf("%s 未声明扩展名", c.Name())
		}
		for _, ext := range c.Extensions() {
			if len(ext) < 2 || ext[0] != '.' {
				t.Errorf("%s 声明的扩展名格式不正确：%q", c.Name(), ext)
			}
		}
		if len(c.Magic()) == 0 {
			t.Errorf("%s 未声明魔数规则", c.Name())
		}
	}
}
