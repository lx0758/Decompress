package core

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ErrUnsafePath 表示归档条目名试图逃逸出目标目录（Zip Slip 攻击）。
type ErrUnsafePath struct {
	// EntryName 是原始条目名。
	EntryName string
	// Reason 说明拒绝原因。
	Reason string
}

// Error 实现 error 接口。
func (e *ErrUnsafePath) Error() string {
	return fmt.Sprintf("拒绝不安全的归档条目路径 %q: %s", e.EntryName, e.Reason)
}

// SanitizeEntryPath 将归档内的条目名转换为目标目录下的安全绝对路径。
//
// 该函数用于防御 Zip Slip 漏洞：恶意归档可以使用 ".." 或绝对路径把文件
// 写到目标目录之外。处理步骤：
//
//  1. 统一分隔符（归档内部一律使用 "/"）
//  2. 拒绝绝对路径与盘符前缀
//  3. 逐段解析，拒绝任何试图逃逸出根目录的 ".."
//  4. 清理 "." 与重复分隔符
//
// 归一化后为空的名字（如 tar 中的 "./"）表示归档根目录，此时返回
// destDir 本身。调用方需区分"根目录条目"与"普通文件"，具体见
// writeArchiveEntry。
//
// 返回的路径一定位于 destDir 之内。遇到不安全路径时返回 ErrUnsafePath。
func SanitizeEntryPath(destDir, entryName string) (string, error) {
	// 归档统一使用 "/" 作为分隔符。
	normalized := strings.ReplaceAll(entryName, "\\", "/")

	// 拒绝绝对路径（POSIX 风格与 Windows 盘符风格）。
	if strings.HasPrefix(normalized, "/") {
		return "", &ErrUnsafePath{EntryName: entryName, Reason: "条目名是绝对路径"}
	}
	if len(normalized) >= 2 && normalized[1] == ':' {
		return "", &ErrUnsafePath{EntryName: entryName, Reason: "条目名包含盘符前缀"}
	}

	// 逐段解析，构造安全的相对路径。
	segments := strings.Split(normalized, "/")
	cleaned := make([]string, 0, len(segments))
	for _, seg := range segments {
		switch seg {
		case "", ".":
			// 空段与 "." 直接跳过。
			continue
		case "..":
			// 一旦出现 ".."，必须能安全地弹出一层已有目录。
			if len(cleaned) == 0 {
				return "", &ErrUnsafePath{EntryName: entryName, Reason: "条目路径逃逸出目标目录"}
			}
			cleaned = cleaned[:len(cleaned)-1]
		default:
			cleaned = append(cleaned, seg)
		}
	}

	if len(cleaned) == 0 {
		// 归档根目录条目（如 tar 的 "./"）。
		return destDir, nil
	}

	// 保留原始分隔符语义：目标目录用 filepath.Join 拼接。
	return filepath.Join(append([]string{destDir}, cleaned...)...), nil
}

// IsWithinDir 判断 path 是否位于 dir 之内（含 dir 本身）。
//
// 用于二次校验，防止符号链接或非常规路径绕过 SanitizeEntryPath。
func IsWithinDir(dir, path string) bool {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}
