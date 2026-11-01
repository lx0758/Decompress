package format

import (
	"archive/tar"
	"context"
	"io"
	"os"

	"github.com/liux/decompress/internal/core"
)

// Tar 是 TAR 归档格式的实现。
type Tar struct{}

// init 自动注册到内置注册表。
func init() { Register(Tar{}) }

// Name 返回格式标识。
func (Tar) Name() string { return "tar" }

// DisplayName 返回展示名。
func (Tar) DisplayName() string { return "TAR" }

// Extensions 返回支持的扩展名。
func (Tar) Extensions() []string { return []string{".tar"} }

// Magic 返回魔数规则。
//
// POSIX 规范把 ustar 标记放在偏移 257 处。
func (Tar) Magic() []core.Magic {
	return []core.Magic{
		{Offset: 257, Pattern: []byte("ustar")},
	}
}

// Scan 预扫描 TAR 的条目清单。
//
// TAR 是顺序流格式，条目只能顺序读出。但 archive/tar 的 discard
// 支持 io.Seeker，在 os.File 上会直接 seek 跳过数据块，
// 因此不会真正读取文件内容。
func (t Tar) Scan(path string) ([]core.Entry, error) {
	entries, _, err := scanTar(path)
	return entries, err
}

// Extract 解压 TAR 归档。
func (t Tar) Extract(ctx context.Context, req *core.ExtractRequest) (*core.ExtractResult, error) {
	// 先扫描得到条目与数据偏移，用于布局决策与内容定位。
	entries, offsets, err := scanTar(req.SrcPath)
	if err != nil {
		return nil, err
	}

	l := planLayout(req.SrcPath, req.DstDir, entries)
	l.root = resolveLayoutRoot(l, req.Options.Overwrite)
	if l.root == "" {
		// 空归档，不产出。
		return &core.ExtractResult{}, nil
	}

	archiveEntries := make([]archiveEntry, 0, len(entries))
	for i, meta := range entries {
		entry := meta
		offset := offsets[i]

		archiveEntries = append(archiveEntries, archiveEntry{
			Entry: entry,
			Open: func() (io.ReadCloser, error) {
				if entry.IsDir {
					return nil, nil
				}
				return openTarSlice(req.SrcPath, offset, entry.Size)
			},
		})
	}

	outPaths, written, err := extractArchive(ctx, l, archiveEntries, req.Options.Overwrite)
	return &core.ExtractResult{OutPaths: outPaths, Written: written, Err: err}, nil
}

// scanTar 扫描 TAR 条目，同时记录每个条目的数据起始偏移。
//
// 返回的 offsets 与 entries 一一对应：偏移为该条目数据段的起始位置，
// 长度为条目大小。目录条目的偏移无意义，记为 0。
func scanTar(path string) ([]core.Entry, []int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	tr := tar.NewReader(f)

	var (
		entries []core.Entry
		offsets []int64
	)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}

		// tr.Next() 之后文件位置即数据段起点。
		pos, serr := f.Seek(0, io.SeekCurrent)
		if serr != nil {
			pos = 0
		}

		entries = append(entries, core.Entry{
			Name:    hdr.Name,
			IsDir:   hdr.Typeflag == tar.TypeDir,
			Size:    hdr.Size,
			Mode:    hdr.FileInfo().Mode(),
			ModTime: hdr.ModTime,
		})
		offsets = append(offsets, pos)
	}
	return entries, offsets, nil
}

// openTarSlice 打开 tar 文件中指定偏移处的定长数据段。
//
// 利用 tar 条目在文件中的物理布局直接定位，避免顺序流带来的耦合。
func openTarSlice(path string, offset, size int64) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	return &tarSliceReader{f: f, remaining: size}, nil
}

// tarSliceReader 读取 tar 文件中的一段定长数据。
type tarSliceReader struct {
	f         *os.File
	remaining int64
}

// Read 读取数据。
func (r *tarSliceReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.f.Read(p)
	r.remaining -= int64(n)
	return n, err
}

// Close 关闭底层文件。
func (r *tarSliceReader) Close() error {
	return r.f.Close()
}
