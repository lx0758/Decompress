package format

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/liux/decompress/internal/core"
	"github.com/liux/decompress/internal/util"
)

// 本文件负责归档的"智能输出布局"决策：
// 依据归档的顶层条目数量决定产物落在哪个目录。
//
// 布局规则：
//   - 顶层 0 个条目 → 不产出
//   - 顶层 1 个条目 → 平铺到输出根目录
//   - 顶层多个条目 → 建以归档名命名的子目录
//
// 这些规则是各归档格式共用的，因此放在公共层。
//
// 例外一：归档类格式（zip/tar/7z/rar）在"顶层解压链"上会启用
// "强制目录"模式（forceDir），一律建以归档名命名的目录，不再按
// 条目数区分平铺与否。
//
// 顶层解压链指从用户直接指定的输入出发、经由单流格式层层解开得到的
// 那条链（如 log.tar.gz → log.tar → 内容）。日志包的名字往往只写在
// 最外层的文件名上，链上的每一层各按自己的文件名去掉最后一个扩展名，
// 中间的压缩后缀不会残留到最终的目录名上（log.tar.gz → log/）。
//
// 归档内部嵌套的子压缩包不在链上，仍按"顶层条目数"决定布局。
//
// 例外二：单流格式（gzip/bzip2/xz/zstd）恒为平铺——它内部没有条目概念，
// 解压后只有一个文件，任何时候都不额外建目录。

// archiveEntry 是归档中一个待写出的条目，附带打开内容的方式。
type archiveEntry struct {
	// Entry 是条目的元数据。
	core.Entry

	// Open 打开条目内容；目录条目为 nil。
	Open func() (io.ReadCloser, error)
}

// layoutKind 描述解压产物的布局方式。
type layoutKind int

const (
	// layoutFlat 表示产物直接落在目标目录下，不建额外目录。
	layoutFlat layoutKind = iota

	// layoutDirectory 表示产物落在以归档名命名的子目录中。
	layoutDirectory
)

// layout 描述一次解压的输出布局。
type layout struct {
	// kind 是布局类型。
	kind layoutKind

	// root 是最终产物根目录。
	root string
}

// planLayout 依据归档的顶层条目数量决定输出布局。
//
// 目录名取归档文件名去掉最后一个扩展名；若归档没有扩展名，
// 则用 "文件名.d"。
//
// forceDir 为真时忽略条目数规则，一律建以归档名命名的目录。
// 该模式用于顶层解压链上的归档类格式：日志包的文件名里往往带着
// 机器名与时间戳，固定成目录可以把这些信息保留下来，
// 同时避免多个日志包的内容混在同一个目录里。
func planLayout(srcPath, dstDir string, entries []core.Entry, forceDir bool) layout {
	topLevel := countTopLevel(entries)

	if topLevel == 0 {
		// 空归档：不产出。
		return layout{kind: layoutFlat, root: ""}
	}

	// 强制目录：忽略条目数，一律建以归档名命名的目录。
	if forceDir {
		return layout{kind: layoutDirectory, root: archiveDirPath(srcPath, dstDir)}
	}

	if topLevel == 1 {
		// 单条目：平铺。
		return layout{kind: layoutFlat, root: dstDir}
	}

	// 多条目：建以归档名命名的目录。
	return layout{kind: layoutDirectory, root: archiveDirPath(srcPath, dstDir)}
}

// archiveDirName 生成归档对应的输出目录名。
//
//   - "a.zip"      → "a"
//   - "a.tar"      → "a"（只去最后一个扩展名）
//   - "data"       → "data.d"（无扩展名时追加 .d）
//
// 顶层解压链上每一层各去一个扩展名，因此 log.tar.gz 会在 gzip 层
// 变成 log.tar、在 tar 层变成 log，不会残留中间的压缩后缀。
func archiveDirName(srcPath string) string {
	base := filepath.Base(srcPath)

	// 去掉最后一个扩展名。
	name := util.StripExtension(base)

	// 没有可识别的名字（文件名就是 ".gz" 这类），退回到完整名。
	if name == "" {
		name = base
	}

	// 原文件没有扩展名时，用 ".d" 后缀避免与原文件同名。
	if name == base {
		return name + ".d"
	}
	return name
}

// archiveDirPath 返回归档对应的输出目录路径（未处理同名冲突）。
//
// 目录名由 archiveDirName 决定，去掉最后一个扩展名；无扩展名时追加 ".d"。
func archiveDirPath(srcPath, dstDir string) string {
	return filepath.Join(dstDir, archiveDirName(srcPath))
}

// streamSuffix 描述单流格式的一个压缩后缀。
type streamSuffix struct {
	// Compressed 是压缩后缀（含点），如 ".gz"。
	Compressed string

	// Inner 是解压后内容应保留的后缀（含点）。
	//
	// 单流格式解压后得到的其实是"内层文件"，后缀决定它是什么：
	// ".gz" 解出的是普通文件（无后缀），".tgz" 解出的则是 tar 包。
	// 复合后缀（.tgz/.tbz2/.txz/.tzst）据此还原成内层后缀，
	// 让后续的归档层能正确识别它，也让输出目录名保持干净。
	Inner string
}

// planStreamEntry 为单流格式构造合成的条目清单。
//
// 单流格式（gzip/bzip2/xz/zstd）内部没有条目概念，解压后恒产出一个
// 文件，因此这里合成单个条目供布局决策与命名使用。
//
// 输出文件名取原文件名去掉压缩后缀，并还原内层后缀：
//
//   - "main_log.zst"    → "main_log"
//   - "log.tar.gz"      → "log.tar"
//   - "log.tgz"         → "log.tar"
//   - "data"            → "data.d"（无对应后缀时追加 .d）
func planStreamEntry(path string, suffixes []streamSuffix) ([]core.Entry, error) {
	base := filepath.Base(path)

	outName := ""
	for _, s := range suffixes {
		if !strings.HasSuffix(base, s.Compressed) {
			continue
		}
		outName = strings.TrimSuffix(base, s.Compressed) + s.Inner
		break
	}

	if outName == "" {
		outName = base + ".d"
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	return []core.Entry{{
		Name:    outName,
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}}, nil
}

// countTopLevel 统计归档的顶层条目数量。
//
// 顶层条目按路径第一段去重：a/b.txt 与 a/c.txt 都属于顶层条目 "a"。
//
// 路径先做规范化：剥离开头的 "./" 段（tar 常用这种写法表示归档根），
// 规范化后为空表示这是归档自身的根条目，不计入统计。
func countTopLevel(entries []core.Entry) int {
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		name := normalizeEntryPath(e.Name)
		if name == "" {
			// 归档根条目，不计入。
			continue
		}
		first := name
		if idx := strings.Index(name, "/"); idx >= 0 {
			first = name[:idx]
		}
		seen[first] = true
	}
	return len(seen)
}

// normalizeEntryPath 规范化归档内的条目路径。
//
// 统一分隔符、剥离开头的 "./" 段与结尾的 "/"。
// 结果为空表示该条目指向归档根。
func normalizeEntryPath(name string) string {
	normalized := strings.ReplaceAll(name, "\\", "/")

	segments := strings.Split(normalized, "/")
	cleaned := make([]string, 0, len(segments))
	for _, seg := range segments {
		if seg == "" || seg == "." {
			continue
		}
		cleaned = append(cleaned, seg)
	}
	return strings.Join(cleaned, "/")
}

// resolveLayoutRoot 为归档布局确定最终的根目录，处理同名冲突。
//
// 多个归档可能解出同名目录（例如 a.zip 与 a.tar 都叫 "a"），
// 此时为后者追加序号，避免互相覆盖。
func resolveLayoutRoot(l layout, overwrite bool) string {
	if l.root == "" {
		return ""
	}
	if l.kind == layoutFlat {
		// 平铺布局直接落在目标目录，不改名。
		return l.root
	}
	if overwrite {
		return l.root
	}
	if _, err := os.Stat(l.root); err != nil {
		return l.root
	}

	base := l.root
	for i := 1; i < 10000; i++ {
		candidate := fmt.Sprintf("%s.%d", base, i)
		if _, err := os.Stat(candidate); err != nil {
			return candidate
		}
	}
	return l.root
}

// metaOf 提取条目清单的元数据部分，供布局决策使用。
func metaOf(entries []archiveEntry) []core.Entry {
	out := make([]core.Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Entry)
	}
	return out
}
