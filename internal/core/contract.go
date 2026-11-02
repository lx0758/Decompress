// Package core 定义解压工具的领域模型与统一契约。
//
// 本包是整个工具的"接口层"：它只描述"压缩格式应该具备什么能力"，
// 不关心任何具体格式如何实现。任何新增格式只要实现 Compression 接口
// 并注册即可被主控制器识别与调度，无需修改本包代码。
package core

import (
	"context"
	"os"
	"time"
)

// Magic 描述一条魔数识别规则。
//
// 魔数是文件头部的固定字节序列，用于在不看扩展名的情况下判断格式。
type Magic struct {
	// Offset 是魔数在文件中的起始偏移。
	Offset int

	// Pattern 是期望的字节序列。
	Pattern []byte
}

// Compression 是压缩格式的统一契约。
//
// 实现方需要同时提供"如何识别自己"与"如何解压"两方面能力。
// 调度引擎只依赖本接口，不感知任何具体格式。
//
// 实现约定：
//   - Name 返回的标识必须全局唯一且全小写，如 "zip"
//   - Extensions 返回小写扩展名（含点），如 ".zip"
//   - Magic 为空表示该格式不支持基于魔数的内容识别
//   - Scan 与 Extract 都必须能在不解压内容的前提下快速返回条目清单
//   - IsStream 必须准确反映该格式是单流还是归档类
type Compression interface {
	// Name 返回格式标识，全局唯一，全小写。
	Name() string

	// DisplayName 返回格式的展示名，如 "ZIP"、"RAR"。
	DisplayName() string

	// Extensions 返回支持的扩展名（含点，全小写）。
	Extensions() []string

	// Magic 返回魔数识别规则，可返回多条（同格式可能有多种版本）。
	Magic() []Magic

	// IsStream 返回该格式是否为"单流格式"。
	//
	// 单流格式内部没有条目清单概念，解压后恒产出一个文件。
	// 调度引擎据此区分两类产物：
	//
	//   - 单流格式的产物是"同一条解压链的下一环"（如 .tar.gz 解出 .tar）
	//   - 归档类格式的产物是"新的一层嵌套"（如 .zip 解出内部的子包）
	//
	// 前者会延续顶层目录命名，后者则按嵌套规则布局。
	IsStream() bool

	// Scan 预扫描归档的条目清单，不解压任何内容。
	//
	// 该方法的返回结果用于智能输出的布局决策，必须比 Extract 便宜得多。
	// 单流格式（gzip 等）没有条目概念时，返回一个合成条目即可。
	Scan(path string) ([]Entry, error)

	// Extract 执行解压，返回产物的文件路径列表。
	Extract(ctx context.Context, req *ExtractRequest) (*ExtractResult, error)
}

// Entry 是归档中的一个条目（尚未解压）。
type Entry struct {
	// Name 是条目在归档内的路径，使用 "/" 分隔。
	Name string

	// IsDir 表示该条目是否为目录。
	IsDir bool

	// Size 是解压后的大小，目录为 0。
	Size int64

	// Mode 是文件权限位。
	Mode os.FileMode

	// ModTime 是修改时间。
	ModTime time.Time
}

// ExtractRequest 描述一次解压请求。
//
// 使用值对象而非多个裸参数，便于后续扩展（如新增并发控制、
// 条目过滤等）而无需改动接口签名。
type ExtractRequest struct {
	// Compression 是被识别出的格式实现，由调用方从注册表取出后传入。
	Compression Compression

	// SrcPath 是待解压的归档文件路径。
	SrcPath string

	// DstDir 是解压产物的根目录。
	DstDir string

	// Options 是解压选项。
	Options Options
}

// ExtractResult 描述一次解压的结果。
type ExtractResult struct {
	// OutPaths 是解压产物的文件路径列表（仅文件，不含目录）。
	OutPaths []string

	// Written 是实际写出的字节数。
	Written int64

	// Err 非空表示解压失败。
	Err error
}
