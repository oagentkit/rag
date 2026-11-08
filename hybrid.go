// Hybrid 向量+关键词双路混合检索（R10，RRF 融合）：语义路（MilvusStore，embedding
// 近邻）与词料路（Local，TF-IDF）各取候选，倒数排名融合（Reciprocal Rank Fusion）——
// 跨路命中的文档得分累加（双路都认为相关=真相关的最强信号），单路独有的按名次计分。
// 降级哲学与装配侧一致：任一路失败降级为另一路纯检索（检索面永不因一路缺失禁用），
// 双路全败才回错。
package rag

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/cloudwego/eino/components/tool"
	"github.com/oekit/concurrency/async"
	"github.com/oekit/concurrency/syncx"
)

// rrfK RRF 常数（惯例 60）：越大排名差异对得分的影响越平缓。
const rrfK = 60

// Hybrid 组合检索器：vector=语义路（必选），keyword=词料路（Local 的 TF-IDF 索引，
// 纯内存随目录重建）。
type Hybrid struct {
	vector  KnowledgeService
	keyword *Local
}

// NewHybrid 构造混合检索器。
func NewHybrid(vector KnowledgeService, keyword *Local) *Hybrid {
	return &Hybrid{vector: vector, keyword: keyword}
}

// Retrieve 双路并行检索 → RRF 融合 → topK。两路各取 topK*2 候选（融合后截断，
// 候选池放大让跨路重叠有归并空间）；单路失败降级另一路，双路全败回 first error。
// 幸存路的合法空命中（该查询无词汇重叠等）是正常检索结果不是错误——返回空切片
// + nil，不得被失败路放大成整体验索错误（rag#76）；失败面含 panic 时空结果不
// 掩盖（自家 bug 必须暴露，见下）。
// 双路经 syncx.FanInErr 收口（concurrency v0.5.0 起 Slot 三态）：路内 panic 不再
// 伪装成空路，而是作为该路失败参与降级裁决——单路 panic 降级另一路，双路全败
// （含 panic）回错不再静默返回空结果；恐慌经全局 handler 上报，不出宿主进程。
func (h *Hybrid) Retrieve(ctx context.Context, query string, topK int, filter Filter) ([]Chunk, error) {
	// 非法 topK 与两个后端同口径缺省归一（oagentkit/rag#30）：此前原始值直通
	// FuseRRF——topK=0 静默吞掉两路全部结果（n=0 err=nil，比报错更坏），topK<0
	// 在 FuseRRF 的 keys[:topK] 裸 panic（且在 FanInErr 收口之外的主 goroutine，
	// 降级机制接不住）。
	if topK <= 0 {
		topK = defaultTopK
	}
	lanes := []func() ([]Chunk, error){
		func() ([]Chunk, error) { return h.vector.Retrieve(ctx, query, topK*2, filter) },
		func() ([]Chunk, error) { return h.keyword.Retrieve(ctx, query, topK*2, filter) },
	}
	slots := syncx.FanInErr(len(lanes), func(i int) ([]Chunk, error) { return lanes[i]() })

	var lists [][]Chunk
	var firstErr error
	var anySuccess, anyPanic bool
	for _, s := range slots {
		// v0.7.0 起 panic 由 Slot.Err 承载 *async.PanicError（errors.As 可辨、
		// 携带栈），Slot.Panicked 已废弃。区分 panic 与普通错误是为了给调用方
		// 一个不同的错误文案——panic 是我们的 bug，普通错误是下游不可用。
		err := s.Err
		var pe *async.PanicError
		if errors.As(err, &pe) {
			anyPanic = true
			err = fmt.Errorf("rag: hybrid 检索路 panic: %w", pe)
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		anySuccess = true
		if len(s.Val) > 0 {
			lists = append(lists, s.Val)
		}
	}
	// 聚合裁决（rag#76）：非空结果优先——任一路命中即融合返回（失败路按降级
	// 契约丢弃）。结果面为空时三分，不再把「成功但空」与「失败」合流：
	//   - 至少一路成功（含合法空命中）且失败面无 panic → 返回空 + nil：空命中
	//     是正常检索结果，调用方的降级/重试/报警链路不得被纯词汇不匹配触发；
	//   - 失败面含 panic → 回 firstErr（自家 bug 不掩盖——空结果不得吞掉 panic
	//     信号，TestHybridLanePanicDegrades 的刻意取舍）；
	//   - 双路全败 → 回 firstErr。
	if len(lists) > 0 {
		return FuseRRF(topK, lists...), nil
	}
	if anySuccess && !anyPanic {
		return FuseRRF(topK), nil
	}
	return nil, firstErr
}

// AsTool 工具化透传语义路（检索语义对 LLM 不可见——混合是检索器内部实现细节）。
func (h *Hybrid) AsTool() tool.BaseTool { return h.vector.AsTool() }

// FuseRRF 倒数排名融合：score=Σ 1/(k+rank_i)（rank 从 1 起），同 key 跨路累加
// （双路共识加权=混合检索的本意）；输出按得分降序、同分按 key 字典序（确定性
// 可重放），截 topK。lists 为空返回空。
//
// 融合 key 与去重语义（oagentkit/rag#40）：优先稳定 chunk 标识 ID（Local 与
// Milvus 双路统一 file#chunk_N 格式）；无 ID 时回退 file+内容、再退纯内容——
// 去重=同源去重（同一 chunk 的跨路副本归并），**不是**同内容跨文件去重：
// 许可证头/告警模板等相同内容不同来源的 chunk 各自保留，此前以内容做 key
// 让它们跨文件塌缩成一份且无信号。同一 list 内同 key 只计首个名次——同路
// 重复不得给自己加权（此前同路两个副本各占一个 rank 给同一 key 双重计分，
// 挤占 topK）。
func FuseRRF(topK int, lists ...[]Chunk) []Chunk {
	score := map[string]float64{}
	best := map[string]Chunk{}
	for _, chunks := range lists {
		seen := make(map[string]bool, len(chunks)) // 同路同 key 只计一次名次
		for i, c := range chunks {
			key := fuseKey(c)
			if seen[key] {
				continue
			}
			seen[key] = true
			score[key] += 1 / float64(rrfK+i+1)
			if prev, ok := best[key]; !ok || len(c.Content) > len(prev.Content) {
				best[key] = c // 跨路同 key 保留信息量最大的代表副本
			}
		}
	}
	keys := make([]string, 0, len(score))
	for k := range score {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if score[keys[i]] != score[keys[j]] {
			return score[keys[i]] > score[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if topK < 0 {
		topK = 0 // 导出纯函数不 panic（负数截断在 keys[:topK] 会越界，rag#30）
	}
	if len(keys) > topK {
		keys = keys[:topK]
	}
	out := make([]Chunk, 0, len(keys))
	for _, k := range keys {
		c := best[k]
		c.Score = score[k] // 融合分回填（检索观测面：跨路共识值 > 单路值）
		out = append(out, c)
	}
	return out
}

// fuseKey 融合去重 key：ID → file+内容 → 内容（类型前缀防三类 key 互相碰撞）。
// 无 ID 时纳入 Metadata["file"] 区分来源——同文件同内容仍是同源（归并），不同
// 文件的相同内容互异（各自保留，rag#40）。
func fuseKey(c Chunk) string {
	if c.ID != "" {
		return "id\x00" + c.ID
	}
	if f := c.Metadata["file"]; f != "" {
		return "file\x00" + f + "\x00" + c.Content
	}
	return "content\x00" + c.Content
}
