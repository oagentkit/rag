// Package wiki 持久 wiki 的写入面：页面 upsert/删除/乐观版本，以及增量摄取
// （Ingest）与结构检查（Lint）。
//
// 刻意边界（oagentkit/rag#144）：
//   - 不做检索：页面产出后由宿主交给 rag 读侧索引（Indexer 接缝负责写入后
//     同步索引），wiki 只负责增量维护；
//   - 不做调度：何时摄取（会话结束/定时/人工）由宿主决定；
//   - 不做存储与向量绑定：Store 是存储无关接口，MemoryStore 与
//     github.com/oagentkit/rag/wiki/fsstore 是零依赖参考实现。
//
// 生命周期: Experimental——页面模型与 Store 缝尚待真实宿主接入磨合，未过
// api-compat.md 晋级三维，初值如实记为不承诺兼容。
package wiki
