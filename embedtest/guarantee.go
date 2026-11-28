package embedtest

import (
	"cmp"
	"math"
	"slices"
)

// Guarantee 是 Embedder 一致性承诺的分级口径（两档，实现自选声明）。
//
// 两档是蕴含关系：确定性 ⊃ 余弦排序一致。声明 GuaranteeDeterministic 的实现
// 必然满足排序档判据；声明 GuaranteeCosineRank 的实现不承诺逐分量相等
// （随机后端 / 非幂等 API 的诚实声明）。
type Guarantee string

const (
	// GuaranteeDeterministic 承诺确定性：同输入两次 Embed 的向量分量级近等
	// （|Δ| ≤ determinismEps）。适用于本地模型、确定性哈希、温度为 0 且
	// 后端保证幂等的 API。
	GuaranteeDeterministic Guarantee = "deterministic"

	// GuaranteeCosineRank 承诺余弦相似度排序一致：同输入两次 Embed 的向量
	// 可以逐分量漂移，但任意一组锚文本的两两余弦相似度排序必须相同。
	// 适用于带采样温度、数值非幂等的云 API。
	GuaranteeCosineRank Guarantee = "cosine-rank"
)

// determinismEps 确定性档的分量级近等容差。取 1e-5：对 L2 归一化向量
// （分量 ≤ 1）而言，这是「同向量」的实用口径——容忍浮点归约顺序带来的
// 噪声，但远小于任何真实语义漂移（负对照 DriftingEmbedder 的 1e-3 量级
// 缩放漂移在 20 倍余量外判红）。bit 相等是更强的实现性质，不作要求。
const determinismEps = 1e-5

// pair 锚文本对下标（i < j）。
type pair struct{ i, j int }

// cosineSim 两向量余弦相似度（float64 累加；长度不等或零向量返回 0）。
func cosineSim(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for k := range a {
		x, y := float64(a[k]), float64(b[k])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// vecClose 分量级近等（|Δ| ≤ eps）；长度不等即不等。
func vecClose(a, b []float32, eps float64) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if math.Abs(float64(a[k])-float64(b[k])) > eps {
			return false
		}
	}
	return true
}

// isZeroVector 全零向量判定（分量精确为 0）——零向量不是合法语义向量，
// 是「错误被吞成零向量」的可观测形态。
func isZeroVector(v []float32) bool {
	for _, x := range v {
		if x != 0 {
			return false
		}
	}
	return true
}

// isZeroMatrix 非空结果集且全部向量为全零。
func isZeroMatrix(m [][]float32) bool {
	if len(m) == 0 {
		return false
	}
	for _, v := range m {
		if !isZeroVector(v) {
			return false
		}
	}
	return true
}

// allSame 全部向量逐分量相同——区分度守卫用（全同向量让排序口径空转）。
func allSame(vecs [][]float32) bool {
	if len(vecs) < 2 {
		return false
	}
	for _, v := range vecs[1:] {
		if !slices.Equal(vecs[0], v) {
			return false
		}
	}
	return true
}

// pairRankOrder 返回按两两余弦相似度降序排列的锚文本对序列；
// 相似度并列时按 (i, j) 升序钉死次序（排序判据只认「两次序列相同」）。
func pairRankOrder(vecs [][]float32) []pair {
	ps := make([]pair, 0, len(vecs)*(len(vecs)-1)/2)
	sims := make(map[pair]float64, len(vecs)*(len(vecs)-1)/2)
	for i := range vecs {
		for j := i + 1; j < len(vecs); j++ {
			p := pair{i, j}
			ps = append(ps, p)
			sims[p] = cosineSim(vecs[i], vecs[j])
		}
	}
	slices.SortStableFunc(ps, func(x, y pair) int {
		if sx, sy := sims[x], sims[y]; sx != sy {
			if sx > sy {
				return -1
			}
			return 1
		}
		if x.i != y.i {
			return cmp.Compare(x.i, y.i)
		}
		return cmp.Compare(x.j, y.j)
	})
	return ps
}
