// Package cli 实现命令行接口。
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/liux/decompress/internal/core"
	"github.com/liux/decompress/internal/format"
)

// Options 是 CLI 的解析结果。
type Options struct {
	// Paths 是待解压的输入路径。
	Paths []string

	// OutputDir 是输出根目录，为空表示与源文件同级。
	OutputDir string

	// Password 是加密归档的密码。
	Password string

	// Recursive 表示递归处理目录。
	Recursive bool

	// Overwrite 表示覆盖已存在的目标文件。
	Overwrite bool

	// DryRun 表示只识别不解压。
	DryRun bool

	// Quiet 表示不输出进度。
	Quiet bool

	// JSON 表示以 JSON 输出结果。
	JSON bool

	// List 表示只列出归档内容而不解压。
	List bool

	// NoWait 表示跳过交互式终端下的退出驻留。
	NoWait bool
}

// Run 执行命令行入口，返回进程退出码。
func Run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseFlags(args, stdout, stderr)
	if err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		if err == errVersionShown {
			return 0
		}
		fmt.Fprintf(stderr, "参数错误：%v\n", err)
		return 2
	}

	if opts.List {
		return runList(opts, stdout, stderr)
	}

	return runExtract(opts, stdout, stderr)
}

// parseFlags 解析命令行参数。
func parseFlags(args []string, stdout, stderr io.Writer) (*Options, error) {
	fs := flag.NewFlagSet("decompress", flag.ContinueOnError)
	fs.SetOutput(stderr)

	opts := &Options{}
	var (
		outputDir = fs.String("o", "", "输出根目录（默认与源文件同级）")
		password  = fs.String("p", "", "加密归档的密码")
		recursive = fs.Bool("r", true, "递归处理目录中的压缩文件")
		overwrite = fs.Bool("force", false, "覆盖已存在的目标文件")
		dryRun    = fs.Bool("dry-run", false, "只识别格式，不实际解压")
		quiet     = fs.Bool("quiet", false, "不输出进度信息")
		asJSON    = fs.Bool("json", false, "以 JSON 输出结果")
		list      = fs.Bool("list", false, "只列出归档内容")
		noWait    = fs.Bool("no-wait", false, "不等待，立即退出")
		showVer   = fs.Bool("version", false, "显示版本信息")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, `decompress —— 跨平台命令行解压工具（Go 实现，无需 JRE）

用法：
  decompress [选项] <文件或目录...>

支持格式：ZIP、TAR、GZIP、BZIP2、XZ、ZSTD、7Z、RAR(2.0/3.0/5.0)

选项：
`)
		fs.PrintDefaults()
		fmt.Fprintf(stderr, `
示例：
  decompress archive.zip
  decompress -o ./out -r ./downloads
  decompress -p secret locked.rar
  decompress --list archive.rar
`)
	}

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	if *showVer {
		fmt.Fprintf(stdout, "decompress %s (%s/%s)\n", Version, runtime.GOOS, runtime.GOARCH)
		return nil, errVersionShown
	}

	opts.Paths = fs.Args()
	opts.OutputDir = *outputDir
	opts.Password = *password
	opts.Recursive = *recursive
	opts.Overwrite = *overwrite
	opts.DryRun = *dryRun
	opts.Quiet = *quiet
	opts.JSON = *asJSON
	opts.List = *list
	opts.NoWait = *noWait

	if len(opts.Paths) == 0 {
		fs.Usage()
		return nil, fmt.Errorf("缺少输入路径")
	}

	return opts, nil
}

// errVersionShown 表示 --version 已输出，应以成功码退出。
var errVersionShown = fmt.Errorf("version shown")

// Version 是工具版本号。
//
// 编译时由 Makefile 通过 ldflags 注入：
//
//	-X github.com/liux/decompress/internal/cli.Version=$(VERSION)
//
// 变量路径必须与本包的实际导入路径一致，否则注入会静默失效。
// 未注入时（如 go run）显示 dev。
var Version = "dev"

// runExtract 执行解压流程。
func runExtract(opts *Options, stdout, stderr io.Writer) int {
	engine := core.NewEngine(format.Default())

	coreOpts := core.DefaultOptions()
	coreOpts.OutputDir = opts.OutputDir
	coreOpts.Password = opts.Password
	coreOpts.Recursive = opts.Recursive
	coreOpts.Overwrite = opts.Overwrite
	coreOpts.DryRun = opts.DryRun

	if !opts.Quiet && !opts.JSON {
		progress := newConsoleProgress(stdout)
		coreOpts.Progress = progress.onEvent
	}

	summary, err := engine.Extract(context.Background(), opts.Paths, coreOpts)
	if err != nil {
		fmt.Fprintf(stderr, "解压失败：%v\n", err)
		return 1
	}

	if opts.JSON {
		writeJSON(stdout, summary)
	} else {
		writeSummary(stdout, summary)
	}

	// 交互式终端下短暂驻留，避免窗口一闪而过看不到结果。
	// 重定向到文件或管道时立即返回，不拖慢脚本。
	// -no-wait 可显式跳过驻留。
	if !opts.NoWait && isTerminal(stdout) {
		fmt.Fprintf(stdout, "\n%d 秒后自动退出，按 Ctrl+C 立即退出...\n", int(exitDelay.Seconds()))
		time.Sleep(exitDelay)
	}

	if summary.Failed > 0 {
		return 1
	}
	return 0
}

// exitDelay 是交互式终端下的退出驻留时长。
const exitDelay = 3 * time.Second

// isTerminal 判断写入目标是否为交互式终端。
//
// 用 os.ModeCharDevice 判定即可跨平台完成，无需引入 x/term：
// stdout 被重定向到文件或管道时不是字符设备，此时无需驻留。
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// runList 列出归档内容。
func runList(opts *Options, stdout, stderr io.Writer) int {
	registry := format.Default()

	for _, p := range opts.Paths {
		// registry.Detect 内部已含魔数识别与扩展名回退。
		detected := registry.Detect(p)
		if detected == core.FormatUnknown {
			fmt.Fprintf(stderr, "%s: 无法识别的格式\n", p)
			continue
		}

		c := registry.Get(detected)
		if c == nil {
			fmt.Fprintf(stderr, "%s: 不支持的格式 %s\n", p, detected)
			continue
		}

		fmt.Fprintf(stdout, "%s  [%s]\n", filepath.Base(p), c.DisplayName())

		// Scan 只读归档头部，不解压任何内容。
		entries, err := c.Scan(p)
		if err != nil {
			fmt.Fprintf(stderr, "  读取失败：%v\n", err)
			continue
		}

		for _, e := range entries {
			kind := "-"
			if e.IsDir {
				kind = "d"
			}
			fmt.Fprintf(stdout, "  %s %10d  %s\n", kind, e.Size, e.Name)
		}
		fmt.Fprintf(stdout, "  共 %d 个条目\n", len(entries))
	}
	return 0
}

// writeSummary 输出人类可读的汇总。
//
// 只保留一句汇总，不再逐条列出产物：进度行已经显示过每个正在处理的
// 文件，明细表格与之重复，且嵌套日志包动辄上百行。
func writeSummary(w io.Writer, s *core.Summary) {
	fmt.Fprintf(w, "合计 %d 个文件：成功 %d，失败 %d，总计 %s，耗时 %s\n",
		s.TotalFiles, s.Succeeded, s.Failed,
		humanBytes(s.TotalBytes), s.Duration.Round(time.Millisecond))
}

// writeJSON 输出 JSON 结果。
func writeJSON(w io.Writer, s *core.Summary) {
	fmt.Fprintf(w, `{"total":%d,"succeeded":%d,"failed":%d,"bytes":%d,"durationMs":%d,"files":[`,
		s.TotalFiles, s.Succeeded, s.Failed, s.TotalBytes, s.Duration.Milliseconds())
	for i, r := range s.Files {
		if i > 0 {
			fmt.Fprint(w, ",")
		}
		errStr := ""
		if r.Err != nil {
			errStr = r.Err.Error()
		}
		fmt.Fprintf(w, `{"path":%q,"format":%q,"entries":%d,"bytes":%d,"durationMs":%d,"error":%q}`,
			r.SourcePath, core.DisplayName(r.Format), r.EntryCount, r.BytesWritten, r.Duration.Milliseconds(), errStr)
	}
	fmt.Fprint(w, "]}\n")
}

// humanBytes 把字节数格式化为易读形式。
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// consoleProgress 是一个简单的终端进度输出器。
type consoleProgress struct {
	w       io.Writer
	started time.Time
}

func newConsoleProgress(w io.Writer) *consoleProgress {
	return &consoleProgress{w: w, started: time.Now()}
}

// onEvent 是进度回调实现，输出到终端。
func (p *consoleProgress) onEvent(ev core.ProgressEvent) {
	switch ev.Type {
	case core.ProgressStart:
		fmt.Fprintf(p.w, "开始：%s\n", ev.Message)
	case core.ProgressFile:
		// 展示计数而非百分比：嵌套归档的总量在开始时不可知，
		// 分母会随发现新文件而增长，百分比会回退，反而误导。
		fmt.Fprintf(p.w, "[%3d/%-3d] %s\n", ev.DoneFiles+1, ev.TotalFiles, ev.Message)
	case core.ProgressError:
		fmt.Fprintf(p.w, "[错误] %s\n", ev.Message)
	}
}
