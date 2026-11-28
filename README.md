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
| `rag/ontology` | 实体/关系本体层：类型声明（`Schema`）、抽取结果契约（`Entity`/`Triple`）、判据（`Validate`）、最小查询面（`Store` + 内存参考实现） |
| `rag/embedtest` | Embedder 合规契约套件（conformance）：分级口径两档声明 + 批量/单条一致 + 维度/边界/错误不静默，换实现同套测试全绿 |

> **向量后端在子包**：只用本地索引/混合召回的调用方 `import github.com/oagentkit/rag`
> **不会**把 milvus sdk 及其传递依赖（cockroachdb/errors、sentry-go、gogo/protobuf 等）
> 拖进二进制；要用向量后端才 `import github.com/oagentkit/rag/milvus`。
> 这条性质由 `make dep-guard`（`go list -deps` 判据）机器守住。

## 如何验证你的 Embedder 合规

三行接入（`import "github.com/oagentkit/rag/embedtest"`；`New` 出被测实现、
`NewFailing` 出一个必败实现、`Guarantee` 二选一声明分级口径——确定性
`GuaranteeDeterministic` 或余弦排序一致 `GuaranteeCosineRank`）：

```go
func TestMyEmbedder(t *testing.T) {
	embedtest.Run(t, embedtest.Contract{New: func() rag.Embedder { return my }, Guarantee: embedtest.GuaranteeDeterministic, NewFailing: func() rag.Embedder { return broken }})
}
```

## 定位

**tier 2** — 集成层，依赖 tier0/tier1 + 重第三方地板。

- 本仓依赖的矩阵内仓：`textx`。

## 本体层（`rag/ontology`）

实体/关系本体层的**契约与判据**——只回答三个互相独立的问题：某领域有哪些实体类型
（`Schema`）、一条断言长什么样（`Entity`/`Triple`）、这批断言是否符合声明
（`Validate`）以及怎么查（`Store`）。

模型一句话：**实体是节点，三元组是节点的字段取值**——谓词是主体类型的字段名，
客体是该字段的取值（`ref:<Type>` 字段的客体是另一个实体的名字）。

### 不是什么（三条刻意边界）

1. **不做抽取**：LLM 抽取提示词与领域强相关，留在宿主；本包只定义**契约**
   （`Schema`/`Entity`/`Triple`/`Validate`）与查询面 ⇒「抽取实现可换、契约不变」。
2. **不建图数据库**：`Store` 是接口 + 内存参考实现（`MemoryStore`）；
   PG/Neo4j 由宿主接自己的实现。
3. **不引入 wiki 依赖**：wiki 页是「叙事」，ontology 是「结构」，两者互不依赖。

另外两条边界（本包**不做**的事，写明以免误用）：**不做实体名规范化**
（大小写折叠/别名归一/环境前缀剥离属领域资产，留在宿主）；**不假设实体登记完备**
（主体或引用目标未登记时跳过依赖类型的检查）。

### 与 wiki 的分工

- wiki = 叙事面：页面正文、front-matter、链接关系，产出给人读；
- ontology = 结构面：实体、类型、字段、关系，产出给判据与查询用；
- 组合方式：wiki 页的 front-matter 引用实体 slug，实体页由 wiki 侧聚合；
  宿主按 slug 反查页面时，用 `Store.Neighbors` 拿结构面、用 wiki 读叙事面。

### 宿主怎么用

```go
schema := ontology.Schema{
    Types: []ontology.Type{
        {
            Name: "service",
            Fields: []ontology.Field{
                {Name: "owner", Type: ontology.FieldTypeString, Required: true},
                {Name: "depends_on", Type: ontology.RefType("component")},
            },
        },
        {
            Name:   "component",
            Fields: []ontology.Field{{Name: "label", Type: ontology.FieldTypeString, Required: true}},
        },
    },
}

// 抽取实现留在宿主；产出的是契约面（没有证据的三元组不收）。
triples := []ontology.Triple{
    {Subject: "svc-a", Predicate: "owner", Object: "team-x", Evidence: "chunk-1", Confidence: 0.9},
}

// 判据是纯函数：同一批断言可离线复算。结论逐条带类别与说明。
for _, f := range ontology.Validate(schema, entities, triples) {
    log.Printf("%s: %s", f.Kind, f.Message)
}

// 一对多默认报「矛盾候选」；领域允许时显式关掉。
findings := ontology.Validate(schema, entities, triples, ontology.AllowMultiObject())

// 取数面：内存参考实现；生产接 PG/Neo4j 自己的 ontology.Store 实现。
store := ontology.NewMemoryStore()
_ = store.Upsert(ctx, triples)
neighbors, _ := store.Neighbors(ctx, "svc-a", 2) // 确定顺序，便于复现
```

判据类别（`FindingKind`）：`undeclared_type` / `undeclared_predicate` /
`missing_required_field` / `invalid_value` / `duplicate_triple` /
`missing_evidence` / `conflicting_objects` / `incomplete` / `invalid_confidence`。

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