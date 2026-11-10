# rag CHANGELOG

> 本文件由 `matrix-tools/scripts/gen_changelogs.py` 从 git 历史生成，按 tag 切段、取每段内的 conventional commit 渲染。

## [v0.11.8] — 2026-11-11

### 修复(milvus,local)：存在路径形状校验 + 错误字段标准名（#97 #98）

- #97 ensureCollection 的「已存在」路径补 DescribeCollection 形状校验：
  向量维度与本仓配置一致、主键字段名/四字段形状兼容，漂移在构造期显式
  报错——旧版本/外部工具建出的集合不再「构造期全绿、首次读写才在服务端
  深处炸」；未加载（外部建而不 load / 手工 release）时补 LoadCollection，
  与创建路径收尾加载同语义。dim 漂移带病运行的代价是写入期炸在
  IndexQuiet 的 delete-已执行窗口（文件从检索面消失），必须拒绝
- #98 重扫路径 3 处错误字段从手写 "error" 字面量改 logx.ErrKey（"err"）——
  两矩阵落同一 Loki/ES，查询侧不再 OR 双写
（#97 的运行时校验逻辑依赖真实 Milvus，单测覆盖不了 GetLoadState 分支；
集成测试 milvus_integration_test.go 常规 CI 不跑。逻辑面以 sdk 类型编译期
校验 + 既有单测回归。）

## [v0.11.7] — 2026-11-10

### 修复(chunking)：单块 ≤ maxChunkRunes 不变量在任何形态下成立（#85）

style: gofmt local_test.go——lint 门禁红

- 杂务(deps)：依赖地板追到最新——github.com/oagentkit/textx v0.10.7

| 依赖 | 现地板 | 抬到 |
|---|---|---|
| `github.com/oagentkit/textx` | `v0.10.6` | `v0.10.7` |
## 为什么
矩阵的**发版时刻地板时效**门禁（`verify_tag_floor --ref HEAD`）要求：各仓 HEAD
（= 下次发版会发出去的内容）的依赖地板，相对那一刻的依赖最新版本，不能已落后。
oekit 基座与矩阵模块都在高频发版 ⇒ 地板是**跑步机**，落后一格就会判红。
## 兼容性实测
本工具在提交前跑完 `go build` / `go vet` / `go test -race -count=1 ./...`（全绿才提交）
与 `go mod tidy -diff`（无漂移）⇒ 改动**只影响依赖版本**，零行为差异。
只改 `go.mod` / `go.sum` ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。

- 杂务(deps)：依赖地板追到最新——github.com/oekit/concurrency v0.31.2

| 依赖 | 现地板 | 抬到 |
|---|---|---|
| `github.com/oekit/concurrency` | `v0.31.1` | `v0.31.2` |
| `github.com/oekit/logx` | `v0.9.0` | `v0.9.1` |
## 为什么
矩阵的**发版时刻地板时效**门禁（`verify_tag_floor --ref HEAD`）要求：各仓 HEAD
（= 下次发版会发出去的内容）的依赖地板，相对那一刻的依赖最新版本，不能已落后。
oekit 基座与矩阵模块都在高频发版 ⇒ 地板是**跑步机**，落后一格就会判红。
## 兼容性实测
本工具在提交前跑完 `go build` / `go vet` / `go test -race -count=1 ./...`（全绿才提交）
与 `go mod tidy -diff`（无漂移）⇒ 改动**只影响依赖版本**，零行为差异。
只改 `go.mod` / `go.sum` ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。


## [v0.11.6] — 2026-11-10

### 修复(local)：分词器覆盖全部非 ASCII 字母，谚文/假名等不再静默丢失（#84）

style: gofmt tokenize_test.go——lint 门禁红

- 杂务(deps)：依赖地板追到最新——github.com/oekit/pkg v0.41.1

| 依赖 | 现地板 | 抬到 |
|---|---|---|
| `github.com/oekit/pkg` | `v0.41.0` | `v0.41.1` |
## 为什么
矩阵的**发版时刻地板时效**门禁（`verify_tag_floor --ref HEAD`）要求：各仓 HEAD
（= 下次发版会发出去的内容）的依赖地板，相对那一刻的依赖最新版本，不能已落后。
oekit 基座与矩阵模块都在高频发版 ⇒ 地板是**跑步机**，落后一格就会判红。
## 兼容性实测
本工具在提交前跑完 `go build` / `go vet` / `go test -race -count=1 ./...`（全绿才提交）
与 `go mod tidy -diff`（无漂移）⇒ 改动**只影响依赖版本**，零行为差异。
只改 `go.mod` / `go.sum` ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。

- 杂务(deps)：依赖地板追到最新——github.com/oekit/concurrency v0.31.1

| 依赖 | 现地板 | 抬到 |
|---|---|---|
| `github.com/oekit/concurrency` | `v0.31.0` | `v0.31.1` |
| `github.com/oekit/logx` | `v0.8.12` | `v0.9.0` |
| `github.com/oekit/pkg` | `v0.40.0` | `v0.41.0` |
## 为什么
矩阵的**发版时刻地板时效**门禁（`verify_tag_floor --ref HEAD`）要求：各仓 HEAD
（= 下次发版会发出去的内容）的依赖地板，相对那一刻的依赖最新版本，不能已落后。
oekit 基座与矩阵模块都在高频发版 ⇒ 地板是**跑步机**，落后一格就会判红。
## 兼容性实测
本工具在提交前跑完 `go build` / `go vet` / `go test -race -count=1 ./...`（全绿才提交）
与 `go mod tidy -diff`（无漂移）⇒ 改动**只影响依赖版本**，零行为差异。
只改 `go.mod` / `go.sum` ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。

- 杂务(deps)：依赖地板追到最新——github.com/oagentkit/textx v0.10.6

| 依赖 | 现地板 | 抬到 |
|---|---|---|
| `github.com/oagentkit/textx` | `v0.10.5` | `v0.10.6` |
## 为什么
矩阵的**发版时刻地板时效**门禁（`verify_tag_floor --ref HEAD`）要求：各仓 HEAD
（= 下次发版会发出去的内容）的依赖地板，相对那一刻的依赖最新版本，不能已落后。
oekit 基座与矩阵模块都在高频发版 ⇒ 地板是**跑步机**，落后一格就会判红。
## 兼容性实测
本工具在提交前跑完 `go build` / `go vet` / `go test -race -count=1 ./...`（全绿才提交）
与 `go mod tidy -diff`（无漂移）⇒ 改动**只影响依赖版本**，零行为差异。
只改 `go.mod` / `go.sum` ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。


## [v0.11.5] — 2026-11-09

### 修复：rag 第三轮 issue 清零（#75 #76）

* fix(local): carryOverlap 取尾改对 TrimSpace 后母块文本+flush 兜底比对 TrimSpace——空行紧贴标题+裸标题收尾不再产出幻影块（#75）
* fix(hybrid): 聚合裁决区分 panic 与普通错误——幸存路合法空命中返回空而非回错、双路全败才回错（#76）

- 杂务(deps)：依赖地板追到最新——github.com/oekit/pkg v0.40.0

| 依赖 | 现地板 | 抬到 |
|---|---|---|
| `github.com/oekit/pkg` | `v0.39.0` | `v0.40.0` |
## 为什么
矩阵的**发版时刻地板时效**门禁（`verify_tag_floor --ref HEAD`）要求：各仓 HEAD
（= 下次发版会发出去的内容）的依赖地板，相对那一刻的依赖最新版本，不能已落后。
oekit 基座与矩阵模块都在高频发版 ⇒ 地板是**跑步机**，落后一格就会判红。
## 兼容性实测
本工具在提交前跑完 `go build` / `go vet` / `go test -race -count=1 ./...`（全绿才提交）
与 `go mod tidy -diff`（无漂移）⇒ 改动**只影响依赖版本**，零行为差异。
只改 `go.mod` / `go.sum` ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。

- 杂务(deps)：依赖地板追到最新——github.com/oagentkit/textx v0.10.5

| 依赖 | 现地板 | 抬到 |
|---|---|---|
| `github.com/oagentkit/textx` | `v0.10.4` | `v0.10.5` |
| `github.com/oekit/concurrency` | `v0.30.1` | `v0.31.0` |
## 为什么
矩阵的**发版时刻地板时效**门禁（`verify_tag_floor --ref HEAD`）要求：各仓 HEAD
（= 下次发版会发出去的内容）的依赖地板，相对那一刻的依赖最新版本，不能已落后。
oekit 基座与矩阵模块都在高频发版 ⇒ 地板是**跑步机**，落后一格就会判红。
## 兼容性实测
本工具在提交前跑完 `go build` / `go vet` / `go test -race -count=1 ./...`（全绿才提交）
与 `go mod tidy -diff`（无漂移）⇒ 改动**只影响依赖版本**，零行为差异。
只改 `go.mod` / `go.sum` ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。

- 杂务(deps)：依赖地板追到最新——github.com/oagentkit/textx v0.10.4

| 依赖 | 现地板 | 抬到 |
|---|---|---|
| `github.com/oagentkit/textx` | `v0.10.3` | `v0.10.4` |
| `github.com/oekit/concurrency` | `v0.29.0` | `v0.30.1` |
| `github.com/oekit/logx` | `v0.8.11` | `v0.8.12` |
| `github.com/oekit/pkg` | `v0.38.0` | `v0.39.0` |
## 为什么
矩阵的**发版时刻地板时效**门禁（`verify_tag_floor --ref HEAD`）要求：各仓 HEAD
（= 下次发版会发出去的内容）的依赖地板，相对那一刻的依赖最新版本，不能已落后。
oekit 基座与矩阵模块都在高频发版 ⇒ 地板是**跑步机**，落后一格就会判红。
## 兼容性实测
本工具在提交前跑完 `go build` / `go vet` / `go test -race -count=1 ./...`（全绿才提交）
与 `go mod tidy -diff`（无漂移）⇒ 改动**只影响依赖版本**，零行为差异。
只改 `go.mod` / `go.sum` ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。


## [v0.11.4] — 2026-11-08

- 与 v0.11.3 相比无代码差异（版本号/打包变更）

## [v0.11.3] — 2026-11-08

### 修复(chunker,milvus)：幻影块守卫跨空行/EOF 存活 + 文件身份键全链路统一（rag#60 rag#59）

Merge branch 'main' into fix/audit-issues-60-59

- 杂务(deps)：依赖地板追到最新——基座 v0.38.0/v0.29.0/v0.8.11 + 矩阵 tier0 新 tag（按本仓依赖取齐）

发版时刻地板时效门禁要求追平。
兼容性实测：go build / go vet / go test -race -count=1 ./... 全绿，go mod tidy -diff 无漂移。


## [v0.11.2] — 2026-11-08

- 杂务(deps)：依赖地板追平 oekit BOM v0.2.9——pkg v0.37.4 / logx v0.8.10 / concurrency v0.28.4

## [v0.11.1] — 2026-11-08

- 杂务(deps)：依赖地板追到最新——concurrency v0.28.4 / logx v0.8.9（按本仓依赖取齐）

oekit 基座 v0.28.4 / v0.8.9 落地，发版时刻地板时效门禁要求追平（matrix-tools#188 发版链）。
兼容性实测：go build / go vet / go test -race -count=1 ./... 全绿，go mod tidy -diff 无漂移。

- 杂务(deps)：依赖地板追到最新——github.com/oekit/pkg v0.37.4

pkg v0.37.4（httpjson NormalizeBaseURL scheme 大小写归一 #239 + refresh_versions）落地，地板同步追平（matrix-tools#188 发版链）。
兼容性实测：go build / go vet / go test -race -count=1 ./... 全绿，go mod tidy -diff 无漂移。

- 杂务(deps)：依赖地板追到最新——concurrency v0.28.3 / logx v0.8.8 / pkg v0.37.3 / textx v0.10.1 / guardx v0.23.1 / runtime v0.12.1（按本仓依赖取齐）

发版时刻地板时效门禁（verify_tag_floor --ref HEAD）要求追平（matrix-tools#188 发版链）。
兼容性实测：go build / go vet / go test -race -count=1 ./... 全绿，go mod tidy -diff 无漂移。

- 杂务(deps)：依赖地板追到最新——github.com/oekit/concurrency v0.28.1

| 依赖 | 现地板 | 抬到 |
|---|---|---|
| `github.com/oekit/concurrency` | `v0.28.0` | `v0.28.1` |
## 为什么
矩阵的**发版时刻地板时效**门禁（`verify_tag_floor --ref HEAD`）要求：各仓 HEAD
（= 下次发版会发出去的内容）的依赖地板，相对那一刻的依赖最新版本，不能已落后。
oekit 基座与矩阵模块都在高频发版 ⇒ 地板是**跑步机**，落后一格就会判红。
## 兼容性实测
本工具在提交前跑完 `go build` / `go vet` / `go test -race -count=1 ./...`（全绿才提交）
与 `go mod tidy -diff`（无漂移）⇒ 改动**只影响依赖版本**，零行为差异。
只改 `go.mod` / `go.sum` ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。

- 杂务(build)：补统一 Makefile——本地 ci 门禁与共享 CI 同口径（agentkit#138）

## [v0.11.0] — 2026-11-07

- 杂务(deps)：依赖地板追到最新——github.com/oekit/logx v0.8.6

| 依赖 | 现地板 | 抬到 |
|---|---|---|
| `github.com/oekit/logx` | `v0.8.5` | `v0.8.6` |
## 为什么
矩阵的**发版时刻地板时效**门禁（`verify_tag_floor --ref HEAD`）要求：各仓 HEAD
（= 下次发版会发出去的内容）的依赖地板，相对那一刻的依赖最新版本，不能已落后。
oekit 基座与矩阵模块都在高频发版 ⇒ 地板是**跑步机**，落后一格就会判红。
## 兼容性实测
本工具在提交前跑完 `go build` / `go vet` / `go test -race -count=1 ./...`（全绿才提交）
与 `go mod tidy -diff`（无漂移）⇒ 改动**只影响依赖版本**，零行为差异。
只改 `go.mod` / `go.sum` ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。

- 杂务(logging)：logger 组件名对齐矩阵规则 oagentkit-<module>（agentkit#139）

ragLog 组件名 "agentkit-rag" → "oagentkit-rag"，对齐 modules.json
loggingRule 要求的 logx.Delegating("oagentkit-<module>") 命名。
行为可见变更：日志 component 属性值由 agentkit-rag 变为 oagentkit-rag。
按组件名做日志过滤/路由的下游（采集规则、告警匹配等）需同步改为新名，
否则旧名规则不再命中。
Refs oagentkit/agentkit#139

- 杂务(deps)：依赖地板追到最新——github.com/oekit/pkg v0.37.1

| 依赖 | 现地板 | 抬到 |
|---|---|---|
| `github.com/oekit/pkg` | `v0.37.0` | `v0.37.1` |
## 为什么
矩阵的**发版时刻地板时效**门禁（`verify_tag_floor --ref HEAD`）要求：各仓 HEAD
（= 下次发版会发出去的内容）的依赖地板，相对那一刻的依赖最新版本，不能已落后。
oekit 基座与矩阵模块都在高频发版 ⇒ 地板是**跑步机**，落后一格就会判红。
## 兼容性实测
本工具在提交前跑完 `go build` / `go vet` / `go test -race -count=1 ./...`（全绿才提交）
与 `go mod tidy -diff`（无漂移）⇒ 改动**只影响依赖版本**，零行为差异。
只改 `go.mod` / `go.sum` ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。


## [v0.10.0] — 2026-11-07

### 修复(rag)：FuseRRF 稳定键/幻影块/embedding 批上限/Milvus VarChar 全字段守卫（#40-#43）

## 概览
一批修复 4 个 issue：FuseRRF 融合键塌缩（#40）、carryOverlap 幻影块（#41）、embedding 批大小超端点上限（#42）、Milvus VarChar 守卫缺失（#43）。本地 `gofmt`/`go vet`/`go build`/`go test -race -count=2` 全绿，覆盖率 70.5%（门禁 30%）。
close #40
close #41
close #42
close #43
---
## #40 FuseRRF 以 Content 当 key——同内容跨文件塌缩 + 同路自增强
**根因**：Local 后端 chunk 只填 Content/Metadata、从不设 ID，`FuseRRF` 对空 ID 回退用内容做融合 key。两份内容相同但来源不同的 chunk（许可证头、告警模板、同名配置样例）在 key 上塌缩，`best` 只保留一个代表副本——另一文件的 chunk 从结果消失且无信号；同路两个副本还各占一个 rank 给同一 key 双重计分（自增强挤占 topK）。
**修法**：
- chunk 标识单源 `chunkID(file, seq)` = `file#chunk_N`（rag.go）：Local 索引给每个 chunk 赋确定性 ID；Milvus 检索路径把 id 列加入 search output 并回传到 `Chunk.ID`——双路统一格式，跨路共识按 ID 归并。
- `FuseRRF` 融合 key 三级回退：ID → `Metadata["file"]`+内容 → 纯内容；去重语义明确为**同源去重**（同一 chunk 的跨路副本归并），不是同内容跨文件去重。
- 同一 list 内同 key 只计首个名次，同路重复不再给自己加权。
**测试**：`TestLocalChunkStableID`（两文件同内容不塌缩、ID 格式/互异）、`TestFuseRRFKeySemantics`（跨文件保留/同源去重/同路不自增强/无元数据回退）、`TestMilvusHitChunksCarriesID`（接口桩构造 SearchResult，无需外部 Milvus）。
## #41 carryOverlap 把 100-rune 纯重叠尾巴旋成独立「幻影块」
**根因**：正文行使 `curLen ≥ chunkTargetRunes` 且还有后续行时 `carryOverlap` flush 母块、cur 只剩 100-rune 携带尾；下一行若是标题，`carryOverlap` 对纯尾巴取 `overlapTail` 返回 `""`（`len(runes) <= overlapRunes`），随后 `flush` 把尾巴独立成块——内容=母块末尾逐字副本、顶着上一节标题，Local 与 Milvus 双路索引同时被污染（每边界多付一次 embedding，检索面母块+幻影块双占 topK）。同根因副产物：代码块内 `maxChunkRunes` 强制 flush 后闭合围栏落在空 cur，旋出 content 仅为 ```` ``` ```` 的垃圾块。
**修法**：`mdChunker` 新增 `curIsCarry` 状态（cur 仅含携带尾且此后未追加新内容，`appendLine` 清除）；标题边界、超长行边界遇纯携带尾直接丢弃——母块已逐字持有该文本，纯重叠独立块严格劣于丢弃；闭合围栏落在空 cur 不再收留。
**测试**：`TestChunkMarkdownNoPhantomCarryTail`（issue 原复现：甲节 1200-rune 单行+乙节标题，2 块无幻影；含「任何块不得是另一块末尾 overlap 窗口逐字副本」通用断言 + 边界后跟正文行的对照组证明重叠机制本身保留）、`TestChunkMarkdownNoLoneFenceGarbageChunk`（孤立围栏垃圾块回归）。
## #42 embedder batchSize=16 超过 DashScope text-embedding-v3 单批上限 10
**根因**：`embedder.go` 默认 `batchSize = 16`，而该实现的类型注释明确点名支持百炼 compatible-mode——DashScope text-embedding-v3/v4 单批上限 10 条（端点不代调用方拆分，超限直接 4xx）。输入 >10 条时第二批起全部报错，「配百炼 v3」开箱即挂，排查方向会被带去 API key/网络。
**修法**：默认批大小降为 10（`defaultEmbedBatchSize`，对齐支持的端点中最受限者，更小批次对所有端点合法）；新增 `WithBatchSize(n)` 选项，接入上限更高的提供商（OpenAI 2048 / DashScope v1-v2 25）可调大减少请求轮次，`n<=0` 视为未设置；文档写明各提供商上限矩阵。
**测试**：`TestOpenAIEmbedderBatching` 更新（20 条 → 10+10，且断言任何单请求 ≤ 端点上限）、`TestOpenAIEmbedderBatchSizeOption`（WithBatchSize(16) → 16+4；非法值回落默认）。
## #43 milvus VarChar 上限守卫只护 content——heading/file/主键 id 按字节超限致整文件 Upsert 失败
**根因**：schema 中 id=VarChar(528)、file=VarChar(512)、heading=VarChar(512)（Milvus 按字节计长），写入侧守卫只有 chunker 的 `maxChunkRunes`（只护 content）。CJK 标题 ≥171 字（513 字节）即让该节全部 chunk 的 Upsert 失败；深路径+CJK 路径 file 可超 512 字节，且 `id = file+"#chunk_N"` 同步破 528——错误埋在 Milvus 服务端报错深处，文件从语义路消失。
**修法**：字段上限单源化（`milvusIDMaxLen`/`milvusContentMaxLen`/`milvusFileMaxLen`/`milvusHeadingMaxLen`，schema 定义与写入守卫共用）；`indexRows` 对全部标量字段做字节守卫，按字段语义区分处理：
- **heading**：按 rune 边界截断（`truncateUTF8` 不劈多字节字符）——标题仅是展示/过滤元数据，内容全文保留在 content 字段；
- **file**：超限截断到上限内并附加全路径 sha256 短散列后缀——朴素截断会让共享长前缀的两个深路径文件 file 与主键同时碰撞、Upsert 互相覆盖（静默丢数据，比超限报错更坏）；
- **主键 id**：超限 fail-fast 报错并注明字段与上限——主键不可截断，截断=跨文件碰撞。
**测试**：`TestIndexRowsVarCharGuards`（600 字节 CJK 标题截断保前缀且 UTF-8 合法、深路径 file/id 有界且共享前缀不碰撞、短路径原样透传）、`TestMilvusRowIDGuard`（主键压线/越界边界：9 位序号恰好 528、10 位报错）——均为纯函数单测，不依赖外部 Milvus 实例。

- 杂务(deps)：依赖地板追到最新——github.com/oekit/concurrency v0.28.0

| 依赖 | 现地板 | 抬到 |
|---|---|---|
| `github.com/oekit/concurrency` | `v0.27.0` | `v0.28.0` |
## 为什么
矩阵的**发版时刻地板时效**门禁（`verify_tag_floor --ref HEAD`）要求：各仓 HEAD
（= 下次发版会发出去的内容）的依赖地板，相对那一刻的依赖最新版本，不能已落后。
oekit 基座与矩阵模块都在高频发版 ⇒ 地板是**跑步机**，落后一格就会判红。
## 兼容性实测
本工具在提交前跑完 `go build` / `go vet` / `go test -race -count=1 ./...`（全绿才提交）
与 `go mod tidy -diff`（无漂移）⇒ 改动**只影响依赖版本**，零行为差异。
只改 `go.mod` / `go.sum` ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。

- 杂务(deps)：依赖地板追到最新——github.com/oekit/logx v0.8.5

| 依赖 | 现地板 | 抬到 |
|---|---|---|
| `github.com/oekit/logx` | `v0.8.4` | `v0.8.5` |
| `github.com/oekit/pkg` | `v0.36.0` | `v0.37.0` |
## 为什么
矩阵的**发版时刻地板时效**门禁（`verify_tag_floor --ref HEAD`）要求：各仓 HEAD
（= 下次发版会发出去的内容）的依赖地板，相对那一刻的依赖最新版本，不能已落后。
oekit 基座与矩阵模块都在高频发版 ⇒ 地板是**跑步机**，落后一格就会判红。
## 兼容性实测
本工具在提交前跑完 `go build` / `go vet` / `go test -race -count=1 ./...`（全绿才提交）
与 `go mod tidy -diff`（无漂移）⇒ 改动**只影响依赖版本**，零行为差异。
只改 `go.mod` / `go.sum` ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。

- 杂务(deps)：依赖地板追到最新——github.com/oagentkit/textx v0.10.0

| 依赖 | 现地板 | 抬到 |
|---|---|---|
| `github.com/oagentkit/textx` | `v0.9.0` | `v0.10.0` |
| `github.com/oekit/concurrency` | `v0.25.1` | `v0.27.0` |
| `github.com/oekit/logx` | `v0.8.3` | `v0.8.4` |
| `github.com/oekit/pkg` | `v0.34.1` | `v0.36.0` |
## 为什么
矩阵的**发版时刻地板时效**门禁（`verify_tag_floor --ref HEAD`）要求：各仓 HEAD
（= 下次发版会发出去的内容）的依赖地板，相对那一刻的依赖最新版本，不能已落后。
oekit 基座与矩阵模块都在高频发版 ⇒ 地板是**跑步机**，落后一格就会判红。
## 兼容性实测
本工具在提交前跑完 `go build` / `go vet` / `go test -race -count=1 ./...`（全绿才提交）
与 `go mod tidy -diff`（无漂移）⇒ 改动**只影响依赖版本**，零行为差异。
只改 `go.mod` / `go.sum` ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。


## [v0.9.0] — 2026-11-06

### 修复(local,milvus)：rescan 读失败告警兑现 doc + NewMilvusStore nil embedder 返回 error（#33 #34）

rag#33——scanMarkdown 单文件 ReadFile 失败补 ragLog.Warn（与目录项错误同
面）：doc 明写「部分失败告警后照常换入」，此前静默跳过 = 文档从索引静默
消失且违反自家 doc（权限收紧/磁盘抖动的排查方向被带偏到检索面）。
rag#34——NewMilvusStore 构造期判 nil embedder 返回 error（对齐包内其他
构造面）：此前直接 embedder.Dim() nil 解引用 panic 且 doc 未声明。
TestRescanReadErrorWarns / TestNewMilvusStoreNilEmbedder。全仓 -race 绿。
关联 oagentkit/rag#33 oagentkit/rag#34
Co-authored-by: Murphy-Yi <zhangyi@Murphy-YideMac-Mini.local>

### 修复(hybrid,tool)：topK 缺省归一 + NewSearchTool error 通道（#30 #31）

rag#30——Hybrid.Retrieve 入口与两个后端同口径 topK<=0 → defaultTopK：此前
原始值直通 FuseRRF，topK=0 静默吞掉两路全部结果（n=0 err=nil 比报错更坏）、
topK<0 在 keys[:topK] 裸 panic（FanInErr 收口之外的主 goroutine，降级机制
接不住）。FuseRRF 导出面负数钳零（导出纯函数不 panic）。
rag#31——「静态 schema 即编程错误」是内部注释非 API 契约：
- NewSearchTool 改返回 (tool.BaseTool, error)——InferTool 对入参结构体的
  反射可因字段/标签问题失败（下游包装层改了结构体即触发），导出面给
  error 通道，装配期失败可被宿主接住
- AsTool 接口路径（Local/MilvusStore）panic 契约显式写进 doc（接口签名
  无 error 通道）——文档与行为对齐，不再是隐式内部注释
TestHybridRetrieveTopKNormalized：topK 0/-1/-100 三点归一 + FuseRRF 负数
钳零 + 正常截断语义不变。
关联 oagentkit/rag#30 oagentkit/rag#31
Co-authored-by: Murphy-Yi <zhangyi@Murphy-YideMac-Mini.local>

- 杂务(deps)：依赖地板追到最新——github.com/oagentkit/textx v0.9.0

| 依赖 | 现地板 | 抬到 |
|---|---|---|
| `github.com/oagentkit/textx` | `v0.8.0` | `v0.9.0` |
## 为什么
矩阵的**发版时刻地板时效**门禁（`verify_tag_floor --ref HEAD`）要求：各仓 HEAD
（= 下次发版会发出去的内容）的依赖地板，相对那一刻的依赖最新版本，不能已落后。
oekit 基座与矩阵模块都在高频发版 ⇒ 地板是**跑步机**，落后一格就会判红。
## 兼容性实测
本工具在提交前跑完 `go build` / `go vet` / `go test -race -count=1 ./...`（全绿才提交）
与 `go mod tidy -diff`（无漂移）⇒ 改动**只影响依赖版本**，零行为差异。
只改 `go.mod` / `go.sum` ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。
Co-authored-by: Murphy-Yi <zhangyi@Murphy-YideMac-Mini.local>

- 杂务(deps)：依赖地板追到最新——github.com/oekit/concurrency v0.25.1

| 依赖 | 现地板 | 抬到 |
|---|---|---|
| `github.com/oekit/concurrency` | `v0.24.0` | `v0.25.1` |
| `github.com/oekit/logx` | `v0.8.2` | `v0.8.3` |
## 为什么
矩阵的**发版时刻地板时效**门禁（`verify_tag_floor --ref HEAD`）要求：各仓 HEAD
（= 下次发版会发出去的内容）的依赖地板，相对那一刻的依赖最新版本，不能已落后。
oekit 基座与矩阵模块都在高频发版 ⇒ 地板是**跑步机**，落后一格就会判红。
## 兼容性实测
本工具在提交前跑完 `go build` / `go vet` / `go test -race -count=1 ./...`（全绿才提交）
与 `go mod tidy -diff`（无漂移）⇒ 改动**只影响依赖版本**，零行为差异。
只改 `go.mod` / `go.sum` ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。
Co-authored-by: Murphy-Yi <zhangyi@Murphy-YideMac-Mini.local>

- 杂务(deps)：依赖地板追到最新——github.com/oagentkit/textx v0.8.0

| 依赖 | 现地板 | 抬到 |
|---|---|---|
| `github.com/oagentkit/textx` | `v0.7.2` | `v0.8.0` |
## 为什么
矩阵的**发版时刻地板时效**门禁（`verify_tag_floor --ref HEAD`）要求：各仓 HEAD
（= 下次发版会发出去的内容）的依赖地板，相对那一刻的依赖最新版本，不能已落后。
oekit 基座与矩阵模块都在高频发版 ⇒ 地板是**跑步机**，落后一格就会判红。
## 兼容性实测
本工具在提交前跑完 `go build` / `go vet` / `go test -race -count=1 ./...`（全绿才提交）
与 `go mod tidy -diff`（无漂移）⇒ 改动**只影响依赖版本**，零行为差异。
只改 `go.mod` / `go.sum` ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。
Co-authored-by: Murphy-Yi <zhangyi@Murphy-YideMac-Mini.local>

- 杂务(deps)：依赖地板追到最新——github.com/oekit/pkg v0.34.1

| 依赖 | 现地板 | 抬到 |
|---|---|---|
| `github.com/oekit/pkg` | `v0.34.0` | `v0.34.1` |
## 为什么
矩阵的**发版时刻地板时效**门禁（`verify_tag_floor --ref HEAD`）要求：各仓 HEAD
（= 下次发版会发出去的内容）的依赖地板，相对那一刻的依赖最新版本，不能已落后。
oekit 基座与矩阵模块都在高频发版 ⇒ 地板是**跑步机**，落后一格就会判红。
## 兼容性实测
本工具在提交前跑完 `go build` / `go vet` / `go test -race -count=1 ./...`（全绿才提交）
与 `go mod tidy -diff`（无漂移）⇒ 改动**只影响依赖版本**，零行为差异。
只改 `go.mod` / `go.sum` ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。
Co-authored-by: Murphy-Yi <zhangyi@Murphy-YideMac-Mini.local>

- 杂务(deps)：依赖地板追到最新——github.com/oagentkit/textx v0.7.2

| 依赖 | 现地板 | 抬到 |
|---|---|---|
| `github.com/oagentkit/textx` | `v0.7.1` | `v0.7.2` |
| `github.com/oekit/concurrency` | `v0.23.2` | `v0.24.0` |
| `github.com/oekit/logx` | `v0.8.0` | `v0.8.2` |
| `github.com/oekit/pkg` | `v0.31.0` | `v0.34.0` |
## 为什么
矩阵的**发版时刻地板时效**门禁（`verify_tag_floor --ref HEAD`）要求：各仓 HEAD
（= 下次发版会发出去的内容）的依赖地板，相对那一刻的依赖最新版本，不能已落后。
oekit 基座与矩阵模块都在高频发版 ⇒ 地板是**跑步机**，落后一格就会判红。
## 兼容性实测
本工具在提交前跑完 `go build` / `go vet` / `go test -race -count=1 ./...`（全绿才提交）
与 `go mod tidy -diff`（无漂移）⇒ 改动**只影响依赖版本**，零行为差异。
只改 `go.mod` / `go.sum` ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。
Co-authored-by: Murphy-Yi <zhangyi@Murphy-YideMac-Mini.local>

- 杂务(deps)：依赖地板追到最新——github.com/oekit/concurrency v0.23.2

| 依赖 | 现地板 | 抬到 |
|---|---|---|
| `github.com/oekit/concurrency` | `v0.23.1` | `v0.23.2` |
## 为什么
矩阵的**发版时刻地板时效**门禁（`verify_tag_floor --ref HEAD`）要求：各仓 HEAD
（= 下次发版会发出去的内容）的依赖地板，相对那一刻的依赖最新版本，不能已落后。
oekit 基座与矩阵模块都在高频发版 ⇒ 地板是**跑步机**，落后一格就会判红。
## 兼容性实测
本工具在提交前跑完 `go build` / `go vet` / `go test -race -count=1 ./...`（全绿才提交）
与 `go mod tidy -diff`（无漂移）⇒ 改动**只影响依赖版本**，零行为差异。
只改 `go.mod` / `go.sum` ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。

- 杂务(deps)：依赖地板追到最新——github.com/oekit/concurrency v0.23.1

oekit 基座又发了一版（`concurrency` v0.23.1，2026-10-29 02:19 CST）⇒ 上一轮抬到
v0.23.0 的地板已经落后 ⇒ 再抬一格。
本仓只用到 `concurrency` 的少量 API ⇒ 实测 go build / go vet / go test -race 全绿，
**零行为差异**；go 指令未被顶起。
只改 go.mod / go.sum ⇒ 不需要 CHANGELOG 条目（版本段由下次发版的 `prewrite` 生成）。

- 杂务(deps)：依赖地板追到最新——github.com/oekit/concurrency v0.23.0 github.com/oekit/logx v0.8.0 github.com/oekit/pkg v0.31.0

oekit 基座发了新版本（pkg v0.31.0 / concurrency v0.23.0 / logx v0.8.0）⇒ 本仓的
依赖地板落后 ⇒ 抬到最新，让**下一次发版**不再带出落后地板（打 tag 时刻的地板
必须是当时的最新——`verify_tag_floor` / `release.py doctor` 都在查这条）。
兼容性实测：本仓只用这些基座包的少量 API ⇒ go build / go vet / go test -race
全绿，**零行为差异**；go 指令未被顶起（本仓的 go 地板不动）。

- 杂务(deps)：依赖地板追平上游本轮新版本——textx v0.7.1

本仓 HEAD 的依赖地板已落后于 textx 的最新版本（`verify_tag_floor.py --org oagentkit --ref HEAD` 判红）。
本仓一旦发版，就会把落后地板发出去——而 tag 受 protect-tags ruleset 保护、打完不能重打，
只能靠后续版本补齐。
本版只抬依赖地板，不改任何行为：
- github.com/oagentkit/textx v0.6.2 → v0.7.1
对应 issue matrix-tools#143。

- 杂务(deps)：依赖地板追平上游本轮新版本——github.com/oekit/concurrency@v0.22.0、github.com/oekit/pkg@v0.28.0

本仓 HEAD 的依赖地板已落后于各依赖的最新版本（`verify_tag_floor.py --org oagentkit --ref HEAD` 判红）。
本仓一旦发版，就会把落后地板发出去——而 tag 受 protect-tags ruleset 保护、打完不能重打，
只能靠后续版本补齐。
本版只抬依赖地板，不改任何行为：
- github.com/oekit/concurrency → v0.22.0
- github.com/oekit/pkg → v0.28.0
对应 issue matrix-tools#143。


- 文档(rag)：刷正 README.md 的层位声明——tier 1 → tier 2

## 现状
本仓 `README.md` 的两处层位声明写的是**层位重定位之前**的旧层位（tier 1），
而矩阵单一数据源 `matrix-tools/modules.json` 说本仓是 **tier 2（集成层）**。
层位重定位的依据见 matrix-tools 提交 `6d800c4`「层位 = 依赖地板」——
tier 2 的定义是「依赖 tier0/tier1 + 重第三方地板」（`modules.json` 的 `tiers` 映射）。
| 位置 | 改前 | 改后 |
|---|---|---|
| `README.md` 第 3 行（定位声明行） | `**tier 1**` | `**tier 2**` |
| `README.md` 第 15 行（`## 定位` 自述行） | `**tier 1**` | `**tier 2**` |
- `modules.json`：`rag` 的 `tier` = **2**，`tiers["2"]` = 集成层
- 本仓 `AGENTS.md` 第 5 行：**已是** `> 矩阵分层：**tier 2**（集成层，依赖 tier0/tier1 + 重第三方地板）`
  ⇒ 改之前，本仓的**两个第一入口互相矛盾**（`AGENTS.md` 说 tier 2、`README.md` 说 tier 1）
## 影响
`README.md` 是仓级的第一入口（人读 + agent 读）。它写 tier 1 而数据源是 tier 2 ⇒
读者按 README 判断依赖方向就会算错，而分层是矩阵的**硬不变量**（tier N 不得依赖 tier > N）。
## 改动
**只改层位数字与层名，其余段落逐字不动**：
- 第 3 行：`**tier 1**` → `**tier 2**`，层名 `（能力层，依赖 tier0）` → `（集成层，依赖 tier0/tier1 + 重第三方地板）`
- `## 定位`：`**tier 1**` → `**tier 2**`，层名 `能力层` → `集成层`
层名与整段括号说明对齐到 tier 2 的权威表述后，与本仓 `AGENTS.md`
及 `llm` / `toolgate` / `tools` / `agent` 四个 tier 2 仓的 README 逐字一致。
diff 只含层位行，`README.md` 的手写段落逐字未动。
## 验收
- [x] `python3 matrix-tools/scripts/verify_repo_tiers.py --root .` ⇒ 本仓 README **红 0**（本地实跑，26 处声明全一致）
- [x] 层位数字与层名与 `modules.json` 一致，且与本仓 `AGENTS.md` 一致
- [x] **纯 .md 改动** ⇒ 不需要 CHANGELOG 条目、不打 tag、不发版（无发版步骤）
关联 oagentkit/matrix-tools#123

- 文档(rag)：刷正 AGENTS.md 层位声明——tier 1 → tier 2

AGENTS.md 的层位声明与矩阵单一数据源 modules.json 不一致。
- 矩阵分层：**tier 1**（能力层，依赖 tier0）→ **tier 2**（集成层，依赖 tier0/tier1 + 重第三方地板）
- 「本仓是 tier 1，只允许依赖 tier ≤ 1 的仓」→ tier 2
modules.json 里 rag 的 tier = 2（层名「集成层」；层位 t1 → t2 时带 eino + milvus-sdk
地板，向量化/混合召回本身绑 SDK）。层位写错会让人按 tier 1 的口径判依赖方向，
而分层是矩阵的硬不变量。
属 matrix-tools#109 的仓级层位漂移；同批修 queue / obs / skillkit / mcpkit。
diff 只含层位行，AGENTS.md 的手写段落逐字未动。


## [v0.8.5] — 2026-10-21

- 杂务(deps)：依赖地板跟到矩阵本轮新版本——textx v0.6.2

## 下游要做什么
**`go mod tidy`**。地板抬高对下游是**隐式破坏性变更**：不跑就报
`updates to go.mod needed`，且报错不提真正原因（**不是本仓的 API 变了**）。
本仓**自身代码零改动**——只看本仓的 diff 看不出这是一次地板抬高。
## 为什么抬
上一版 tag 打出去时，它依赖的模块的**新版已经先发出**了——
即「打 tag 那一刻地板就落后」。矩阵的「发版时刻地板时效」门禁
（`verify_tag_floor.py --org oagentkit --latest-only`）当场报红：
- github.com/oagentkit/textx@v0.6.2
门禁给的修法是「抬 go.mod 地板 → go mod tidy → 重新发 tag」。
本 PR 就是那条修法。
## 为什么无行为差异
被抬的这几个版本**自身也都是纯依赖地板追平**（各仓代码零改动，只抬
`oekit/pkg` / `oekit/concurrency` / `guardx` / `llm` 四类地板之一），
本仓用到的转发面与符号集一字未动。
## 门禁
```
go build / vet / test -race   ✅
gofmt -l .                    ✅ 空
```


## [v0.8.4] — 2026-10-21

- 杂务(deps)：依赖地板追平上游本轮新版本——oekit/concurrency v0.19.0 oekit/pkg v0.20.0

## 下游要做什么
**`go mod tidy`**。地板抬高对下游是隐式 breaking：不跑就报
`updates to go.mod needed`，且报错不提真正原因（**不是本仓的 API 变了**）。
本仓**自身代码零改动**——只看本仓的 diff 看不出这是一次地板抬高。
## 抬了哪几块、为什么无行为差异
**`github.com/oekit/concurrency` v0.18.0 → v0.19.0**
上游 diff 只动了 2 个包：
- `retry`：**抖动 panic 修复**——`rand.Int63n` 的契约是 n > 0，而「退避幅度为 0」
  是生产可达路径（cfg.Delay=0 / DelayHint 返回 0 / float64→int64 溢出成负数），
  旧实现在这些路径上直接 panic。改为钳到 [0, MaxInt64]。
- `syncx`：新增全局在飞闸门原语（`Gate`）+ 说明同步。
**`breaker` 与 `async` 两个包一字未动。**
**本仓实际 import 的 `oekit/concurrency` 子包：async syncx **
与上游改动相交的：**syncx**
**`github.com/oekit/pkg` v0.18.0 → v0.20.0**
上游 diff 只动了 5 个包：
- `errors`：**纯增量**——新增 `Errorf`（格式化 + `%w` 包装，nil-safe）。
  `New`/`As`/`Is`/`Unwrap`/`Wrap` 是同型透传，一字未动。
- `httpjson`：**退避溢出修复**——旧写法 `base << (attempt-1)` 在 attempt 较大时
  int64 溢出成负数，负数绕过 `MaxDelay` 封顶，`time.After(负数)` 立刻返回
  ⇒ **该轮退避彻底消失**。改为乘法 + 饱和封顶。正常 attempt 数（1..20）下
  结果**逐字相同**，差别只在溢出的病态路径。
- `urlguard`：**纯增量**——新增 `WithExactHosts`，未设置时行为与既有完全一致。
- `stringx`：**仅注释**（补 `TruncEllipsis` 的 n 语义说明）。
- `pathx`：新增包（`SanitizeRel` 等）。
**本仓实际 import 的 `oekit/pkg` 子包：httpjson **
与上游改动相交的：**httpjson**
## 门禁
```
go build / vet / test -race   ✅
gofmt -l .                    ✅ 空
```


- 文档(AGENTS)：补「发版顺序」小节，指向 org CONTRIBUTING 单一副本

13 个业务仓的 AGENTS.md 此前对发版顺序零覆盖（只有 textx 有个
「### 版本与发版」标题，但里面只讲 SemVer 与 module 路径），
发版顺序只写在 org 规则仓的 .github/CONTRIBUTING.md 里，单仓读者看不到。
本次只加**指针 + 本仓相关提醒**，runbook 本体保持**单一副本**
（CONTRIBUTING §发版），避免 13 份副本各自漂移。
覆盖四条：
- CHANGELOG.md 由 matrix-tools/scripts/gen_changelogs.py 从 git 历史生成，不要手改
- tag 必须落在 CHANGELOG 提交之后（下游 go get 拿到的是 tag 快照）
- 警告：无参重生成会吞掉待发版段，--next-version 是唯一能产出待发版段的开关
- 本仓相关提醒（见正文）


## [v0.8.3] — 2026-10-20

### 新增(embedder)：OpenAIEmbedder 补注入缝——client / 超时 / 重试可配

承接 oagentkit/rag#15。`NewOpenAIEmbedder` 原本是四个位置参数的构造函数，
**没有任何注入缝**：HTTP client、超时、重试策略、出站 transport 全部由包内写死
（`client: &http.Client{Timeout: 30 * time.Second}`），且 `embedBatch` 走的是
**不带重试的** `httpjson.DoJSON`。
缺口有三层：
1. **无法注入 transport / client**——下游要在 embedding 出站请求上挂自己的
   出站守卫（SSRF 策略、mTLS、代理、连接池调参），没有入口（client 是未导出字段）。
2. **超时不可配**——30s 写死；下游既不能调短（快速失败）也不能调长。
3. **重试能力存在但接不上**——`oekit/pkg/httpjson` 已导出 `DoJSONWithRetry`
   与 `RetryConfig`（默认判定：网络超时或 HTTP 5xx/429 可重试），但这条路径
   在 embedding 上拿不到。
错误面本就是结构化可分类的（`httpjson.StatusError{Code, Body}` + `%w` 包装，
下游 `errors.As` 能拿到状态码），缺的只是「让下游决定用不用」的那条缝。
## 改动
- 新增 `EmbedderOption`（**不裸 `Option`**——多包 co-import 会冲突）+ 三个选项：
  `WithHTTPClient` / `WithTimeout` / `WithRetry`。
- `NewOpenAIEmbedder` 改为变参（`opts ...EmbedderOption`），**旧 4 参调用点
  全部无需改动即可编译**（仓内 7 处调用点已验证）。
- 有 `WithRetry` 时走 `httpjson.DoJSONWithRetry`，否则保持原来的单次
  `httpjson.DoJSON`——**默认行为与引入本选项前完全一致**（单次尝试、30s 超时）。
## 交互规则（写进 godoc，且与选项传入顺序无关）
- 生效 client：给了 `WithHTTPClient` 就用它（**取副本**，不改调用方持有的对象
  ——就地改 Timeout 会污染共享 client 的其它使用者），否则用默认的 30s client。
- `WithTimeout` 覆盖生效 client 自身的 Timeout（**timeout 优先**）。
- 只给 `WithHTTPClient` 时，完全沿用注入 client 自身的 Timeout（0 = 无超时）。
- `WithHTTPClient(nil)` / `WithTimeout(d<=0)` 视为未设置；`nil` option 跳过。
- 零值 `RetryConfig{}` 等价单次尝试（httpjson 把 `MaxAttempts < 1` 视为 1）。
## 验证
新增 12 个测试（`embedder_option_test.go`），覆盖：注入的 client 确实被用上
（自定义 RoundTripper 计数）、timeout 生效、5xx/429 重试后成功、默认路径行为
与之前完全一致、错误链仍保留 `StatusError.Code` 等。
```
$ go build ./... && go vet ./... && gofmt -l .
$ go test -count=1 ./...        ok  github.com/oagentkit/rag
$ go test -race -count=1 ./...  ok  github.com/oagentkit/rag
```
测试只打 httptest 本地服务器，无真实网络。

- 杂务(deps)：依赖地板抬到最新

地板对齐到发版时刻的各依赖最新 tag：
- oekit/pkg、oekit/concurrency 抬到 v0.18.0（oekit/logx 已是最新 v0.7.1）
- 矩阵内跨仓依赖抬到各仓最新 tag
依据 matrix-tools#44：最新 tag 的依赖地板在「打 tag 那一刻」就已落后一格，
只能靠后续版本补齐（tag 受 protect-tags ruleset 保护不能重打）。


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

matrix-tools 的 verify_dep_floors.py 首次全矩阵扫描查出的地板漂移，本仓是受影响仓之一。
本轮按 tier 拓扑顺序推进：tier0（textx / runtime / guardx / queue）先发，
本仓再把地板跟到它们的新 tag——否则就是「发了新的、旧的立刻变落后」，永远收敛不了。
所升版本与原钉版本相比源码零改动，逐版实测确认：
- textx v0.4.0→v0.5.0 / runtime v0.6.0→v0.7.0 / guardx v0.4.1→v0.5.0
  全树逐字节相同（diff -rq 退出码 0）
- oekit/pkg v0.14.0→v0.15.1  仅新增 urlguard 包（纯标准库，pkg 的 go.mod
  连 require 块都没有，零新增三方依赖）
- oekit/concurrency v0.17.0→v0.17.1  仅三个文档文件
- oekit/logx v0.7.0→v0.7.1  仅 CI 配置与文档
无 API 变更、无兼容风险。
本地门禁：go mod tidy -diff 无输出 / go build / go vet / gofmt / go test -race 全通过。
关联 oagentkit/matrix-tools#13


## [v0.7.0] — 2026-10-19

- 杂务(deps)：依赖地板对齐远端最新（textx v0.4.0 / concurrency v0.17.1 / logx v0.7.1 / pkg v0.15.1）

matrix-tools 的 verify_dep_floors.py 首次全矩阵扫描查出：43 条第一方依赖里
28 条落后，横跨 11 个仓；本仓是其中之一。
所升版本与本仓原钉版本相比源码零改动，逐版实测确认：
- textx v0.3.0→v0.4.0 / obs v0.4.1→v0.5.0 / runtime v0.5.0→v0.6.0 /
  mcpkit v0.5.1→v0.6.0  全树逐字节相同（diff -rq 退出码 0）
- oekit/pkg v0.14.0→v0.15.1  仅新增 urlguard 包（纯标准库，pkg 的 go.mod
  连 require 块都没有，零新增三方依赖）
- oekit/concurrency v0.17.0→v0.17.1  仅三个文档文件
- oekit/logx v0.7.0→v0.7.1  仅 CI 配置与文档
无 API 变更、无兼容风险。
本地门禁：go mod tidy -diff 无输出 / go build / go vet / gofmt / go test -race 全通过。


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

本 PR 含两项改动。
## 1. oekit 依赖地板
姊妹矩阵 oekit 的 `concurrency` 出了 v0.17.0，依赖地板一并对齐。
升级前已核对 `oekit/concurrency` v0.16.0...v0.17.0 的全部 2 个提交：
- `feat(ratelimit): KeyedLimiter.Create 护栏内原子登记 (Fixes #33)` — `ratelimit/keyed.go` +28 行
- `docs: 矩阵总览表 DESC 口径同步` — 仅 AGENTS.md / README.md / CHANGELOG.md
无 breaking / remove 标记，未触及任何已有导出签名，故为纯补丁式升级。
## 2. 迁出被 force 移动过的 tag
本仓原先依赖的姊妹仓 tag（发布后被强推到了重写后的 main）内容已与
sum.golang.org 记录不一致，于是任何走 `proxy.golang.org` 或 `GOPROXY=direct`
的构建都会 SECURITY ERROR。本地往往不报错——模块缓存预热 + goproxy.cn
还留着移动前的字节，把问题掩盖了。
已为 8 个模块重打不可变的**附注 tag**（内容与被移动 tag 指向同一 commit，
只换版本号，让校验和数据库重新记录一次正确哈希），并把依赖迁过去。
`go.sum` 的 h1 全部取自 sum.golang.org，仅就地替换目标模块的两行
（zip h1 + go.mod h1），其余行不动。


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

本轮做了版本线重置：清空全部历史 tag，按「历史最新 + 0.1.0」重打新版本
（如 llm v0.6.0 → v0.7.0、toolgate v0.6.1 → v0.7.0、agentkit v1.3.0 → v1.4.0）。
各仓 go.mod 的矩阵内依赖地板同步跟进到新版本号。
**go.sum 的 oagentkit 行已整段删除**，由 CI 的 `go mod tidy` 重算：
新 tag 的内容与任何历史版本都不同，`h1` 必须现算；本地没有 Go 工具链，
且 `/go.mod` 的 dirhash 无法自行复现（zip hash 可复现，gomod hash 不可复现）。
重算依赖 `.github` 的 go-ci 配了 `GOPRIVATE=github.com/oagentkit/*` 直连 VCS：
goproxy.cn 缓存着旧版本，若走 proxy 会拉回**旧代码**并算出旧 hash，
门禁全绿但依赖是错的。
（第二个提交会补上重算好的 go.sum。）
* chore(deps): 依赖地板升到「历史最新 + 0.1.0」
chore(deps): 依赖地板升到「历史最新 + 0.1.0」
本轮做了版本线重置：清空全部历史 tag，按「历史最新 + 0.1.0」重打新版本
（如 llm v0.6.0 → v0.7.0、toolgate v0.6.1 → v0.7.0、agentkit v1.3.0 → v1.4.0）。
各仓 go.mod 的矩阵内依赖地板同步跟进到新版本号。
**go.sum 的 oagentkit 行已整段删除**，由 CI 的 `go mod tidy` 重算：
新 tag 的内容与任何历史版本都不同，`h1` 必须现算；本地没有 Go 工具链，
且 `/go.mod` 的 dirhash 无法自行复现（zip hash 可复现，gomod hash 不可复现）。
重算依赖 `.github` 的 go-ci 配了 `GOPRIVATE=github.com/oagentkit/*` 直连 VCS：
goproxy.cn 缓存着旧版本，若走 proxy 会拉回**旧代码**并算出旧 hash，
门禁全绿但依赖是错的。
（第二个提交会补上重算好的 go.sum。）
* chore(deps): 回填 CI 重算的 go.sum
`h1` 由 CI 里真实的 `go mod tidy` 算出后回填——本地没有 Go 工具链，
且 `/go.mod` 的 dirhash 无法自行复现（zip hash 可复现，gomod hash 不可复现），
所以这是唯一权威来源。
重算依赖 `.github` go-ci 里的 `GOPRIVATE=github.com/oagentkit/*` 直连 VCS：
goproxy.cn 仍缓存着版本线重置前的旧内容，走 proxy 会拉回**旧代码**并算出旧 hash，
门禁全绿但依赖是错的。
验证方式就是本 PR 的 `go mod tidy -diff`：若回填有任何偏差，它会照样报差异。
* chore(deps): 回填 CI 重算的 go.sum
`h1` 由 CI 里真实的 `go mod tidy` 算出后回填——本地没有 Go 工具链，
且 `/go.mod` 的 dirhash 无法自行复现（zip hash 可复现，gomod hash 不可复现），
所以这是唯一权威来源。
重算依赖 `.github` go-ci 里的 `GOPRIVATE=github.com/oagentkit/*` 直连 VCS：
goproxy.cn 仍缓存着版本线重置前的旧内容，走 proxy 会拉回**旧代码**并算出旧 hash，
门禁全绿但依赖是错的。
验证方式就是本 PR 的 `go mod tidy -diff`：若回填有任何偏差，它会照样报差异。
* chore(deps): 回填 CI 重算的 go.sum
`h1` 由 CI 里真实的 `go mod tidy` 算出后回填——本地没有 Go 工具链，
且 `/go.mod` 的 dirhash 无法自行复现（zip hash 可复现，gomod hash 不可复现），
所以这是唯一权威来源。
重算依赖 `.github` go-ci 里的 `GOPRIVATE=github.com/oagentkit/*` 直连 VCS：
goproxy.cn 仍缓存着版本线重置前的旧内容，走 proxy 会拉回**旧代码**并算出旧 hash，
门禁全绿但依赖是错的。
验证方式就是本 PR 的 `go mod tidy -diff`：若回填有任何偏差，它会照样报差异。

- 杂务(deps)：跟进最新 tag

oagentkit/textx v0.1.0→v0.2.0
go.sum 取 sumdb 权威 h1，仅就地替换目标 mod 两行。

- 杂务(deps)：oekit 依赖升级

concurrency v0.13.1→v0.16.0、pkg v0.13.0→v0.14.0
姊妹矩阵 oekit 的依赖地板此前被漏掉（上一轮只扫了 oagentkit 内部依赖），本次一并对齐到最新。
升级前已确认 oekit/concurrency v0.13.1..v0.16.0 的 8 个提交全是 feat/docs/chore，
无 breaking/remove 标记。其中 #28 订正了 `WithCooldown(0)` 的语义——
即 llms.txt 里警告过的那个陷阱，升上去正好拿到修正后的行为。
go.sum 的 h1 取自 sumdb，仅就地替换目标 mod 的两行、其余行不动。

- 杂务(deps)：oekit/concurrency v0.12.0 -> v0.13.1

本仓不使用上游 breaker（熔断只在 llm 仓），v0.13.0 的 #25 修复与
v0.13.1 的 #26（RetryableWithHeaders 头值 OWS 修剪）对本仓无直接影响，
本次为矩阵内版本对齐。
门禁：go mod tidy -diff / build / vet /
test -race / gofmt 全绿。

- 杂务(deps)：oekit 三模块升至最新（pkg v0.13.0 / concurrency v0.12.0 / logx v0.7.0）

拿到上游新增能力：
- concurrency #22 RetryableWithHeaders：403 头感知判定（次级限流 / 主限流耗尽）
- concurrency #23 FanInLimit：受限并发扇出
- concurrency #24 WithTokenClock、#21 Lockout（keyed lockout/penalty）
- logx #12 TraceHandler：给裸 slog.Handler 注入 trace_id
破坏性变更核对（均无影响）：
- logx v0.7.0 ParseLogLevel 签名改为 (Level, error) —— 我们未使用该函数
- concurrency v0.9.0 的 Slot.Panicked 废弃 —— rag 已迁到 Slot.Err + errors.As
门禁：go build / go vet / go test -race 全绿。

- 杂务(deps)：oekit/concurrency v0.6.0 -> v0.9.0

拿到上游显式三态状态机（#17）与 singleflight 可观测折叠（#18），并为本次
breaker 替换评估了语义差异。
用到的子包只有 async / singleflight / syncx —— 上游 breaker 包我们并未引用
（runtime/breaker 是本仓自己的三态实现），所以本次升级零 breaking。
关于 breaker 替换的现状：#17 已落地（closed/open/half-open + 代绑定 + 探测时限 +
开放期失败不延长冷却），但两侧的触发语义仍不对齐，已在上游补两个需求：
- #19 冷却期与探测失联时限共用 probeTimeout，无法独立配置
- #20 缺「连续失败 N 次即熔断」（trip）触发
因此本包暂不删除 —— llm.TestResilientBreakerTrips 钉的是 trip 语义，
上游 v0.9.0 只有速率型触发（dropRatio > 0），现在换会让该测试失败。


- CI：接入 org 共享门禁（规则本体在 oagentkit/.github）
