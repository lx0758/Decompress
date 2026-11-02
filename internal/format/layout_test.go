package format

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/liux/decompress/internal/core"
)

// TestArchiveDirName 验证归档目录名的推导规则。
//
// 目录名取归档文件名去掉最后一个扩展名；没有扩展名时追加 ".d"，
// 避免产出的目录与源文件同名。
func TestArchiveDirName(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"onekeylog.zip", "onekeylog"},
		{"a.tar.gz", "a.tar"},
		{"ap_log.tar.zst.zip", "ap_log.tar.zst"},
		{"data", "data.d"},
		{"logs/main_log_2026_0327.zst", "main_log_2026_0327"},
	}

	for _, tc := range cases {
		if got := archiveDirName(tc.src); got != tc.want {
			t.Errorf("archiveDirName(%q) = %q，期望 %q", tc.src, got, tc.want)
		}
	}
}

// TestPlanLayout 验证输出布局决策。
//
// 嵌套归档（由解压衍生的子包）沿用"按顶层条目数决定"的智能规则：
// 0 个条目不产出，1 个条目平铺，多个条目建以归档名命名的目录。
//
// 顶层输入（用户直接指定的归档）强制建目录，不看条目数——归档名里
// 往往带着机器名、时间戳等信息，平铺会让这些信息丢失。
func TestPlanLayout(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		entries  []core.Entry
		forceDir bool
		wantKind layoutKind
		wantRoot string
	}{
		{
			name:     "空归档不产出",
			src:      "a.zip",
			entries:  nil,
			wantKind: layoutFlat,
			wantRoot: "",
		},
		{
			name:     "单条目平铺",
			src:      "a.zip",
			entries:  []core.Entry{{Name: "a.txt"}},
			wantKind: layoutFlat,
			wantRoot: "out",
		},
		{
			name:     "多条目建目录",
			src:      "a.zip",
			entries:  []core.Entry{{Name: "a.txt"}, {Name: "b.txt"}},
			wantKind: layoutDirectory,
			wantRoot: filepath.Join("out", "a"),
		},
		{
			name:     "同目录多个文件算一个顶层条目",
			src:      "a.zip",
			entries:  []core.Entry{{Name: "d/a.txt"}, {Name: "d/b.txt"}},
			wantKind: layoutFlat,
			wantRoot: "out",
		},
		{
			name:     "强制目录时单条目也建目录",
			src:      "onekeylog.zip",
			entries:  []core.Entry{{Name: "a.txt"}},
			forceDir: true,
			wantKind: layoutDirectory,
			wantRoot: filepath.Join("out", "onekeylog"),
		},
		{
			name:     "强制目录时无扩展名追加d后缀",
			src:      "data",
			entries:  []core.Entry{{Name: "a.txt"}},
			forceDir: true,
			wantKind: layoutDirectory,
			wantRoot: filepath.Join("out", "data.d"),
		},
		{
			name:     "强制目录时空归档仍不产出",
			src:      "empty.zip",
			entries:  nil,
			forceDir: true,
			wantKind: layoutFlat,
			wantRoot: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := planLayout(tc.src, "out", tc.entries, tc.forceDir)
			if l.kind != tc.wantKind {
				t.Errorf("布局类型 = %v，期望 %v", l.kind, tc.wantKind)
			}
			if l.root != tc.wantRoot {
				t.Errorf("根目录 = %q，期望 %q", l.root, tc.wantRoot)
			}
		})
	}
}

// TestPlanStreamEntry 验证单流格式的输出命名。
//
// 输出名去掉压缩后缀，并还原内层后缀：复合后缀（.tgz/.tbz2/.txz/.tzst）
// 解出来的其实是一个 tar 包，还原成 .tar 后，后续的归档层才能正确识别，
// 目录名也不会残留多余的后缀。
func TestPlanStreamEntry(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name     string
		file     string
		suffixes []streamSuffix
		want     string
	}{
		{
			name:     "gz去掉后缀",
			file:     "main_log.gz",
			suffixes: []streamSuffix{{Compressed: ".gz"}},
			want:     "main_log",
		},
		{
			name:     "tgz还原为tar",
			file:     "log.tgz",
			suffixes: []streamSuffix{{Compressed: ".gz"}, {Compressed: ".tgz", Inner: ".tar"}},
			want:     "log.tar",
		},
		{
			name:     "tar.gz只去掉gz",
			file:     "log.tar.gz",
			suffixes: []streamSuffix{{Compressed: ".gz"}},
			want:     "log.tar",
		},
		{
			name:     "无对应后缀追加d",
			file:     "data",
			suffixes: []streamSuffix{{Compressed: ".gz"}},
			want:     "data.d",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.file)
			if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}

			entries, err := planStreamEntry(path, tc.suffixes)
			if err != nil {
				t.Fatalf("构造条目失败：%v", err)
			}
			if len(entries) != 1 {
				t.Fatalf("期望 1 个条目，实际 %d", len(entries))
			}
			if entries[0].Name != tc.want {
				t.Errorf("输出名 = %q，期望 %q", entries[0].Name, tc.want)
			}
		})
	}
}

// TestResolveLayoutRoot 验证同名目录冲突的处理。
func TestResolveLayoutRoot(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "a")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}

	// 目录已存在且不允许覆盖时追加序号。
	l := layout{kind: layoutDirectory, root: base}
	got := resolveLayoutRoot(l, false)
	if got == base {
		t.Errorf("同名目录应被改名，实际仍为 %q", got)
	}

	// 允许覆盖时保持原名。
	if got := resolveLayoutRoot(l, true); got != base {
		t.Errorf("允许覆盖时应保持原名，实际 %q", got)
	}
}
