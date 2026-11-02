package format

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/liux/decompress/internal/core"
	"github.com/liux/decompress/internal/util"
	"github.com/nwaples/rardecode/v2"
)

// ErrPasswordRequired 表示归档已加密但未提供密码。
var ErrPasswordRequired = errors.New("归档已加密，需要提供密码")

// ErrWrongPassword 表示提供的密码不正确。
var ErrWrongPassword = errors.New("密码错误")

// Rar 是 RAR 归档格式的实现，支持 RAR 2.0 / 3.0 / 5.0。
//
// 采用纯 Go 实现（nwaples/rardecode），无需外部 unrar 可执行文件，
// 因此可以真正做到"不依赖 JRE、不依赖额外原生库"的单文件分发。
//
// 特性：
//   - 支持加密归档（通过 Options.Password 传入密码）
//   - 支持分卷归档（.part1.rar / .part01.rar），按文件名打开可自动衔接
//   - 支持 solid 归档与 Unicode 文件名
type Rar struct{}

// init 自动注册到内置注册表。
func init() { Register(Rar{}) }

// Name 返回格式标识。
func (Rar) Name() string { return "rar" }

// DisplayName 返回展示名。
func (Rar) DisplayName() string { return "RAR" }

// Extensions 返回支持的扩展名。
func (Rar) Extensions() []string { return []string{".rar"} }

// Magic 返回魔数规则。
//
// 覆盖 RAR 1.5-4.x 与 RAR 5.0 两种签名。
func (Rar) Magic() []core.Magic {
	return []core.Magic{
		{Offset: 0, Pattern: []byte{'R', 'a', 'r', '!', 0x1A, 0x07, 0x00}},
		{Offset: 0, Pattern: []byte{'R', 'a', 'r', '!', 0x1A, 0x07, 0x01, 0x00}},
	}
}

// IsStream 返回 false：RAR 是归档类格式，内部有条目清单。
func (Rar) IsStream() bool { return false }

// Scan 预扫描 RAR 的条目清单。
//
// rardecode.List 只解析文件头，不解压任何内容。
func (r Rar) Scan(path string) ([]core.Entry, error) {
	files, err := rardecode.List(path)
	if err != nil {
		return nil, translateRarError(err)
	}

	entries := make([]core.Entry, 0, len(files))
	for _, f := range files {
		entries = append(entries, core.Entry{
			Name:    f.Name,
			IsDir:   f.IsDir,
			Size:    f.UnPackedSize,
			Mode:    f.Mode(),
			ModTime: f.ModificationTime,
		})
	}
	return entries, nil
}

// Extract 解压 RAR 归档。
func (r Rar) Extract(ctx context.Context, req *core.ExtractRequest) (*core.ExtractResult, error) {
	var readerOptions []rardecode.Option
	if req.Options.Password != "" {
		readerOptions = append(readerOptions, rardecode.Password(req.Options.Password))
	}

	// 第一遍：扫描条目，用于确定输出布局。
	files, err := rardecode.List(req.SrcPath, readerOptions...)
	if err != nil {
		return nil, translateRarError(err)
	}

	meta := make([]core.Entry, 0, len(files))
	for _, f := range files {
		meta = append(meta, core.Entry{
			Name:    f.Name,
			IsDir:   f.IsDir,
			Size:    f.UnPackedSize,
			Mode:    f.Mode(),
			ModTime: f.ModificationTime,
		})
	}

	l := planLayout(req.SrcPath, req.DstDir, meta, req.Options.ForceArchiveDir)
	l.root = resolveLayoutRoot(l, req.Options.Overwrite)
	if l.root == "" {
		// 空归档，不产出。
		return &core.ExtractResult{}, nil
	}

	if err := util.EnsureDir(l.root); err != nil {
		return nil, err
	}

	// 第二遍：实际解压。
	// 必须按文件名打开，rardecode 依赖文件名才能定位后续分卷。
	rc, err := rardecode.OpenReader(req.SrcPath, readerOptions...)
	if err != nil {
		return nil, translateRarError(err)
	}
	defer rc.Close()

	var (
		outPaths []string
		total    int64
	)

	for {
		if ctx.Err() != nil {
			return &core.ExtractResult{OutPaths: outPaths, Written: total, Err: ctx.Err()}, nil
		}

		hdr, err := rc.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return &core.ExtractResult{OutPaths: outPaths, Written: total, Err: translateRarError(err)}, nil
		}

		entry := archiveEntry{
			Entry: core.Entry{
				Name:    hdr.Name,
				IsDir:   hdr.IsDir,
				Size:    hdr.UnPackedSize,
				Mode:    hdr.Mode(),
				ModTime: hdr.ModificationTime,
			},
		}

		if hdr.IsDir {
			_, _, werr := writeArchiveEntry(ctx, l.root, entry, req.Options.Overwrite)
			if werr != nil && isUnsafePath(werr) {
				return nil, werr
			}
			continue
		}

		// rardecode 是流式读取：条目内容必须在 Next() 之后立即消费。
		// rc 本身即当前条目的内容流（实现 io.Reader）。
		entry.Open = func() (io.ReadCloser, error) {
			return io.NopCloser(rc), nil
		}

		target, written, werr := writeArchiveEntry(ctx, l.root, entry, req.Options.Overwrite)
		total += written
		if werr != nil {
			// 路径穿越属于致命错误：中止整个解压。
			if isUnsafePath(werr) {
				return nil, werr
			}
			// 无密码时读取内容会报"需要密码"，密码错误时报"密码错误"。
			if isPasswordError(werr) {
				if isWrongPassword(werr) {
					return nil, ErrWrongPassword
				}
				return nil, ErrPasswordRequired
			}
			continue
		}
		outPaths = append(outPaths, target)
	}

	return &core.ExtractResult{OutPaths: outPaths, Written: total}, nil
}

// isPasswordError 判断错误是否为密码相关错误。
//
// rardecode 在读取加密条目内容时才报告密码错误，此时错误已被
// util.CopyWithContext 包装，因此这里同时匹配底层错误与文本。
func isPasswordError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, rardecode.ErrArchiveEncrypted) ||
		errors.Is(err, rardecode.ErrArchivedFileEncrypted) ||
		errors.Is(err, rardecode.ErrBadPassword) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "password required") ||
		strings.Contains(msg, "incorrect password")
}

// isWrongPassword 判断错误是否为"密码不正确"。
//
// 与"缺少密码"区分开：前者是用户提供了错误密码，后者是没有提供。
func isWrongPassword(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, rardecode.ErrBadPassword) {
		return true
	}
	return strings.Contains(err.Error(), "incorrect password")
}

// translateRarError 把 rardecode 的底层错误翻译成更友好的提示。
func translateRarError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, rardecode.ErrArchiveEncrypted),
		errors.Is(err, rardecode.ErrArchivedFileEncrypted):
		return ErrPasswordRequired
	case errors.Is(err, rardecode.ErrBadPassword):
		return ErrWrongPassword
	case errors.Is(err, rardecode.ErrMultiVolume):
		return errors.New("分卷归档需要完整的分卷文件")
	default:
		return err
	}
}
