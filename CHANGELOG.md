# rag CHANGELOG

> 本文件由 `matrix-tools/scripts/gen_changelogs.py` 从 git 历史生成，按 tag 切段、取每段内的 conventional commit 渲染。

## [v0.8.2] — 2026-10-20

### 修复(go)：Go 地板降到 go 1.24.0，CI go-version 同步到 1.24

承接 matrix-tools#22（tier0 三仓的地板被无谓抬高）——本轮把 tier1+ 里
**依赖链全兼容**的仓也降下来（matrix-tools#34 的排查结论）。
## 依据
- **依赖链上没有要求更高版本的包**：本仓的每个依赖（直接与间接）的 go 指令
  都 ≤ 1.24.x，本仓声明 1.26.0 没有任何依赖需要它。
- **代码本身兼容 1.24**：真实 go1.24.0 工具链（`GOTOOLCHAIN=go1.24.0`）
  build + test 全绿——实跑，不是推断。
- **门槛是 go 指令不是 toolchain**（编译器实证）：`go 1.28.0` 被拒，
  `go 1.24.0` + `toolchain go1.28.0` 通过。
## toolchain 行的取舍：删掉
toolchain **不构成门槛**，但 `toolchain go1.27.2` 会让 `go` 命令**自动下载**
1.27.2，与「最低支持版本是 1.24」相悖。删掉，也与基座 oekit 六个基础仓一致。
## 依赖地板同步重钉
降 go 指令后，`require` 必须指向**已发新 tag 的 tier0 仓**（它们的 tag 快照
里才是 go 1.24.0）——否则 MVS 会拉到旧 tag 的 1.26 地板，降级形同虚设。
本仓的内部 require 已随 `go mod tidy` 一并抬到对应的新 tag。
## 改动
- `go.mod`：`go 1.26.0` → `go 1.24.0`；删 `toolchain go1.27.2`；
  内部 require 抬到 tier0 新 tag
- `.github/workflows/ci.yml`：`go-version: '1.26'` → `'1.24'`
CI 的 go-version 与 go.mod 的 go 指令**成对**——这是跨层通用纪律，不因本次
降级而放松。CI 用高版本构建、go.mod 承诺低版本时，CI 全绿但用户按最低版本
编译失败，门禁绿了而承诺没兑现。
## 验证
- `GOTOOLCHAIN=go1.24.0 go build ./...` → exit 0
- `GOTOOLCHAIN=go1.24.0 go test ./...` → 全绿
- `go mod tidy` → 零差异
门禁见 oagentkit/matrix-tools：`scripts/verify_go_floor.py`（判据 4 覆盖
tier1+ 相对地板，判据独立不复用任何生成器）。

## [v0.8.1] — 2026-10-20

### 修复(docs)：Embedding/Retriever 改为真实接口名 Embedder/KnowledgeService

Co-authored-by: Claude Opus 4.8 (1M context) <noreply@anthropic.com>

## [v0.8.0] — 2026-10-19

- 杂务(deps)：依赖地板对齐远端最新（textx v0.5.0） (13)

## [v0.7.0] — 2026-10-19

- 杂务(deps)：依赖地板对齐远端最新（textx v0.4.0 / concurrency v0.17.1 / logx v0.7.1 / pkg v0.15.1）

## [v0.6.0] — 2026-10-19

### 重构(rag)!：包上移至 module 根，import 路径不再重复末段

BREAKING CHANGE: import 路径 github.com/oagentkit/rag/rag → github.com/oagentkit/rag。
包名 rag 恰好等于仓名 rag，留在 rag/ 子目录会产出 stutter 的公开路径
（Go 惯例：包名不重复 import 路径末段）。
矩阵 import 规则随之收敛：matrix_paths.new_import_path 在「包名末段 == 仓名」时
收口到 module 根，不再拼末段。这是该规则的**唯一例外**，写在唯一构造点里，
gen_versions / gen_llms_txt / rewrite_imports / test_matrix_paths 全部随之生效。
与 oagentkit/llm 同批：全矩阵 47 个包中只有这两处仓名=包名。

## [v0.5.0] — 2026-10-19

- 杂务(deps)：oekit 依赖升级（concurrency v0.16.0→v0.17.0）

## [v0.4.1] — 2026-10-18

- 与 v0.4.0 相比无代码差异（版本号/打包变更）

## [v0.4.0] — 2026-10-18

### 新增：rag 仓建立——知识检索（1 包）

从 agentkit 单仓切出 tier1 能力层模块，module 路径 github.com/oagentkit/rag。
收编 rag 包（原 knowledge/rag，路径按三层方案提到仓根一级）：
Embedding 接口、向量后端可插拔（Milvus）、混合召回与重排。
矩阵内依赖：textx。外部地板 eino + milvus-sdk-go。
AGENTS.md 立了混合召回纪律：单路失败不得降级为 (nil,nil)，全部路失败须返回 panic 错误。
门禁：go build / go vet / go test -race 全绿，gofmt 干净。

### 修复(rag)：迁移已废弃的 syncx.Slot.Panicked 到 Slot.Err + errors.As

oekit/concurrency v0.7.0 起 panic 由 Slot.Err 承载 *async.PanicError
（errors.As 可辨、携带栈），Slot.Panicked 标 Deprecated，staticcheck SA1019 拦下。
行为保持不变：panic 仍只降级该路、不影响另一路；文案从 %v 改 %w，
让 PanicError 的栈能一路 errors.Unwrap 到调用方。
本地 golangci-lint 0 issues。

- 杂务(deps)：依赖地板升到「历史最新 + 0.1.0」
- 杂务(deps)：跟进最新 tag
- 杂务(deps)：oekit 依赖升级
- 杂务(deps)：oekit/concurrency v0.12.0 -> v0.13.1
- 杂务(deps)：oekit 三模块升至最新（pkg v0.13.0 / concurrency v0.12.0 / logx v0.7.0）
- 杂务(deps)：oekit/concurrency v0.6.0 -> v0.9.0

- CI：接入 org 共享门禁（规则本体在 oagentkit/.github）
