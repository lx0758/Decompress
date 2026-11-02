# 解压工具（Go 实现）

用 Go 重写的跨平台命令行解压工具，功能对标原版 `Decompression.exe`（Java Swing 版），
**不依赖 JRE，也不依赖任何原生动态库** —— 单个静态二进制，拷走即用。

## 特性

| 项目 | 说明 |
|---|---|
| 支持格式 | ZIP、TAR、GZIP、BZIP2、XZ、ZSTD、7Z、RAR（2.0/3.0/5.0） |
| 依赖 | 纯 Go，`CGO_ENABLED=0` 静态编译，跨平台零外部依赖 |
| RAR 支持 | 纯 Go 解码，支持加密、分卷、solid、Unicode 文件名 |
| 识别方式 | **魔数嗅探**，改扩展名的文件照样能识别 |
| 输出布局 | 顶层解压链上的归档建以归档名命名的目录；单流格式直接输出文件，不建目录 |
| 嵌套解压 | 解压产物自动重新入队，子压缩包被逐层解开 |
| 安全 | 修复了原版的 Zip Slip 路径穿越漏洞 |

## 快速开始

```bash
# 编译
make build          # 产物在 bin/decompress
make release        # 交叉编译各平台，产物在 dist/
make winres         # 仅生成 Windows 版本资源（.syso）

# 使用
./bin/decompress archive.zip
./bin/decompress -o ./output -r ./downloads
./bin/decompress -p secret locked.rar
./bin/decompress --list archive.7z
./bin/decompress --json -o ./out ./data | jq .
```

### 支持的平台

`make release` 交叉编译以下 8 个目标，均为 `CGO_ENABLED=0` 静态二进制，
无需任何运行时依赖：

| 操作系统 | 架构 | 产物 |
|---------|------|------|
| Windows | amd64 | `decompress-<版本>-windows-amd64.exe` |
| Windows | arm64 | `decompress-<版本>-windows-arm64.exe` |
| Windows | 386 | `decompress-<版本>-windows-386.exe` |
| macOS | amd64（Intel） | `decompress-<版本>-darwin-amd64` |
| macOS | arm64（Apple Silicon） | `decompress-<版本>-darwin-arm64` |
| Linux | amd64 | `decompress-<版本>-linux-amd64` |
| Linux | arm64 | `decompress-<版本>-linux-arm64` |
| Linux | 386 | `decompress-<版本>-linux-386` |

路径处理统一走 Go 标准库 `path/filepath`，不硬编码分隔符，因此各平台行为一致。

### 版本号

版本号默认取最近的 git tag，并同时用于**产物文件名**和**二进制内的 `--version` 输出**：

```bash
make version              # 查看当前解析出的版本号
make release              # 版本号取自 git describe
make release VERSION=1.2.3   # 手动指定，优先于 git tag
```

解析规则与 `git describe --tags --always` 一致：

| 情况 | 版本号 | 产物示例 |
|---|---|---|
| 有 tag（如 `v1.0.0`） | `v1.0.0` | `decompress-v1.0.0-linux-amd64` |
| 无 tag | 短 commit hash | `decompress-927b27b-linux-amd64` |
| 不在 git 仓库中 | `dev` | `decompress-dev-linux-amd64` |

版本号通过 ldflags 注入到 `internal/cli.Version`，注入路径必须与包的实际
导入路径一致，写错会被静默忽略（`--version` 仍显示 `dev`）。

同一个版本号也会写进 Windows 版本资源（`--version` 与文件属性里的
`FileVersion` 一致），因此 `make release` 会先生成 `.syso` 再交叉编译。

### 持续集成

`.github/workflows/build-and-release.yml` 在推送 `v*` tag 时触发：
交叉编译 8 个平台 → 上传 artifact → 创建 GitHub Release 并附上全部产物。
构建用的版本号直接来自被 checkout 的 tag，无需额外配置。

## 工作流程

```mermaid
flowchart TD
  IN[输入路径] --> Q{工作队列}
  Q -->|取出 I| T{I 的类型}
  T -->|目录| TR[遍历其中的文件]
  TR -->|逐个| Q
  T -->|文件| DET[魔数识别格式]
  DET -->|非压缩文件| SKIP[跳过]
  DET -->|压缩包| SCAN[预扫描条目清单<br/>不解压任何内容]
  SCAN --> TOP{是顶层输入?}
  TOP -->|是 归档类| FDIR[强制建以归档名命名的目录]
  TOP -->|是 单流类| FLAT2[直接输出文件 不建目录]
  TOP -->|否 嵌套子包| PLAN{顶层条目数?}
  PLAN -->|0 个| NONE[不产出]
  PLAN -->|1 个| FLAT[平铺到所在目录]
  PLAN -->|多个| DIR[建以归档名命名的目录]
  FDIR --> EX[解压]
  FLAT2 --> EX
  FLAT --> EX
  DIR --> EX
  EX --> TREE[产出文件树 仅文件]
  TREE -->|逐个| Q
```

核心是**单一工作队列**：解压产出的文件会被重新放回队列，
因此"包里套包"的场景会被自动逐层解开，无需额外的递归逻辑。

## 输出布局

### 顶层解压链：一律建目录

**用户直接指定的输入**若最终展开成归档类格式（ZIP / TAR / 7Z / RAR），
无论内部有几个条目，产物一律落在以归档名命名的目录里：

| 输入 | 输出 |
|---|---|
| `onekeylog.zip` | `out/onekeylog/…` |
| `log_20261102_102646.tar.gz` | `out/log_20261102_102646/…` |
| `data`（无后缀） | `out/data.d/…` |

目录名取**该层自己的文件名**去掉最后一个扩展名；文件本身没有扩展名时
追加 `.d`，避免产出的目录与源文件同名。

对于 `log_20261102_102646.tar.gz` 这类"压缩套压缩"的输入，名字要按层剥离：
gzip 层产出 `log_20261102_102646.tar`，tar 层再产出目录
`log_20261102_102646/`。**每一层各去掉一个扩展名**，中间的压缩后缀
不会残留到最终的目录名上——否则会出现 `log_20261102_102646.tar/` 这种
多一层无意义目录的情况。复合后缀（`.tgz` / `.tbz2` / `.txz` / `.tzst`）
同样按内层还原：`.tgz` → `.tar`，因此 `log.tgz` → `log/`。

这么做的原因：日志压缩包的文件名里通常带着机器名、时间戳等信息
（`main_log_dev01_2026_0327_101238.zst`）。按条目数决定布局时，
单条目归档会被平铺到输出根目录，这些信息就丢了；固定成目录后，
每个日志包在自己的目录里，信息完整保留。

### 单流格式：直接输出文件，不建目录

GZIP / BZIP2 / XZ / ZSTD 属于**单流格式**：内部没有条目概念，
解压后恒产出一个文件。因此它们在任何情况下都不建目录——
顶层输入直接输出到输出根目录，嵌套子包直接落在它所在的目录里。

| 输入 | 输出 |
|---|---|
| `main_log_2026_0327.zst`（顶层） | `out/main_log_2026_0327` |
| `data`（顶层，无后缀） | `out/data.d` |
| `inner.gz`（嵌套在 `outer/` 内） | `out/outer/inner` |

输出文件名取原名去掉压缩后缀；原名没有对应后缀时追加 `.d`
（与原版 Java 工具行为一致）。

### 嵌套归档：按条目数决定

由解压衍生出来的**子压缩包**维持原有规则，按顶层条目数决定布局：

| 归档内容 | 输出布局 | 示例 |
|---|---|---|
| 顶层 0 个条目 | 不产出 | — |
| 顶层 1 个条目 | 平铺到所在目录 | `child.zip` → `out/child/a.txt` |
| 顶层多个条目 | 建以归档名命名的目录 | `child.zip` → `out/child/f1.txt` |

顶层条目按路径第一段去重：`a/b.txt` 与 `a/c.txt` 都算作顶层条目 `a`。

### 嵌套压缩包的清理

解压产出的文件会被重新放回队列，因此"包里套包"会被逐层解开。
其中**衍生出来的子压缩包在解完后立即删除**，产物里不会残留中间态压缩包：

```
outer.zip ──解压──▶ outer/child.zip ──解压──▶ outer/data.txt
                     ↑ 解完即删
```

用户直接指定的源压缩包永远保留——删掉它等于破坏用户数据。
子压缩包则不同：它的内容已经落盘，且随时可以从父包重新解出，丢弃不损失信息。

解压失败时不做清理，便于排查问题；`-dry-run` 模式下没有任何文件被删除。

### 预扫描的性能前提

嵌套归档的布局决策需要在解压前就知道归档内容。各格式的支持情况：

| 格式 | 列举接口 | 是否需读内容 | 2001 条目实测 |
|---|---|---|---|
| ZIP | 中央目录，`zip.Reader.File` | 否 | **2.7ms** |
| TAR | 顺序读头部 + `io.Seeker` 跳数据 | 否 | 49.6ms |
| 7Z | 头部信息表，`sevenzip.Reader.File` | 否 | 16.7ms |
| RAR | `rardecode.List()` | 否 | 0.3ms |
| GZ/BZ2/XZ/ZSTD | 单流格式，无条目概念 | — | — |

结论：**全部格式都支持不解压内容即列出条目**，因此布局决策不引入额外解压开销。

## 命令行用法

```
用法：
  decompress [选项] <文件或目录...>

选项：
  -o string       输出根目录（默认与源文件同级）
  -p string       加密归档的密码
  -r              递归处理目录中的压缩文件（默认 true）
  -force          覆盖已存在的目标文件
  -dry-run        只识别格式，不实际解压
  -quiet          不输出进度信息
  -json           以 JSON 输出结果
  -list           只列出归档内容
  -no-wait        不等待，立即退出
  -version        显示版本信息
```

退出码：`0` 成功，`1` 存在失败文件，`2` 参数错误。

### 输出

进度按 `[当前/已知总量]` 计数展示，末尾只留一句汇总，不再逐条列出产物
（进度行已显示每个正在处理的文件，明细表格与之重复）：

```
开始：开始处理 1 个输入路径
[  1/1  ] 正在解压：onekeylog.zip
[  2/12 ] 正在解压：eSight.zip
[ 12/20 ] 正在解压：main_log_2026_0326_11.zst
合计 12 个文件：成功 12，失败 0，总计 140.6 KB，耗时 25ms
```

分母是**此刻已知的总量**（已完成 + 当前 + 待处理），会随嵌套归档被逐层
发现而增长——开始阶段无法预知最终总数，因此这里不显示百分比。

输出到交互式终端时，末尾会驻留 3 秒再退出，避免窗口一闪而过；
重定向到文件或管道时立即退出，不拖慢脚本。
加 `-no-wait` 可显式跳过驻留。

## Windows 杀毒软件误报

Go 编译的 Windows 可执行文件被 Microsoft Defender 误报为病毒，是 Go 生态的
[普遍现象](https://github.com/microsoft/go/issues/1255)（微软 Go 团队在 issue 中
确认已知晓该问题）。本工具的产物曾被检出为 `Trojan:Win32/Sabsik.FL.A!ml`
——`!ml` 表示该判定由机器学习模型给出，而非命中了病毒特征库。

### 为什么会被误报

Defender 对**未签名的新二进制**缺少信誉依据，只能退回到启发式判定。
而 Go 二进制恰好具备若干容易触发启发式规则的固有特征：

| 特征 | 说明 |
|---|---|
| `CreateProcessW` 等 API 名 | 来自 Go 标准库 `syscall` 包，任何 Go Windows 程序都有，最小的 hello-world 也一样 |
| 无 Authenticode 签名 | 云端信誉无从查起，只能靠本地启发式打分 |
| 无版本资源 | 文件属性里连产品名都没有，看起来像"随手打包的临时程序" |
| 高熵的静态链接 | 依赖全部静态打进去，无动态库可佐证来源 |

本工具已排除其它常见诱因：**未加壳**（无 UPX/VMProtect 等）、
**业务代码不调用任何进程创建 API**（`os/exec` 只出现在测试辅助里，
不链接进发布产物）、文件熵 6.4（属正常范围，加壳器通常 >7.2）。

### 本项目已做的处置

**补齐 Windows 版本资源**。`make release` 会先生成 `.syso`（PE 的 `.rsrc` 段），
链入 Windows 产物：

```bash
make winres    # 单独生成
make release   # 自动先生成再交叉编译
```

生成的产物在文件属性 → 详细信息里能看到产品名、公司名、版本号，
不再是一个"匿名程序"，可有效降低启发式打分。

`.syso` 文件名带 `_windows_<arch>` 后缀，Go 只会把它链进对应平台的构建，
Linux / macOS 产物完全不受影响（回归测试 `format/winres_test.go` 守着这条）。

### 仍然建议做的两件事

版本资源只能降低误报概率，**不能根治**。彻底解决还需要：

1. **提交误报样本**，让微软更新特征库：
   <https://www.microsoft.com/wdsi/filesubmission>
   提交 `decompress-<版本>-windows-amd64.exe`，并附上文件的 SHA256。
   审核通过后该哈希会被加入白名单。

2. **代码签名**（Authenticode）。签名后的二进制在信誉模型里权重最高，
   也是微软官方给出的首要建议。目前本项目未接入签名流程——
   若需要，可购买 OV 证书后在 CI 中用 `signtool` 签名。

企业环境可临时让 IT 配置 Defender 排除项（按哈希或路径），
让用户先能用起来。

## 项目结构

```
decompress/
├── cmd/decompress/          程序入口
├── internal/
│   ├── core/                领域契约与调度引擎
│   │   ├── contract.go      Compression 接口、条目与请求/响应模型
│   │   ├── registry.go      格式注册表（插件容器）
│   │   ├── detect.go        魔数识别（由注册表汇总驱动）
│   │   ├── model.go         格式标识、任务配置、进度事件
│   │   ├── path.go          路径穿越防护
│   │   └── engine.go        工作队列调度
│   ├── format/              各格式解压实现（每个格式自行注册）
│   │   ├── register.go      内置注册表入口
│   │   ├── extract.go       内容写盘流程（归档 + 单流）
│   │   ├── layout.go        输出布局与命名决策
│   │   ├── layout_test.go   布局决策的纯函数测试
│   │   ├── zip.go           ZIP
│   │   ├── tar.go           TAR（含偏移定位）
│   │   ├── sevenzip.go      7Z
│   │   ├── rar.go           RAR（纯 Go）
│   │   ├── gzip.go          GZIP
│   │   ├── bzip2.go         BZIP2
│   │   ├── xz.go            XZ
│   │   └── zstd.go          ZSTD
│   ├── util/                与业务无关的通用能力
│   │   ├── fs.go            文件系统与路径操作
│   │   ├── stream.go        流式复制
│   │   └── charset.go       字符编码回退
│   ├── cli/                 命令行接口
│   └── testutil/            测试辅助（仅被 _test.go 引用）
├── build/winres/            Windows 版本资源模板
│   └── versioninfo.json     产品名/公司名/版本号等元数据
└── Makefile
```

### 测试的归属

测试跟着被测行为走，不分出独立的"集成测试"目录：

- 引擎的调度行为（队列、递归、嵌套落点）→ `core/engine_*_test.go`
- 布局决策的纯函数 → `format/layout_test.go`
- 布局与预扫描契约 → `format/smart_output_test.go`
- 各格式的解压正确性 → `format/format_test.go`
- Windows 版本资源的生成与挂载 → `format/winres_test.go`
- 纯函数与通用能力 → `core/core_test.go`、`util/util_test.go`

需要装配真实格式实现的测试使用**外部测试包**（`package core_test` /
`package format_test`）。这是硬性要求而非风格选择：内部测试包
`package core` 若 import `format`，而 `format` 又 import `core`，
Go 会报 `import cycle not allowed in test`；外部测试包编译为独立包，
`core_test → format → core` 不构成环。

跨包共用的测试辅助（构造归档、调用外部工具、检索产物目录树）集中在
`internal/testutil`，避免在多个包里重复实现。

`util` 只处理文件、路径与字节流，不理解"归档""条目"这些概念；
`format` 保留归档领域逻辑（条目写盘、布局决策），通用能力一律下沉到 `util`。

### 分层

```mermaid
graph TD
  CLI[cli 命令行] --> ENG[core.Engine 工作队列]
  CLI --> REG[core.Registry 插件容器]
  ENG --> REG
  ENG --> SAFE[core.SanitizeEntryPath 路径防护]
  REG -.->|init 注册| ZIP[Zip]
  REG -.->|init 注册| TAR[Tar]
  REG -.->|init 注册| SZ[SevenZ]
  REG -.->|init 注册| RAR[Rar]
  REG -.->|init 注册| GZ[Gzip]
  REG -.->|init 注册| BZ[Bzip2]
  REG -.->|init 注册| XZ[Xz]
  REG -.->|init 注册| ZS[Zstd]
  ZIP & TAR & SZ & RAR & GZ & BZ & XZ & ZS -->|实现| I[core.Compression]
  ZIP & TAR & SZ & RAR --> EA[extractArchive 归档落盘]
  GZ & BZ & XZ & ZS --> ES[extractStream 单流落盘]
  EA & ES --> UTIL[util 通用能力]
  EA -.-> PLAN[layout.go 输出布局]
```

每个格式实现统一的契约，魔数与扩展名由格式自己声明：

```go
type Compression interface {
    Name() string                        // 全局唯一标识，如 "zip"
    DisplayName() string                 // 展示名，如 "ZIP"
    Extensions() []string                // 扩展名，如 [".zip"]
    Magic() []Magic                      // 魔数识别规则
    // IsStream 声明该格式是否为单流格式
    //
    // 单流格式（gzip/bzip2/xz/zstd）内部没有条目概念，解压后恒产出
    // 一个文件；归档类格式（zip/tar/7z/rar）则有条目清单。
    // 引擎据此判断解压产物是"同一条解压链的下一环"还是"新的嵌套层级"。
    IsStream() bool
    // Scan 预扫描条目清单，不解压任何内容
    Scan(path string) ([]Entry, error)
    // Extract 实际解压，返回产物的文件路径列表
    Extract(ctx context.Context, req *ExtractRequest) (*ExtractResult, error)
}
```

新增一种格式只需新建一个文件实现该接口，并在 `init()` 中注册，
无需修改 `core` 或 `format` 的任何现有代码：

```go
func init() { Register(Zip{}) }
```

## 相对原版 Java 工具的改进

| 问题 | 原版行为 | 本实现 |
|---|---|---|
| Zip Slip 路径穿越 | 条目名直接拼路径，可写出目录外的任意文件 | 归一化 + 二次校验，恶意条目**中止整个解压** |
| `replaceAll` 误用正则 | 文件名当正则用，含 `.`/`(` 等会出错 | 改用 `filepath` 与显式字符串处理 |
| 目录内源压缩包被删 | 处理目录时源包被消费掉 | 默认保留（`-keep`），可显式关闭 |
| 输出名冲突 | 多归档解出同名文件时后者报错 | 自动追加序号（`a.tar`、`a.1.tar`） |
| 全串行解压 | 线程池核心线程数为 1 | 默认 4 路并行（`-j` 可调） |
| Windows 路径硬编码 | `replaceAll("/", "\\\\")` 在非 Windows 必崩 | 全平台统一用 `filepath` |
| tar 魔数过宽 | 512 字节内全窗口扫 `ustar` | 按 POSIX 规范定位偏移 257 |
| 目录名残留后缀 | `log.tar.gz` 解出 `log.tar/`，多一层目录 | 逐层剥离扩展名，`log.tar.gz` → `log/` |
| Windows 产物被报毒 | 无版本资源，易被启发式判定为可疑程序 | 补齐版本资源（产品名/公司名/版本号），并提供误报排查指引 |
| 无内容列举 | 只能解压，不能查看归档内容 | `--list` 支持 ZIP/TAR/7Z/RAR |
| 无 JSON 输出 | 仅文本 | `--json` 便于集成 |

## 第三方依赖

| 依赖 | 用途 | 许可 |
|---|---|---|
| `github.com/klauspost/compress` | Zstandard | MIT |
| `github.com/ulikunitz/xz` | XZ | BSD-2 |
| `github.com/bodgit/sevenzip` | 7-Zip | MIT |
| `github.com/nwaples/rardecode/v2` | RAR 2.0/3.0/5.0 | Unlicense |
| `golang.org/x/text` | GBK 文件名解码 | BSD-3 |
| `github.com/josephspurrier/goversioninfo` | 仅构建期：生成 Windows 版本资源 | MIT |

全部为纯 Go 实现，交叉编译无需 CGO 工具链。

`goversioninfo` 是**构建期工具**，通过 `go.mod` 的 `tool` 指令锁定版本，
不链接进任何发布产物——运行时依赖只有上表前五行。

## 测试

```bash
make test           # 全部测试
make check          # 格式检查 + vet + 测试
```

测试覆盖：

- 8 种格式的真实归档解压
- RAR 加密（正确/错误/缺失密码）、分卷、solid
- Zip Slip 攻击防护
- 路径归一化的边界用例
- 输出布局（顶层解压链强制建目录 / 单流格式恒为平铺 / 嵌套按条目数布局 / 无扩展名 `.d` / 空归档不产出）
- 复合后缀还原（`.tgz` → `.tar`），顶层解压链的目录名逐层剥离扩展名
- 预扫描不解压内容
- 嵌套压缩逐层解开，且保持目录层级
- 目录递归与队列派生处理
- 与 7z 的解压结果逐文件比对

## 许可

本项目为原版 `Decompression.exe` 的 Go 重写，仅用于学习与内部工具场景。
