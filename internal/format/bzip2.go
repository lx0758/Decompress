package format

import (
	"compress/bzip2"
	"context"
	"io"

	"github.com/liux/decompress/internal/core"
)

// Bzip2 是 BZIP2 单流压缩格式的实现。
//
// BZIP2 是"一个压缩流对应一个文件"的格式，内部没有条目清单概念，
// 解压后恒产出一个文件，输出布局恒为平铺。
type Bzip2 struct{}

// init 自动注册到内置注册表。
func init() { Register(Bzip2{}) }

// Name 返回格式标识。
func (Bzip2) Name() string { return "bzip2" }

// DisplayName 返回展示名。
func (Bzip2) DisplayName() string { return "BZIP2" }

// Extensions 返回支持的扩展名。
func (Bzip2) Extensions() []string { return []string{".bz2", ".tbz2", ".tbz"} }

// Magic 返回魔数规则。
func (Bzip2) Magic() []core.Magic {
	return []core.Magic{
		{Offset: 0, Pattern: []byte{'B', 'Z', 'h'}},
	}
}

// Scan 返回合成的单条目。
func (b Bzip2) Scan(path string) ([]core.Entry, error) {
	return planStreamEntry(path, ".bz2")
}

// Extract 解压 BZIP2 流。
func (b Bzip2) Extract(ctx context.Context, req *core.ExtractRequest) (*core.ExtractResult, error) {
	return extractStream(ctx, req, ".bz2", func(r io.Reader) (io.ReadCloser, error) {
		return io.NopCloser(bzip2.NewReader(r)), nil
	})
}
