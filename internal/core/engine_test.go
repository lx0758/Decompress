package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// fakeCompression 是用于测试的假格式实现。
//
// 它实现完整的 Compression 接口，用于验证主控制器的行为，
// 而不依赖任何真实格式。
type fakeCompression struct {
	name string
	// extensions 是支持的扩展名（含点）。
	extensions []string
	// magic 是魔数识别规则。
	magic []Magic
	// produced 是解压产物的文件名（相对目标目录）。
	produced string
	// scanEntries 是预扫描返回的条目。
	scanEntries []Entry
	// extractFunc 可选，自定义解压行为。
	extractFunc func(ctx context.Context, req *ExtractRequest) (*ExtractResult, error)
}

// Name 返回格式标识。
func (f *fakeCompression) Name() string { return f.name }

// DisplayName 返回展示名。
func (f *fakeCompression) DisplayName() string { return f.name }

// Extensions 返回扩展名。
func (f *fakeCompression) Extensions() []string { return f.extensions }

// Magic 返回魔数规则。
func (f *fakeCompression) Magic() []Magic { return f.magic }

// Scan 返回预扫描的条目清单。
func (f *fakeCompression) Scan(path string) ([]Entry, error) {
	if f.scanEntries != nil {
		return f.scanEntries, nil
	}
	return []Entry{{Name: f.produced}}, nil
}

// Extract 执行解压。
func (f *fakeCompression) Extract(ctx context.Context, req *ExtractRequest) (*ExtractResult, error) {
	if f.extractFunc != nil {
		return f.extractFunc(ctx, req)
	}
	if err := os.MkdirAll(req.DstDir, 0o755); err != nil {
		return nil, err
	}
	out := filepath.Join(req.DstDir, f.produced)
	content := []byte("content of " + req.SrcPath)
	if err := os.WriteFile(out, content, 0o644); err != nil {
		return nil, err
	}
	return &ExtractResult{OutPaths: []string{out}, Written: int64(len(content))}, nil
}

// newFakeRegistry 构造一个注册了假 ZIP 格式的注册表。
func newFakeRegistry(produced string) *Registry {
	reg := NewRegistry()
	reg.MustRegister(&fakeCompression{
		name:       "zip",
		extensions: []string{".zip"},
		magic:      []Magic{{Offset: 0, Pattern: []byte("PK")}},
		produced:   produced,
	})
	return reg
}

// TestEngineProcessesArchiveFiles 验证引擎只处理可识别的压缩文件。
func TestEngineProcessesArchiveFiles(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.zip")
	if err := os.WriteFile(archive, []byte("PK\x03\x04"), 0o644); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(plain, []byte("just text"), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := NewEngine(newFakeRegistry("out.txt"))
	opts := DefaultOptions()
	opts.OutputDir = t.TempDir()

	summary, err := engine.Extract(context.Background(), []string{dir}, opts)
	if err != nil {
		t.Fatalf("解压失败：%v", err)
	}
	if summary.TotalFiles != 1 {
		t.Errorf("期望处理 1 个压缩文件，实际 %d", summary.TotalFiles)
	}
	if summary.Failed != 0 {
		t.Errorf("期望 0 个失败，实际 %d", summary.Failed)
	}
}

// TestEngineProgressEvents 验证进度事件被正确上报。
func TestEngineProgressEvents(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		p := filepath.Join(dir, string(rune('a'+i))+".zip")
		if err := os.WriteFile(p, []byte("PK\x03\x04"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var starts, files, completes int
	opts := DefaultOptions()
	opts.OutputDir = t.TempDir()
	opts.Progress = func(ev ProgressEvent) {
		switch ev.Type {
		case ProgressStart:
			starts++
		case ProgressFile:
			files++
		case ProgressComplete:
			completes++
		}
	}

	engine := NewEngine(newFakeRegistry("out.txt"))
	if _, err := engine.Extract(context.Background(), []string{dir}, opts); err != nil {
		t.Fatalf("解压失败：%v", err)
	}

	if starts != 1 {
		t.Errorf("期望 1 次开始事件，实际 %d", starts)
	}
	if files != 3 {
		t.Errorf("期望 3 次文件事件，实际 %d", files)
	}
	if completes != 1 {
		t.Errorf("期望 1 次完成事件，实际 %d", completes)
	}
}

// TestEngineUnknownFormatFiltered 验证无法识别的文件被跳过而不是报错。
func TestEngineUnknownFormatFiltered(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(plain, []byte("just text"), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := NewEngine(newFakeRegistry("out.txt"))
	opts := DefaultOptions()
	opts.OutputDir = t.TempDir()

	summary, err := engine.Extract(context.Background(), []string{dir}, opts)
	if err != nil {
		t.Fatalf("不应报错：%v", err)
	}
	if summary.TotalFiles != 0 {
		t.Errorf("普通文件应被过滤，实际处理了 %d 个", summary.TotalFiles)
	}
}

// TestEngineQueueProcessesNestedOutput 验证解压产物会重新入队处理。
//
// 这是队列模型的核心行为：解压出的文件若本身是压缩包，会被继续解开。
func TestEngineQueueProcessesNestedOutput(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "outer.zip")
	if err := os.WriteFile(archive, []byte("PK\x03\x04"), 0o644); err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	innerPath := filepath.Join(outDir, "inner.zip")

	// 假实现：外层解出 inner.zip（它也是压缩包），内层解出 payload.txt。
	registry := NewRegistry()
	registry.MustRegister(&fakeCompression{
		name:       "zip",
		extensions: []string{".zip"},
		magic:      []Magic{{Offset: 0, Pattern: []byte("PK")}},
		extractFunc: func(ctx context.Context, req *ExtractRequest) (*ExtractResult, error) {
			if req.SrcPath == innerPath {
				out := filepath.Join(req.DstDir, "payload.txt")
				if err := os.WriteFile(out, []byte("payload"), 0o644); err != nil {
					return nil, err
				}
				return &ExtractResult{OutPaths: []string{out}, Written: 7}, nil
			}
			// 外层：产出一个假的压缩包。
			if err := os.WriteFile(innerPath, []byte("PK\x03\x04"), 0o644); err != nil {
				return nil, err
			}
			return &ExtractResult{OutPaths: []string{innerPath}, Written: 4}, nil
		},
	})

	engine := NewEngine(registry)
	opts := DefaultOptions()
	opts.OutputDir = outDir

	summary, err := engine.Extract(context.Background(), []string{archive}, opts)
	if err != nil {
		t.Fatalf("解压失败：%v", err)
	}

	// 外层与内层都应被处理。
	if summary.TotalFiles != 2 {
		t.Errorf("期望处理 2 个压缩包（外层+内层），实际 %d", summary.TotalFiles)
	}

	// 最终产物应存在。
	if _, err := os.Stat(filepath.Join(outDir, "payload.txt")); err != nil {
		t.Errorf("嵌套解压产物不存在：%v", err)
	}
}

// TestEngineDryRun 验证试运行模式不落盘。
func TestEngineDryRun(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.zip")
	if err := os.WriteFile(archive, []byte("PK\x03\x04"), 0o644); err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	engine := NewEngine(newFakeRegistry("out.txt"))
	opts := DefaultOptions()
	opts.OutputDir = outDir
	opts.DryRun = true

	if _, err := engine.Extract(context.Background(), []string{archive}, opts); err != nil {
		t.Fatalf("解压失败：%v", err)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("试运行不应产出文件，实际有 %d 个", len(entries))
	}
}

// TestRegistryDetectMagic 验证魔数识别由注册表驱动。
func TestRegistryDetectMagic(t *testing.T) {
	reg := NewRegistry()
	reg.MustRegister(&fakeCompression{
		name:       "zip",
		extensions: []string{".zip"},
		magic:      []Magic{{Offset: 0, Pattern: []byte("PK\x03\x04")}},
	})

	dir := t.TempDir()
	archive := filepath.Join(dir, "a.zip")
	if err := os.WriteFile(archive, []byte("PK\x03\x04"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := reg.Detect(archive); got != "zip" {
		t.Errorf("魔数识别失败：期望 zip，实际 %q", got)
	}
}

// TestRegistryDetectByExtension 验证魔数失败时回退扩展名。
func TestRegistryDetectByExtension(t *testing.T) {
	reg := NewRegistry()
	reg.MustRegister(&fakeCompression{
		name:       "zip",
		extensions: []string{".zip"},
		magic:      []Magic{{Offset: 0, Pattern: []byte("PK\x03\x04")}},
	})

	dir := t.TempDir()
	// 无魔数，但扩展名匹配。
	archive := filepath.Join(dir, "a.zip")
	if err := os.WriteFile(archive, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := reg.Detect(archive); got != "zip" {
		t.Errorf("扩展名回退失败：期望 zip，实际 %q", got)
	}
}

// TestRegistryRejectDuplicate 验证重复注册会被拒绝。
func TestRegistryRejectDuplicate(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(&fakeCompression{name: "zip"}); err != nil {
		t.Fatalf("首次注册不应失败：%v", err)
	}
	if err := reg.Register(&fakeCompression{name: "zip"}); err == nil {
		t.Error("重复注册应报错")
	}
}
