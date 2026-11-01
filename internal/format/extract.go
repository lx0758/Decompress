package format

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/liux/decompress/internal/core"
	"github.com/liux/decompress/internal/util"
)

// 本文件负责把解压内容写出到磁盘，是各格式共用的解压流程。
//
// 它分为两条路径：
//
//   - extractArchive：归档类格式（zip/tar/7z/rar），多个条目按布局落盘
//   - extractStream：单流类格式（gz/bz2/xz/zstd），恒产出单个文件
//
// 两条路径都只处理"内容如何安全落盘"，不关心具体格式如何读取内容：
// 后者由各格式实现提供 Open 函数或解压 reader。

// writeArchiveEntry 将一个归档条目写入磁盘。
//
// 完整执行 Zip Slip 防护：条目名先经 SanitizeEntryPath 归一化，
// 再用 IsWithinDir 做二次校验。
//
// 返回的 error 若为 *core.ErrUnsafePath，表示归档中存在恶意条目，
// 调用方必须中止整个解压而不是跳过该条目。
func writeArchiveEntry(ctx context.Context, dstDir string, entry archiveEntry, overwrite bool) (string, int64, error) {
	target, err := core.SanitizeEntryPath(dstDir, entry.Name)
	if err != nil {
		return "", 0, err
	}
	if !core.IsWithinDir(dstDir, target) {
		return "", 0, &core.ErrUnsafePath{
			EntryName: entry.Name,
			Reason:    "条目路径超出目标目录",
		}
	}

	if entry.IsDir {
		if err := os.MkdirAll(target, util.DirPerm(entry.Mode)); err != nil {
			return "", 0, err
		}
		return target, 0, nil
	}

	// 归一化后指向根目录的普通文件条目是非法的。
	if util.SamePath(target, dstDir) {
		return "", 0, &core.ErrUnsafePath{
			EntryName: entry.Name,
			Reason:    "文件条目指向目标目录本身",
		}
	}

	if err := util.EnsureDir(filepath.Dir(target)); err != nil {
		return "", 0, err
	}

	reader, err := entry.Open()
	if err != nil {
		return "", 0, err
	}
	defer reader.Close()

	out, err := util.CreateOutput(target, overwrite)
	if err != nil {
		return "", 0, err
	}

	written, copyErr := util.CopyWithContext(ctx, out, reader, 0)
	closeErr := out.Close()
	if copyErr != nil {
		os.Remove(target)
		return target, written, copyErr
	}
	if closeErr != nil {
		return target, written, closeErr
	}

	// 尽可能保留原始权限与时间戳。
	if entry.Mode != 0 {
		os.Chmod(target, entry.Mode.Perm())
	}
	if !entry.ModTime.IsZero() {
		os.Chtimes(target, entry.ModTime, entry.ModTime)
	}

	return target, written, nil
}

// extractArchive 是归档类格式的通用解压流程。
func extractArchive(ctx context.Context, l layout, entries []archiveEntry, overwrite bool) ([]string, int64, error) {
	if l.root == "" {
		// 空归档，不产出。
		return nil, 0, nil
	}

	if err := util.EnsureDir(l.root); err != nil {
		return nil, 0, err
	}

	var (
		outPaths []string
		total    int64
	)

	for _, entry := range entries {
		if ctx.Err() != nil {
			return outPaths, total, ctx.Err()
		}

		target, written, err := writeArchiveEntry(ctx, l.root, entry, overwrite)
		if err != nil {
			// 路径穿越属于致命错误：中止整个解压。
			if isUnsafePath(err) {
				return outPaths, total, err
			}
			// 其它单条目错误不中断整体解压。
			continue
		}

		total += written
		if !entry.IsDir {
			outPaths = append(outPaths, target)
		}
	}

	return outPaths, total, nil
}

// extractStream 是单流格式（gzip/bzip2/xz/zstd）的通用解压流程。
//
// 单流格式没有条目概念，解压后恒产出一个文件。suffix 是该格式的
// 压缩后缀，用于推导输出文件名；open 负责把原始文件包装成解压 reader。
func extractStream(ctx context.Context, req *core.ExtractRequest, suffix string, open func(io.Reader) (io.ReadCloser, error)) (*core.ExtractResult, error) {
	entries, err := planStreamEntry(req.SrcPath, suffix)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return &core.ExtractResult{}, nil
	}

	outPath := filepath.Join(req.DstDir, entries[0].Name)

	// 多个归档可能解出同名文件（例如 a.tar 与 a.tar.gz 都产出 a.tar）。
	// 此时自动追加序号，避免互相覆盖导致失败。
	outPath = util.UniqueOutputPath(outPath, req.Options.Overwrite)

	if err := util.EnsureDir(req.DstDir); err != nil {
		return nil, err
	}

	in, err := os.Open(req.SrcPath)
	if err != nil {
		return nil, err
	}
	defer in.Close()

	reader, err := open(in)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	out, err := util.CreateOutput(outPath, req.Options.Overwrite)
	if err != nil {
		return nil, err
	}

	written, copyErr := util.CopyWithContext(ctx, out, reader, 0)
	closeErr := out.Close()
	if copyErr != nil {
		os.Remove(outPath)
		return &core.ExtractResult{Written: written, Err: copyErr}, nil
	}
	if closeErr != nil {
		return &core.ExtractResult{Written: written, Err: closeErr}, nil
	}

	return &core.ExtractResult{OutPaths: []string{outPath}, Written: written}, nil
}

// isUnsafePath 判断错误是否为路径穿越类错误。
//
// 这类错误必须中止整个解压：恶意归档不应被"跳过坏条目"的方式部分处理，
// 否则用户会得到一个看似成功实则不完整的结果。
func isUnsafePath(err error) bool {
	var unsafe *core.ErrUnsafePath
	return errors.As(err, &unsafe)
}
