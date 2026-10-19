# AGENTS.md — oagentkit/rag

> 知识检索——向量化/混合召回/重排，向量后端可插拔
> module：`github.com/oagentkit/rag` · org：[oagentkit = Open Agent Kit](https://github.com/oagentkit)
> 矩阵分层：**tier 1**（能力层，依赖 tier0）

## 本仓包含

```
`/`（module 根，包名 rag → import `github.com/oagentkit/rag`）
```

> **包为什么住在 module 根（不要再挪回子目录）**
>
> 矩阵的 import 路径规则是 **`github.com/{org}/{仓名}/{包名末段}`**，中间层目录由**仓**承担、
> 一律丢弃。唯一的例外是**包名末段 == 仓名**：此时包直接落在 module 根，路径不重复末段。
>
> 本仓正是这一例——包名 `rag` 恰好等于仓名 `rag`，所以 import 是 `github.com/oagentkit/rag`
> 而不是 `github.com/oagentkit/rag/rag`。Go 的惯例本就是包名不重复 import 路径末段。
>
> 改布局前先跑 `python3 matrix-tools/scripts/rewrite_imports.py --print-rules` 对权威映射表。

## 硬规则

### 分层与依赖

- 依赖严格自底向上，**禁止反向依赖**。本仓是 tier 1，只允许依赖 tier ≤ 1 的仓。
- 本仓依赖的矩阵内仓：`textx`。
- 改 module 路径必须同步：全部 import、go.mod、README 安装行。

### 错误处理

- 一律使用 `github.com/oekit/pkg/errors`；业务错误用 BizError。
- 所有 error 禁止 `_` 忽略。

### 并发安全

- 禁止裸 `go func`：用 `github.com/oekit/concurrency/async` 的 GoSafe 系列（panic 被 recover）。
- 共享状态用 mutex / atomic 保护；包级全局可变状态用 `atomic.Pointer[T]`。

### API 设计

- 函数选项模式命名 `XxxOption`（禁止裸 `Option`，多包 co-import 会冲突）。
- 导出符号 PascalCase + godoc 注释；包文档放 `doc.go`。

### 日志

- **本仓不引入日志依赖。** tier0 加日志等于给所有下游抬地板；需要观测输出时返回 error 或交给调用方。

## 仓特有纪律

- **单路召回失败不得降级为 `(nil, nil)`。** 混合召回中任一路 panic 或失败
  要作为该路失败参与裁决；全部路失败时**返回 panic 错误而非静默空结果**。
- 向量后端可插拔，新增后端不得改 `Embedder`/`KnowledgeService` 接口形状 —— 那会让所有后端同时 breaking。

## 明确不属于本仓的

| 能力 | 归属 |
|---|---|
| 文本/差异/会话/报告 | `github.com/oagentkit/textx` |
| 事件总线/流控/子进程/热插拔/钩子 | `github.com/oagentkit/runtime` |
| 脱敏/出口围栏/权限/沙箱解析/审计 | `github.com/oagentkit/guardx` |
| DB 队列与选主 | `github.com/oagentkit/queue` |
| 通用 errors/stringx/encoding/mathx | `github.com/oekit/pkg` |
| goroutine 池/单飞/重试/令牌桶 | `github.com/oekit/concurrency` |
