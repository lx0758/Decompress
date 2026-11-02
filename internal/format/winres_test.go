// 本文件验证 Windows 版本资源的生成流程。
//
// 版本资源（PE 的 .rsrc 段）的作用有两个：
//
//   - 产物的文件属性里能看出产品名、公司名与版本号，
//     用户可以据此确认拿到的是哪个程序；
//   - 带完整元数据的可执行文件更不容易被杀毒软件按
//     "来源不明的可疑程序"误判。
//
// 这些用例需要交叉编译 Windows 产物，因此比普通单元测试慢。
package format_test

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// peResourceDirSize 读取 PE 文件的资源目录大小。
//
// 返回 0 表示没有资源段。解析步骤：
//
//	0x3c        → PE 头偏移
//	PE+0x14     → 可选头大小
//	可选头+0x18 → 数据目录起始（PE32 为 +96，PE32+ 为 +112）
//	数据目录+16 → 资源目录项（RVA 与大小）
func peResourceDirSize(data []byte) uint32 {
	if len(data) < 0x40 {
		return 0
	}

	peOff := binary.LittleEndian.Uint32(data[0x3c:])
	if peOff+24 > uint32(len(data)) || string(data[peOff:peOff+4]) != "PE\x00\x00" {
		return 0
	}

	optOff := peOff + 24

	// PE32 的 magic 是 0x10b，PE32+ 是 0x20b。
	magic := binary.LittleEndian.Uint16(data[optOff:])
	var dataDirOff uint32
	switch magic {
	case 0x10b:
		dataDirOff = optOff + 96
	case 0x20b:
		dataDirOff = optOff + 112
	default:
		return 0
	}

	// 资源目录是数据目录的第 3 项（索引 2），每项 8 字节。
	resOff := dataDirOff + 2*8
	if resOff+8 > uint32(len(data)) {
		return 0
	}
	return binary.LittleEndian.Uint32(data[resOff+4:])
}

// TestWindowsVersionResource 验证 Windows 产物带有版本资源。
//
// 用例跑一遍真实的构建流程：make winres 生成 .syso，
// 再交叉编译出 exe，检查资源段确实存在。
//
// 构建需要网络以下载 goversioninfo 工具（go.mod 的 tool 指令），
// 环境不具备时跳过。
func TestWindowsVersionResource(t *testing.T) {
	root, err := findRepoRoot()
	if err != nil {
		t.Skipf("定位仓库根目录失败：%v", err)
	}

	// go.mod 声明了 tool 指令，说明工具已锁定版本；
	// 离线环境下 go tool 会失败，此时跳过而不是让测试红。
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("缺少 go，跳过")
	}

	dir := t.TempDir()

	// 复制必要文件到临时目录，避免污染工作区。
	for _, name := range []string{"go.mod", "go.sum"} {
		copyFile(t, filepath.Join(root, name), filepath.Join(dir, name))
	}
	copyTree(t, filepath.Join(root, "cmd"), filepath.Join(dir, "cmd"))
	copyTree(t, filepath.Join(root, "internal"), filepath.Join(dir, "internal"))
	copyTree(t, filepath.Join(root, "build"), filepath.Join(dir, "build"))

	// 生成版本资源。
	gen := exec.Command("go", "tool", "goversioninfo", "-platform-specific",
		"-ver-major", "2026", "-ver-minor", "10", "-ver-patch", "1", "-ver-build", "0",
		"-product-ver-major", "2026", "-product-ver-minor", "10",
		"-product-ver-patch", "1", "-product-ver-build", "0",
		"-file-version", "2026.10.1.0", "-product-version", "2026.10.1.0",
		filepath.Join(dir, "build", "winres", "versioninfo.json"))
	gen.Dir = filepath.Join(dir, "cmd", "decompress")
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("生成版本资源失败（可能离线）：%v\n%s", err, out)
	}

	// 交叉编译 Windows 产物。
	exe := filepath.Join(dir, "decompress.exe")
	build := exec.Command("go", "build", "-trimpath", "-o", exe, "./cmd/decompress")
	build.Dir = dir
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=windows", "GOARCH=amd64")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("交叉编译失败：%v\n%s", err, out)
	}

	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}

	if size := peResourceDirSize(data); size == 0 {
		t.Error("Windows 产物没有版本资源段")
	}

	// 版本资源里的产品名应为 UTF-16LE 编码。
	if !strings.Contains(utf16leString(data), "decompress") {
		t.Error("版本资源中缺少产品名 decompress")
	}
}

// utf16leString 从二进制中提取 UTF-16LE 编码的可打印字符串。
//
// Windows 版本资源以 UTF-16LE 存储字符串，这里做一次宽松的解码，
// 便于断言其中包含预期的元数据。
func utf16leString(data []byte) string {
	var sb strings.Builder
	for i := 0; i+1 < len(data); i += 2 {
		c := binary.LittleEndian.Uint16(data[i : i+2])
		if c == 0 {
			sb.WriteByte('\n')
			continue
		}
		if c < 0x20 || c > 0x7e {
			sb.WriteByte(' ')
			continue
		}
		sb.WriteRune(rune(c))
	}
	return sb.String()
}

// findRepoRoot 定位仓库根目录。
//
// 从当前测试文件的位置向上查找含 go.mod 的目录。
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

// copyFile 复制单个文件。
func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("读取 %s 失败：%v", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// copyTree 递归复制目录。
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		copyFile(t, path, target)
		return nil
	})
	if err != nil {
		t.Fatalf("复制目录 %s 失败：%v", src, err)
	}
}
