// Command decompress 是一个跨平台命令行解压工具。
//
// 完全使用 Go 标准库与纯 Go 第三方库实现，不依赖 JRE，也不依赖任何原生库。
package main

import (
	"os"

	"github.com/liux/decompress/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
