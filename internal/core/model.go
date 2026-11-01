// Package core 定义解压工具的领域模型与统一契约。
//
// 本包是整个工具的"接口层"：它只描述"压缩格式应该具备什么能力"，
// 不关心任何具体格式如何实现。任何新增格式只要实现 Compression 接口
// 并注册即可被主控制器识别与调度，无需修改本包代码。
package core

import (
	"strings"
	"time"
)

// Format 是格式标识，由各格式实现自己声明。
//
// 使用字符串而非枚举：新增格式不需要修改本包的任何代码，
// 只要实现 Compression 接口并注册即可。
type Format = string

// FormatUnknown 表示无法识别的格式。
const FormatUnknown = ""

// FormatZip / FormatTar 等是内置格式的标识常量，供测试与文档引用。
//
// 这些常量不是"必须存在"的，任何格式实现都可以自行定义标识。
const (
	FormatZip   = "zip"
	FormatTar   = "tar"
	FormatGzip  = "gzip"
	FormatBzip2 = "bzip2"
	FormatXz    = "xz"
	FormatZstd  = "zstd"
	Format7z    = "7z"
	FormatRar   = "rar"
)

// DisplayNames 是内置格式的展示名，供 CLI 输出使用。
//
// 未登记的格式会回退到使用其标识本身作为展示名。
var DisplayNames = map[Format]string{
	FormatZip:   "ZIP",
	FormatTar:   "TAR",
	FormatGzip:  "GZIP",
	FormatBzip2: "BZIP2",
	FormatXz:    "XZ",
	FormatZstd:  "ZSTD",
	Format7z:    "7Z",
	FormatRar:   "RAR",
}

// DisplayName 返回格式的展示名。
func DisplayName(format Format) string {
	if name, ok := DisplayNames[format]; ok {
		return name
	}
	if format == FormatUnknown {
		return "UNKNOWN"
	}
	return strings.ToUpper(format)
}

// Options 是一次解压任务的配置。
type Options struct {
	// OutputDir 指定解压产物的根目录。为空时使用源文件所在目录。
	OutputDir string

	// Password 用于加密归档（当前用于 RAR / 7z）。
	Password string

	// Recursive 表示是否递归处理目录树。
	Recursive bool

	// Overwrite 表示目标文件已存在时是否覆盖。
	Overwrite bool

	// DryRun 表示只做格式识别与模拟，不真正落盘。
	DryRun bool

	// Progress 是进度回调，可为 nil。
	Progress ProgressFunc
}

// DefaultOptions 返回一组安全的默认选项。
func DefaultOptions() Options {
	return Options{
		Recursive: true,
		Overwrite: false,
	}
}

// ProgressEvent 描述一次进度更新。
type ProgressEvent struct {
	// Type 是事件类型。
	Type ProgressType

	// FilePath 是当前正在处理的文件路径。
	FilePath string

	// Percent 是总体完成百分比，取值 0-100。
	//
	// 仅在 ProgressComplete 事件上有效。进度中的文件事件不使用它：
	// 嵌套归档的总量在开始时不可知，百分比会随发现新文件而回退，
	// 反而误导用户，因此改用 TotalFiles / DoneFiles 展示计数。
	Percent int

	// TotalFiles 是本次任务涉及的文件总数。
	//
	// 对 ProgressComplete 而言是最终总数；对进度中的文件事件而言，
	// 是"此刻已知的总量"（已完成 + 当前 + 待处理），会随嵌套归档
	// 被逐层发现而增长。
	TotalFiles int

	// DoneFiles 是已处理完成的文件数。
	DoneFiles int

	// Message 是面向用户的提示信息。
	Message string

	// Err 非空表示该文件处理失败。
	Err error

	// Timestamp 是事件发生时刻。
	Timestamp time.Time
}

// ProgressType 区分进度事件的种类。
type ProgressType int

const (
	// ProgressStart 表示任务开始。
	ProgressStart ProgressType = iota
	// ProgressFile 表示开始处理某个文件。
	ProgressFile
	// ProgressUpdate 表示整体百分比更新。
	ProgressUpdate
	// ProgressComplete 表示任务完成。
	ProgressComplete
	// ProgressError 表示某个文件出错但任务继续。
	ProgressError
)

// ProgressFunc 是进度回调函数类型。
type ProgressFunc func(ProgressEvent)

// FileResult 描述单个文件的解压结果。
type FileResult struct {
	// SourcePath 是源文件路径。
	SourcePath string

	// Format 是识别出的格式，未知格式为 FormatUnknown。
	Format Format

	// OutputPaths 是解压产物的路径列表（文件或目录）。
	OutputPaths []string

	// EntryCount 是归档内的条目总数。
	EntryCount int

	// BytesWritten 是实际写出的字节数。
	BytesWritten int64

	// Duration 是处理耗时。
	Duration time.Duration

	// Err 非空表示解压失败。
	Err error
}

// Summary 描述一次任务的汇总结果。
type Summary struct {
	// Files 是每个文件的处理结果。
	Files []FileResult

	// TotalFiles 是处理的文件总数。
	TotalFiles int

	// Succeeded 是成功数。
	Succeeded int

	// Failed 是失败数。
	Failed int

	// TotalBytes 是写出的总字节数。
	TotalBytes int64

	// Duration 是任务总耗时。
	Duration time.Duration
}
