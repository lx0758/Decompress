package format

import (
	"context"
	"io"

	"github.com/liux/decompress/internal/core"
	"github.com/ulikunitz/xz"
)

// Xz 是 XZ 单流压缩格式的实现。
//
// XZ 是"一个压缩流对应一个文件"的格式，内部没有条目清单概念，
// 解压后恒产出一个文件，输出布局恒为平铺。
type Xz struct{}

// init 自动注册到内置注册表。
func init() { Register(Xz{}) }

// Name 返回格式标识。
func (Xz) Name() string { return "xz" }

// DisplayName 返回展示名。
func (Xz) DisplayName() string { return "XZ" }

// Extensions 返回支持的扩展名。
func (Xz) Extensions() []string { return []string{".xz", ".txz"} }

// Magic 返回魔数规则。
func (Xz) Magic() []core.Magic {
	return []core.Magic{
		{Offset: 0, Pattern: []byte{0xFD, '7', 'z', 'X', 'Z', 0x00}},
	}
}

// Scan 返回合成的单条目。
func (x Xz) Scan(path string) ([]core.Entry, error) {
	return planStreamEntry(path, ".xz")
}

// Extract 解压 XZ 流。
func (x Xz) Extract(ctx context.Context, req *core.ExtractRequest) (*core.ExtractResult, error) {
	return extractStream(ctx, req, ".xz", func(r io.Reader) (io.ReadCloser, error) {
		xr, err := xz.NewReader(r)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(xr), nil
	})
}
