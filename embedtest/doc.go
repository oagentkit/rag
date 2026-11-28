// Package embedtest 是 rag.Embedder 的共享契约测试套件（conformance）：把
// Embedder 实现的行为约定写成可复用的场景集，供第三方实现方（HTTP 服务、
// 云 API 自定义实现等）直接引入做合规验证——换 Embedder 实现，同套测试全绿。
//
// 接入三行（README「如何验证你的 Embedder 合规」）：
//
//	func TestMyEmbedder(t *testing.T) {
//	    embedtest.Run(t, embedtest.Contract{...})
//	}
//
// 分级口径（两档，实现自选声明，见 Guarantee）：
//   - GuaranteeDeterministic 承诺确定性：同输入两次向量分量级近等
//     （|Δ| ≤ determinismEps）；
//   - GuaranteeCosineRank 承诺余弦相似度排序一致：向量可逐分量漂移，但锚文本
//     集的两两余弦相似度排序不变。
//
// 两档是蕴含关系（确定性 ⊃ 排序一致）：声明确定性的实现跑全部确定性判据，
// 排序判据两档都跑。违规以 Err 前缀哨兵报出（stdlib errors.Is 可判，见
// errors.go）；负对照参考实现（SwallowEmbedder）被套件判红，是套件有牙齿的
// 机器证据（contract_test.go）。
//
// 生命周期: Experimental——场景集随判据面演进会增删，正/负对照参考实现同包，
// 套件本身不承诺兼容（与 kernel sidetest/boardtest 同定位）；未过
// api-compat.md 晋级判据，初值如实记为不承诺兼容。
package embedtest
