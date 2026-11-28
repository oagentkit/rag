// Package ontology 是实体/关系本体层的**契约与判据**面。
//
// 本包只回答三个互相独立的问题：
//
//  1. 声明面：某领域有哪些实体类型、每个类型有哪些字段（Schema/Type/Field）；
//  2. 抽取结果面：一条断言长什么样（Entity/Triple）；
//  3. 判据面：这批断言是否符合声明（Validate），以及怎么查（Store）。
//
// 模型一句话：**实体是节点，三元组是节点的字段取值**——谓词是主体类型的字段名，
// 客体是该字段的取值（ref:<Type> 字段的客体是另一个实体的名字）。
//
// 刻意边界（oagentkit/rag#145）：
//
//   - 不做抽取：LLM 抽取提示词与领域强相关，留在宿主；本包只定义契约，
//     「抽取实现可换、契约不变」；
//   - 不建图数据库：Store 是接口 + 内存参考实现，PG/Neo4j 由宿主接；
//   - 与 wiki 分工：wiki 页是「叙事」，ontology 是「结构」；本包**不依赖 wiki**
//     （wiki 页的 front-matter 可引用实体 slug，实体页由 wiki 侧聚合）。
//
// 依赖面：标准库 + github.com/oekit/pkg/errors，无新增第三方依赖。
//
// 生命周期: Experimental——结论分类与 Validate 判据面 v0.13.0 刚立，判据刻度
// 会随领域接入反馈调整，未过 api-compat.md 晋级三维，初值如实记为不承诺兼容。
package ontology
