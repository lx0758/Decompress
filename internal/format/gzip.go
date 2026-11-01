package format

import (
	"compress/gzip"
	"context"
	"io"

	"github.com/liux/decompress/internal/core"
)

// Gzip 是 GZIP 单流压缩格式的实现。
//
// GZIP 是"一个压缩流对应一个文件"的格式，内部没有条目清单概念，
// 解压后恒产出一个文件，输出布局恒为平铺。
type Gzip struct{}

// init 自动注册到内置注册表。
func init() { Register(Gzip{}) }

// Name 返回格式标识。
func (Gzip) Name() string { return "gzip" }

// DisplayName 返回展示名。
func (Gzip) DisplayName() string { return "GZIP" }

// Extensions 返回支持的扩展名。
func (Gzip) Extensions() []string { return []string{".gz", ".tgz"} }

// Magic 返回魔数规则。
func (Gzip) Magic() []core.Magic {
	return []core.Magic{
		{Offset: 0, Pattern: []byte{0x1F, 0x8B}},
	}
}

// Scan 返回合成的单条目。
func (g Gzip) Scan(path string) ([]core.Entry, error) {
	return planStreamEntry(path, ".gz")
}

// Extract 解压 GZIP 流。
func (g Gzip) Extract(ctx context.Context, req *core.ExtractRequest) (*core.ExtractResult, error) {
	return extractStream(ctx, req, ".gz", func(r io.Reader) (io.ReadCloser, error) {
		return gzip.NewReader(r)
	})
}
