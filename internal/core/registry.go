package core

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Registry 是压缩格式的注册表。
//
// 它是整个工具的"插件容器"：通过 Register 注入具体格式实现，
// 主控制器（Engine）只依赖本结构，不 import 任何具体格式。
//
// 注册表同时承担格式识别职责：它汇总所有已注册格式声明的魔数，
// 供 Detect 使用。
type Registry struct {
	mu sync.RWMutex

	// byName 按格式标识索引实现。
	byName map[string]Compression

	// names 保持注册顺序，便于稳定输出。
	names []string
}

// NewRegistry 创建空注册表。
func NewRegistry() *Registry {
	return &Registry{
		byName: make(map[string]Compression),
	}
}

// Register 注册一个格式实现。
//
// 标识必须非空且全局唯一；重复注册同一标识会返回错误，
// 以便尽早发现插件名冲突，而不是静默覆盖。
func (r *Registry) Register(c Compression) error {
	if c == nil {
		return fmt.Errorf("注册失败：格式实现不能为 nil")
	}

	name := strings.ToLower(strings.TrimSpace(c.Name()))
	if name == "" {
		return fmt.Errorf("注册失败：格式标识不能为空")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.byName[name]; exists {
		return fmt.Errorf("注册失败：格式 %q 已存在", name)
	}

	r.byName[name] = c
	r.names = append(r.names, name)
	return nil
}

// MustRegister 注册格式，失败时 panic。
//
// 用于程序初始化阶段的静态注册：注册表配置错误属于启动即应暴露的问题，
// 静默失败会导致后续"某格式莫名不支持"的排查成本极高。
func (r *Registry) MustRegister(c Compression) {
	if err := r.Register(c); err != nil {
		panic(err)
	}
}

// Get 按格式标识取出实现，不存在时返回 nil。
func (r *Registry) Get(name string) Compression {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byName[strings.ToLower(strings.TrimSpace(name))]
}

// All 返回所有已注册的格式实现，按注册顺序排列。
func (r *Registry) All() []Compression {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Compression, 0, len(r.names))
	for _, name := range r.names {
		out = append(out, r.byName[name])
	}
	return out
}

// Extensions 返回所有已注册格式支持的扩展名集合。
func (r *Registry) Extensions() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[string]string, len(r.byName))
	for _, c := range r.byName {
		for _, ext := range c.Extensions() {
			ext = strings.ToLower(strings.TrimSpace(ext))
			if ext != "" {
				out[ext] = c.Name()
			}
		}
	}
	return out
}

// detectByMagic 根据文件头字节匹配格式。
//
// 返回第一个匹配到的格式；匹配不上时返回空串。
//
// 匹配顺序即注册顺序，因此实现方应把特征更强、更不容易误判的
// 格式注册在前面。
func (r *Registry) detectByMagic(head []byte) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, name := range r.names {
		c := r.byName[name]
		for _, m := range c.Magic() {
			end := m.Offset + len(m.Pattern)
			if len(head) < end || len(m.Pattern) == 0 {
				continue
			}
			if bytesEqual(head[m.Offset:end], m.Pattern) {
				return c.Name()
			}
		}
	}
	return ""
}

// bytesEqual 判断两个字节切片是否相等。
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sortedNames 返回已注册格式的标识，按字典序排列，便于输出。
func (r *Registry) sortedNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]string, len(r.names))
	copy(out, r.names)
	sort.Strings(out)
	return out
}
