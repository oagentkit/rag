# rag

知识检索——向量化/混合召回/重排，向量后端可插拔 —— oagentkit 模块矩阵 **tier 1**（能力层，依赖 tier0）。

```
go get github.com/oagentkit/rag
```

| 包 | 职责 |
|---|---|
| `rag` | 知识检索：向量化接口（`Embedder`）、向量后端可插拔、混合召回与重排 |

## 定位

**tier 1** — 能力层，依赖 tier0。

- 本仓依赖的矩阵内仓：`textx`。

## 外部依赖地板

- `github.com/cloudwego/eino`
- `github.com/milvus-io/milvus-sdk-go/v2`

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