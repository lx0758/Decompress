package format

import (
	"archive/zip"
	"context"
	"io"
	"strings"

	"github.com/liux/decompress/internal/core"
	"github.com/liux/decompress/internal/util"
)

// Zip 是 ZIP 归档格式的实现。
type Zip struct{}

// init 自动注册到内置注册表。
func init() { Register(Zip{}) }

// Name 返回格式标识。
func (Zip) Name() string { return "zip" }

// DisplayName 返回展示名。
func (Zip) DisplayName() string { return "ZIP" }

// Extensions 返回支持的扩展名。
func (Zip) Extensions() []string { return []string{".zip"} }

// Magic 返回魔数规则。
//
// 覆盖本地文件头、中央目录结束记录、分卷标记三种起始签名。
func (Zip) Magic() []core.Magic {
	return []core.Magic{
		{Offset: 0, Pattern: []byte{'P', 'K', 0x03, 0x04}},
		{Offset: 0, Pattern: []byte{'P', 'K', 0x05, 0x06}},
		{Offset: 0, Pattern: []byte{'P', 'K', 0x07, 0x08}},
	}
}

// Scan 预扫描 ZIP 的条目清单。
//
// 只读中央目录，不解压任何内容。
func (z Zip) Scan(path string) ([]core.Entry, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	entries := make([]core.Entry, 0, len(reader.File))
	for _, f := range reader.File {
		name := decodeZipName(f)
		entries = append(entries, core.Entry{
			Name:    name,
			IsDir:   f.FileInfo().IsDir() || strings.HasSuffix(name, "/"),
			Size:    int64(f.UncompressedSize64),
			Mode:    f.Mode(),
			ModTime: f.Modified,
		})
	}
	return entries, nil
}

// Extract 解压 ZIP 归档。
func (z Zip) Extract(ctx context.Context, req *core.ExtractRequest) (*core.ExtractResult, error) {
	reader, err := zip.OpenReader(req.SrcPath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	entries := make([]archiveEntry, 0, len(reader.File))
	for _, f := range reader.File {
		file := f
		name := decodeZipName(file)
		entries = append(entries, archiveEntry{
			Entry: core.Entry{
				Name:    name,
				IsDir:   file.FileInfo().IsDir() || strings.HasSuffix(name, "/"),
				Size:    int64(file.UncompressedSize64),
				Mode:    file.Mode(),
				ModTime: file.Modified,
			},
			Open: func() (io.ReadCloser, error) { return file.Open() },
		})
	}

	l := planLayout(req.SrcPath, req.DstDir, metaOf(entries))
	l.root = resolveLayoutRoot(l, req.Options.Overwrite)

	outPaths, written, err := extractArchive(ctx, l, entries, req.Options.Overwrite)
	return &core.ExtractResult{OutPaths: outPaths, Written: written, Err: err}, nil
}

// decodeZipName 解析 ZIP 条目名。
//
// Go 标准库已按 UTF-8 标志位正确解码大部分条目名；对于未标记 UTF-8
// 但实际使用 GBK 编码的中文名，这里做一次回退处理。
func decodeZipName(f *zip.File) string {
	name := f.Name
	if f.NonUTF8 {
		if decoded, ok := util.DecodeGBK(name); ok {
			return decoded
		}
	}
	return name
}
