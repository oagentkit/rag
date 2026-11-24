# rag

知识检索——向量化/混合召回/重排，向量后端可插拔 —— oagentkit 模块矩阵 **tier 2**（集成层，依赖 tier0/tier1 + 重第三方地板）。

```
go get github.com/oagentkit/rag
```

| 包 | 职责 |
|---|---|
| `rag` | 知识检索：向量化接口（`Embedder`）、本地索引（TF-IDF）、混合召回与重排；**不链向量 sdk** |
| `rag/milvus` | Milvus 向量后端（**唯一**链接 milvus sdk 的包）：`milvus.NewStore(ctx, cfg, embedder)` |
| `rag/wiki` | 持久 wiki 写侧：页面 upsert/删除/乐观版本、增量摄取（`Ingest`）、结构检查（`Lint`） |
| `rag/wiki/fsstore` | wiki 的文件系统参考实现（markdown + YAML front-matter，零外部服务） |

> **向量后端在子包**：只用本地索引/混合召回的调用方 `import github.com/oagentkit/rag`
> **不会**把 milvus sdk 及其传递依赖（cockroachdb/errors、sentry-go、gogo/protobuf 等）
> 拖进二进制；要用向量后端才 `import github.com/oagentkit/rag/milvus`。
> 这条性质由 `make dep-guard`（`go list -deps` 判据）机器守住。

## 定位

**tier 2** — 集成层，依赖 tier0/tier1 + 重第三方地板。

- 本仓依赖的矩阵内仓：`textx`。

## 外部依赖地板

- `github.com/cloudwego/eino`
- `github.com/milvus-io/milvus-sdk-go/v2`（**仅 `rag/milvus` 子包**：根包不链，见上）

## 与 oekit 的关系

本矩阵（oagentkit，Agent/LLM 编排工具链）**依赖** oekit（Open Easy Kit，Go 微服务工具链）：
`oekit/pkg` / `oekit/concurrency` / `oekit/logx` 等。oekit 是另一套独立矩阵，不是子品牌。

## 开发

```bash
go test ./... -race
go vet ./...
gofmt -l .
```

规范见 [AGENTS.md](AGENTS.md)。

## License

MIT
- **单路召回失败不得降级为 `(nil, nil)`。** 混合召回中任一路 panic 或失败
  要作为该路失败参与裁决；全部路失败时**返回 panic 错误而非静默空结果**。
- 向量后端可插拔，新增后端不得改 `Embedder`/`KnowledgeService` 接口形状 —— 那会让所有后端同时 breaking。