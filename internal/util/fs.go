package util

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 本文件提供文件系统与路径层面的通用操作。

// EnsureDir 确保目录存在。
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

// CreateOutput 创建输出文件，处理覆盖策略。
//
// 不允许覆盖且目标已存在时返回 *os.PathError（底层错误为 os.ErrExist），
// 由调用方决定是报错还是改用其它路径。
func CreateOutput(path string, overwrite bool) (*os.File, error) {
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return nil, &os.PathError{Op: "create", Path: path, Err: os.ErrExist}
		}
	}
	return os.Create(path)
}

// DirPerm 由权限位推导目录权限，缺省为 0755。
//
// 目录必须具备可执行位才能被进入，因此这里强制补上 0700。
func DirPerm(mode os.FileMode) os.FileMode {
	if mode != 0 && mode.Perm() != 0 {
		return mode.Perm() | 0o700
	}
	return 0o755
}

// StripExtension 去掉文件名最后一个扩展名。
func StripExtension(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// SamePath 判断两个路径是否指向同一位置。
func SamePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return absA == absB
}

// UniqueOutputPath 返回一个不与已有文件冲突的输出路径。
//
// 若目标不存在（或允许覆盖），直接返回原路径；否则在扩展名前追加
// ".1"、".2" 等序号，直到找到可用名称。
func UniqueOutputPath(path string, overwrite bool) string {
	if overwrite {
		return path
	}
	if _, err := os.Stat(path); err != nil {
		return path
	}

	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)

	for i := 1; i < 10000; i++ {
		candidate := fmt.Sprintf("%s.%d%s", base, i, ext)
		if _, err := os.Stat(candidate); err != nil {
			return candidate
		}
	}
	return path
}
