package embedtest

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
)

// 本文件是 Embedder 的正/负对照参考实现（fake）：
//
//   - HashEmbedder      正对照（确定性档）：同输入永远同向量；
//   - DriftingEmbedder  正对照（排序档）：向量逐调用漂移、余弦排序严格不变；
//   - FailingEmbedder   正对照故障注入：失败如实报 ErrInjected；
//   - SwallowEmbedder   负对照（静默吞错）：失败以全零向量 + nil error 冒充
//     成功——套件对它的判定必须是红（contract_test.go 钉住）。
//
// 全部零外部依赖、只实现 rag.Embedder 接口面，可直接抄进自己的测试里对照。

// ErrInjected 故障注入错误：FailingEmbedder 的必败错误（正对照）。
var ErrInjected = errors.New("embedtest: 注入的必败错误")

// driftPerCall DriftingEmbedder 的逐调用整体缩放步长。1e-3 量级让分量漂移
// 远超 determinismEps（确定性档判红）；缩放对余弦严格不变，排序判据零抖动
// ——这是两档分级口径语义差异的最小充分漂移。
const driftPerCall = 1e-3

// HashEmbedder 正对照（确定性档）：向量是文本的确定性哈希函数——同输入
// 永远同向量，不同文本得到不同向量（L2 归一化）。
type HashEmbedder struct {
	dim int
}

// NewHashEmbedder 构造确定性哈希 Embedder；dim <= 0 回落 16。
func NewHashEmbedder(dim int) *HashEmbedder {
	if dim <= 0 {
		dim = 16
	}
	return &HashEmbedder{dim: dim}
}

// Dim 返回向量维度。
func (e *HashEmbedder) Dim() int { return e.dim }

// Embed 返回文本的确定性哈希向量。
func (e *HashEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(texts) == 0 {
		return nil, nil
	}
	out := make([][]float32, len(texts))
	for i, s := range texts {
		out[i] = hashVector(s, e.dim)
	}
	return out, nil
}

// DriftingEmbedder 正对照（排序档）：同输入的向量逐调用整体缩放
// （× (1+driftPerCall·calls)）——分量漂移（确定性档判红），余弦相似度严格
// 不变（排序档全绿）。随机后端的漂移形态更杂，但排序承诺同此判据。
type DriftingEmbedder struct {
	dim   int
	calls atomic.Int64
}

// NewDriftingEmbedder 构造漂移 Embedder；dim <= 0 回落 16。
func NewDriftingEmbedder(dim int) *DriftingEmbedder {
	if dim <= 0 {
		dim = 16
	}
	return &DriftingEmbedder{dim: dim}
}

// Dim 返回向量维度。
func (e *DriftingEmbedder) Dim() int { return e.dim }

// Embed 返回整体缩放后的哈希向量（缩放系数随调用次数递增）。
func (e *DriftingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(texts) == 0 {
		return nil, nil
	}
	n := e.calls.Add(1)
	scale := float32(1 + driftPerCall*float64(n))
	out := make([][]float32, len(texts))
	for i, s := range texts {
		v := hashVector(s, e.dim)
		for k := range v {
			v[k] *= scale
		}
		out[i] = v
	}
	return out, nil
}

// FailingEmbedder 正对照故障注入：每次调用返回 ErrInjected——错误不静默
// 场景的合规形态（失败必须报 error）。
type FailingEmbedder struct {
	dim int
}

// NewFailingEmbedder 构造必败 Embedder；dim <= 0 回落 16。
func NewFailingEmbedder(dim int) *FailingEmbedder {
	if dim <= 0 {
		dim = 16
	}
	return &FailingEmbedder{dim: dim}
}

// Dim 返回向量维度。
func (e *FailingEmbedder) Dim() int { return e.dim }

// Embed 恒返回 ErrInjected（nil 结果）。
func (e *FailingEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return nil, ErrInjected
}

// SwallowEmbedder 负对照（静默吞错）：失败时不返回 error，而是以全零向量
// 冒充成功——契约明令禁止的形态，套件对它的判定必须是红
// （TestNegativeControl_SwallowJudgedRed 钉住这一点）。
type SwallowEmbedder struct {
	dim int
	// failAll=true 时每次调用都处于失败态（本该报错）——但被吞成零向量。
	failAll bool
}

// NewSwallowEmbedder 构造「正常路径出哈希向量、失败被吞成零向量」的吞错实现。
func NewSwallowEmbedder(dim int) *SwallowEmbedder {
	if dim <= 0 {
		dim = 16
	}
	return &SwallowEmbedder{dim: dim}
}

// NewSwallowFailingEmbedder 构造「必然失败」的吞错实现——NewFailing 位的
// 负对照：应报错的调用全部被吞成全零向量 + nil error。
func NewSwallowFailingEmbedder(dim int) *SwallowEmbedder {
	e := NewSwallowEmbedder(dim)
	e.failAll = true
	return e
}

// Dim 返回向量维度。
func (e *SwallowEmbedder) Dim() int { return e.dim }

// Embed 吞错形态：失败（failAll 或 ctx 已取消）返回全零向量 + nil error。
func (e *SwallowEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if e.failAll || ctx.Err() != nil {
		return zeroMatrix(len(texts), e.dim), nil
	}
	out := make([][]float32, len(texts))
	for i, s := range texts {
		out[i] = hashVector(s, e.dim)
	}
	return out, nil
}

// hashVector 文本 → 确定性伪随机单位向量：分量取自 (fnv1a(text), 序号) 的
// 独立混合流，映射到 [-1, 1) 后 L2 归一化。空文本也不产生全零向量。
func hashVector(text string, dim int) []float32 {
	base := fnv1a64([]byte(text))
	v := make([]float32, dim)
	for i := range v {
		x := mix64(base + uint64(i+1)*0x9e3779b97f4a7c15)
		v[i] = float32(float64(x>>11)/float64(uint64(1)<<53)*2 - 1)
	}
	l2normalize(v)
	return v
}

// fnv1a64 FNV-1a 64 位哈希（纯函数，不走 hash.Hash 的 error 面）。
func fnv1a64(data []byte) uint64 {
	h := uint64(14695981039346656037)
	for _, b := range data {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return h
}

// mix64 splitmix64 混合器：把 (文本哈希, 分量序号) 打散成独立分量流。
func mix64(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}

// l2normalize 就地 L2 归一化；全零输入保持全零（不发明向量）。
func l2normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
}

// zeroMatrix n 条 dim 维全零向量（吞错负对照的产出形态）。
func zeroMatrix(n, dim int) [][]float32 {
	out := make([][]float32, n)
	for i := range out {
		out[i] = make([]float32, dim)
	}
	return out
}
