package util

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// 本文件提供字符编码层面的通用操作。

// gbkDecoder 是 GBK 解码器，全局复用避免重复初始化。
var gbkDecoder = simplifiedchinese.GBK.NewDecoder()

// DecodeGBK 尝试把字节串按 GBK 解码为 UTF-8 字符串。
//
// 若输入本身已是合法 UTF-8，或 GBK 解码失败，则返回原始值与 false，
// 由调用方决定是否回退。
//
// 解码结果若含替换字符（U+FFFD），同样视为失败：GBK 解码器遇到无效
// 字节不会报错而是产出替换字符，而替换字符本身是合法 UTF-8。此时若放行，
// 非 GBK 编码的文件名会被变成一串替换字符垃圾，比保留原样更糟。
func DecodeGBK(s string) (string, bool) {
	if s == "" {
		return s, false
	}
	// 已是合法 UTF-8，无需转换。
	if utf8.ValidString(s) {
		return s, false
	}

	decoded, _, err := transform.String(gbkDecoder, s)
	if err != nil {
		return s, false
	}
	if !utf8.ValidString(decoded) || strings.ContainsRune(decoded, utf8.RuneError) {
		return s, false
	}
	return decoded, true
}
