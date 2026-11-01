package core

import (
	"io"
	"os"
	"strings"
)

// headerSize 是读取用于魔数识别的字节数。
//
// 取 512 字节以覆盖 TAR 的偏移 257，同时足够识别其它格式。
const headerSize = 512

// Detect 根据注册表中的魔数规则判断文件的压缩格式。
//
// 只读取文件的前 headerSize 字节，不做全文件扫描，性能开销恒定。
//
// 魔数识别失败时回退到扩展名判断。返回值是格式标识，
// 无法识别时返回空串。
func (r *Registry) Detect(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	head := make([]byte, headerSize)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return ""
	}

	// 先用魔数判断。
	if name := r.detectByMagic(head[:n]); name != "" {
		return name
	}

	// 回退到扩展名判断，兼容改过扩展名或扩展名不规范的文件。
	return r.detectByExtension(path)
}

// detectByExtension 依据文件扩展名判断格式。
func (r *Registry) detectByExtension(path string) string {
	lower := strings.ToLower(path)
	exts := r.Extensions()

	// 从长到短匹配扩展名，确保 ".tar.gz" 这类复合扩展名
	// 优先于 ".gz" 命中。
	candidates := make([]string, 0, len(exts))
	for ext := range exts {
		candidates = append(candidates, ext)
	}
	sortByLengthDesc(candidates)

	for _, ext := range candidates {
		if strings.HasSuffix(lower, ext) {
			return exts[ext]
		}
	}
	return ""
}

// sortByLengthDesc 按长度降序排序（相同长度时按字典序）。
func sortByLengthDesc(s []string) {
	sortByFunc(s, func(a, b string) bool {
		if len(a) != len(b) {
			return len(a) > len(b)
		}
		return a < b
	})
}

// sortByFunc 是一个简单插入排序，供小规模切片使用。
func sortByFunc(s []string, less func(a, b string) bool) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && less(s[j], s[j-1]); j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
