// Package core 提供解压任务的调度能力。
//
// 调度模型是一个统一的工作队列：
//
//	输入 I
//	  ├─ 是目录 → 遍历其中的文件 → 逐个入队
//	  └─ 是文件
//	       ├─ 魔术头识别为非压缩文件 → 跳过
//	       └─ 识别为压缩包 → 解压 → 产出文件树 → 逐个入队
//
// 队列中的每个条目都会被独立检查，因此"解压出的文件里又套着压缩包"
// 这种嵌套场景会被自动递归处理，无需额外的递归逻辑。
package core

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Engine 是解压调度器。
//
// 它只依赖统一的注册表，不感知任何具体格式实现。
type Engine struct {
	registry *Registry

	// mu 保护 processed 与统计计数。
	mu sync.Mutex
	// processed 记录已处理过的路径，防止同一文件被重复处理。
	processed map[string]bool

	// totalFiles 是入队过的文件总数。
	totalFiles int
	// doneFiles 是已处理完成的文件数。
	doneFiles int
}

// NewEngine 创建解压引擎。
//
// registry 是格式插件的容器，引擎只依赖它，不感知任何具体格式。
func NewEngine(registry *Registry) *Engine {
	return &Engine{
		registry:  registry,
		processed: make(map[string]bool),
	}
}

// queueItem 是工作队列中的一个条目。
type queueItem struct {
	// path 是待处理的文件或目录路径。
	path string

	// derived 表示该路径来自解压产物，而非用户直接指定。
	derived bool
}

// Extract 执行一次解压任务。
//
// paths 可以是单个文件、单个目录，或它们的混合。返回汇总结果。
func (e *Engine) Extract(ctx context.Context, paths []string, opts Options) (*Summary, error) {
	start := time.Now()

	if len(paths) == 0 {
		return nil, fmt.Errorf("没有指定任何输入路径")
	}

	// 队列：先进先出。
	// 每项记录该路径是否由解压衍生（而非用户直接指定）。
	queue := make([]queueItem, 0, 16)
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		queue = append(queue, queueItem{path: abs})
	}

	dstRoot := opts.OutputDir
	if dstRoot == "" {
		// 未指定输出目录时，解压产物放在源文件所在目录。
		dstRoot = ""
	}

	e.emit(opts, ProgressEvent{
		Type:       ProgressStart,
		TotalFiles: 0,
		Message:    fmt.Sprintf("开始处理 %d 个输入路径", len(paths)),
		Timestamp:  time.Now(),
	})

	summary := &Summary{}
	var (
		mu      sync.Mutex
		results = make([]FileResult, 0, 16)
	)

	// 队列消费循环。
	for len(queue) > 0 {
		if ctx.Err() != nil {
			break
		}

		item := queue[0]
		queue = queue[1:]
		current := item.path

		info, err := os.Stat(current)
		if err != nil {
			continue
		}

		// 目录：遍历其中的文件，逐一入队。
		if info.IsDir() {
			files, err := e.listFiles(current, opts)
			if err != nil {
				continue
			}
			for _, f := range files {
				queue = append(queue, queueItem{path: f, derived: item.derived})
			}
			continue
		}

		// 文件：检查是否已处理过。
		if !e.markProcessed(current) {
			continue
		}

		e.mu.Lock()
		e.totalFiles++
		total := e.totalFiles
		e.mu.Unlock()

		// 超过处理上限时停止，防止压缩炸弹无限展开。
		if total > maxArchives {
			return nil, fmt.Errorf("处理的压缩包数量超过上限 %d，已中止", maxArchives)
		}

		// 识别格式。非压缩文件直接跳过。
		// 注：registry.Detect 内部已包含魔数识别与扩展名回退。
		format := e.registry.Detect(current)
		if format == FormatUnknown {
			continue
		}

		compression := e.registry.Get(format)
		if compression == nil {
			continue
		}

		// 目标目录的确定：
		//
		//   - 用户直接指定的归档 → 解到输出根目录（未指定则与源文件同级）
		//   - 由解压衍生的子归档 → 解到它自己的父目录，保持目录树结构
		//
		// 后者很重要：若把子归档也解到输出根目录，
		// 其内容就会脱离原有的层级，产出散乱。
		dstDir := dstRoot
		if item.derived {
			dstDir = filepath.Dir(current)
		} else if dstDir == "" {
			dstDir = filepath.Dir(current)
		}

		// 进度分母取"已知总量"：已完成 + 当前 + 队列中待处理的。
		//
		// 归档内容要解压后才能知道，嵌套压缩包会随着逐层展开不断被发现，
		// 因此总量在开始阶段无法获知，只能随发现持续增长。
		// 这里给出的是此刻能诚实表达的上界，而不是最终总数。
		knownTotal := e.doneFiles + 1 + len(queue)

		e.emit(opts, ProgressEvent{
			Type:       ProgressFile,
			FilePath:   current,
			TotalFiles: knownTotal,
			DoneFiles:  e.doneFiles,
			Message:    fmt.Sprintf("正在解压：%s", filepath.Base(current)),
			Timestamp:  time.Now(),
		})

		// 执行解压。
		result := e.extractOne(ctx, compression, current, dstDir, format, opts)

		mu.Lock()
		results = append(results, result)
		mu.Unlock()

		e.mu.Lock()
		e.doneFiles++
		e.mu.Unlock()

		// 解压产物入队，继续处理（产物中可能还有压缩包）。
		if result.Err == nil {
			for _, p := range result.OutputPaths {
				queue = append(queue, queueItem{path: p, derived: true})
			}
			// 衍生出来的子压缩包解完后立即清理，避免产物里残留中间态压缩包。
			//
			// 用户直接指定的源文件永远保留：删掉它等于破坏用户数据；
			// 而子压缩包的内容已经落盘，且随时可以从父包重新解出，丢弃不损失信息。
			// 解压失败时不清理，便于排查问题。
			if item.derived {
				os.Remove(current)
			}
		}
	}

	// 汇总结果。
	for _, r := range results {
		summary.Files = append(summary.Files, r)
		summary.TotalFiles++
		if r.Err != nil {
			summary.Failed++
		} else {
			summary.Succeeded++
		}
		summary.TotalBytes += r.BytesWritten
	}
	summary.Duration = time.Since(start)

	e.emit(opts, ProgressEvent{
		Type:       ProgressComplete,
		Percent:    100,
		TotalFiles: summary.TotalFiles,
		DoneFiles:  summary.Succeeded,
		Message: fmt.Sprintf("完成：成功 %d，失败 %d，耗时 %s",
			summary.Succeeded, summary.Failed, summary.Duration.Round(time.Millisecond)),
		Timestamp: time.Now(),
	})

	return summary, nil
}

// extractOne 执行单个压缩包的解压。
func (e *Engine) extractOne(ctx context.Context, c Compression, src, dstDir string, format Format, opts Options) FileResult {
	start := time.Now()
	result := FileResult{SourcePath: src, Format: format}

	// 试运行模式：只识别格式，不做任何解压。
	if opts.DryRun {
		result.Duration = time.Since(start)
		return result
	}

	// 预扫描条目清单：用于决定输出布局，不解压任何内容。
	entries, err := c.Scan(src)
	if err != nil {
		result.Err = err
		result.Duration = time.Since(start)
		return result
	}
	result.EntryCount = countFileEntries(entries)

	// 执行解压。
	res, err := c.Extract(ctx, &ExtractRequest{
		Compression: c,
		SrcPath:     src,
		DstDir:      dstDir,
		Options:     opts,
	})
	if err != nil {
		result.Err = err
		result.Duration = time.Since(start)
		return result
	}
	if res == nil {
		result.Duration = time.Since(start)
		return result
	}
	if res.Err != nil {
		result.Err = res.Err
		result.Duration = time.Since(start)
		return result
	}

	result.OutputPaths = res.OutPaths
	result.BytesWritten = res.Written
	result.Duration = time.Since(start)
	return result
}

// listFiles 列出目录下的文件（可选递归）。
func (e *Engine) listFiles(dir string, opts Options) ([]string, error) {
	var files []string

	if !opts.Recursive {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			files = append(files, filepath.Join(dir, e.Name()))
		}
		sort.Strings(files)
		return files, nil
	}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// markProcessed 标记路径为已处理，返回是否为首次处理。
func (e *Engine) markProcessed(path string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.processed[path] {
		return false
	}
	e.processed[path] = true
	return true
}

// countFileEntries 统计条目清单中的文件条目数（不含目录）。
func countFileEntries(entries []Entry) int {
	n := 0
	for _, e := range entries {
		if !e.IsDir {
			n++
		}
	}
	return n
}

// emit 安全地触发进度回调。
func (e *Engine) emit(opts Options, event ProgressEvent) {
	if opts.Progress == nil {
		return
	}
	defer func() {
		// 回调中的 panic 不应影响主流程。
		_ = recover()
	}()
	opts.Progress(event)
}

// maxArchives 限制单个任务中最多处理的压缩包数量。
//
// 队列模型下，压缩包会不断产出新的待处理文件。
// 构造的"压缩炸弹"（包内还是包）若无限制会无限展开，
// 因此设置一个总数量上限。
const maxArchives = 100000

// IsSupportedSuffix 判断路径是否以受支持的压缩后缀结尾。
//
// 判断依据来自注册表中各格式声明的扩展名，新增格式无需修改本函数。
func (r *Registry) IsSupportedSuffix(path string) bool {
	lower := strings.ToLower(path)
	for ext := range r.Extensions() {
		if ext != "" && strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}
