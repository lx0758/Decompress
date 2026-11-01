package format

import (
	"context"
	"io"

	"github.com/klauspost/compress/zstd"
	"github.com/liux/decompress/internal/core"
)

// Zstd 是 Zstandard 单流压缩格式的实现。
//
// ZSTD 是"一个压缩流对应一个文件"的格式，内部没有条目清单概念，
// 解压后恒产出一个文件，输出布局恒为平铺。
type Zstd struct{}

// init 自动注册到内置注册表。
func init() { Register(Zstd{}) }

// Name 返回格式标识。
func (Zstd) Name() string { return "zstd" }

// DisplayName 返回展示名。
func (Zstd) DisplayName() string { return "ZSTD" }

// Extensions 返回支持的扩展名。
func (Zstd) Extensions() []string { return []string{".zst", ".tzst"} }

// Magic 返回魔数规则。
func (Zstd) Magic() []core.Magic {
	return []core.Magic{
		{Offset: 0, Pattern: []byte{0x28, 0xB5, 0x2F, 0xFD}},
	}
}

// Scan 返回合成的单条目。
func (z Zstd) Scan(path string) ([]core.Entry, error) {
	return planStreamEntry(path, ".zst")
}

// Extract 解压 Zstandard 流。
func (z Zstd) Extract(ctx context.Context, req *core.ExtractRequest) (*core.ExtractResult, error) {
	return extractStream(ctx, req, ".zst", func(r io.Reader) (io.ReadCloser, error) {
		d, err := zstd.NewReader(r)
		if err != nil {
			return nil, err
		}
		return d.IOReadCloser(), nil
	})
}
