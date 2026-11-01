package format

import (
	"context"
	"io"
	"os"

	"github.com/bodgit/sevenzip"
	"github.com/liux/decompress/internal/core"
)

// SevenZ 是 7-Zip 归档格式的实现。
type SevenZ struct{}

// init 自动注册到内置注册表。
func init() { Register(SevenZ{}) }

// Name 返回格式标识。
func (SevenZ) Name() string { return "7z" }

// DisplayName 返回展示名。
func (SevenZ) DisplayName() string { return "7Z" }

// Extensions 返回支持的扩展名。
func (SevenZ) Extensions() []string { return []string{".7z"} }

// Magic 返回魔数规则。
func (SevenZ) Magic() []core.Magic {
	return []core.Magic{
		{Offset: 0, Pattern: []byte{'7', 'z', 0xBC, 0xAF, 0x27, 0x1C}},
	}
}

// openReader 打开 7z 读取器。
func (s SevenZ) openReader(path, password string) (*sevenzip.Reader, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}

	var reader *sevenzip.Reader
	if password != "" {
		reader, err = sevenzip.NewReaderWithPassword(f, info.Size(), password)
	} else {
		reader, err = sevenzip.NewReader(f, info.Size())
	}
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return reader, func() { f.Close() }, nil
}

// Scan 预扫描 7z 的条目清单。
//
// 只读 7z 头部信息表，不解压任何内容。
func (s SevenZ) Scan(path string) ([]core.Entry, error) {
	reader, closer, err := s.openReader(path, "")
	if err != nil {
		return nil, err
	}
	defer closer()

	entries := make([]core.Entry, 0, len(reader.File))
	for _, sf := range reader.File {
		entries = append(entries, core.Entry{
			Name:    sf.Name,
			IsDir:   sf.FileInfo().IsDir(),
			Size:    int64(sf.UncompressedSize),
			Mode:    sf.Mode(),
			ModTime: sf.Modified,
		})
	}
	return entries, nil
}

// Extract 解压 7z 归档。
func (s SevenZ) Extract(ctx context.Context, req *core.ExtractRequest) (*core.ExtractResult, error) {
	reader, closer, err := s.openReader(req.SrcPath, req.Options.Password)
	if err != nil {
		return nil, err
	}
	defer closer()

	entries := make([]archiveEntry, 0, len(reader.File))
	for _, sf := range reader.File {
		file := sf
		entry := archiveEntry{
			Entry: core.Entry{
				Name:    file.Name,
				IsDir:   file.FileInfo().IsDir(),
				Size:    int64(file.UncompressedSize),
				Mode:    file.Mode(),
				ModTime: file.Modified,
			},
		}
		if !entry.IsDir {
			entry.Open = func() (io.ReadCloser, error) { return file.Open() }
		}
		entries = append(entries, entry)
	}

	l := planLayout(req.SrcPath, req.DstDir, metaOf(entries))
	l.root = resolveLayoutRoot(l, req.Options.Overwrite)

	outPaths, written, err := extractArchive(ctx, l, entries, req.Options.Overwrite)
	return &core.ExtractResult{OutPaths: outPaths, Written: written, Err: err}, nil
}
