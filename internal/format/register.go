// Package format 提供各压缩格式的具体实现。
//
// 每个格式实现 core.Compression 接口，并通过 init() 自行注册到
// 内置注册表。主控制器（core.Engine）只依赖 core.Compression，
// 不 import 本包的任何具体类型。
//
// 新增一种格式的步骤：
//
//  1. 新建一个文件，定义实现 core.Compression 的类型
//  2. 在该文件中用 init() 调用 Register 注册
//  3. 完成 —— 不需要修改本包或 core 包的任何现有代码
//
// 这里是"插件"的集中地，但各格式之间互不感知。
package format

import (
	"github.com/liux/decompress/internal/core"
)

// defaultRegistry 是内置格式的注册表。
//
// 各格式通过 init() 自行注册到这里，形成"插件即插即用"的效果。
var defaultRegistry = core.NewRegistry()

// Register 向内置注册表注册一个格式实现。
//
// 由各格式的 init() 调用。
func Register(c core.Compression) {
	if err := defaultRegistry.Register(c); err != nil {
		panic(err)
	}
}

// Default 返回内置格式的注册表。
//
// 注册表在包初始化时就已完成装配，返回的是共享实例。
func Default() *core.Registry {
	return defaultRegistry
}

// NewRegistry 创建一个空的注册表，供测试或自定义插件集使用。
func NewRegistry() *core.Registry {
	return core.NewRegistry()
}
