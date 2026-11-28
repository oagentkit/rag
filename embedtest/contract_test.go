package embedtest

import (
	"context"
	"errors"
	"testing"

	"github.com/oagentkit/rag"
)

// compliantContract 正对照的 Contract：New 出被测实现、NewFailing 出合规
// 故障注入（FailingEmbedder 如实报错）。
func compliantContract(g Guarantee, newE func() rag.Embedder) Contract {
	return Contract{
		New:        newE,
		Guarantee:  g,
		NewFailing: func() rag.Embedder { return NewFailingEmbedder(16) },
	}
}

// hasViolation 违规清单里是否命中目标哨兵（%w 包裹 ⇒ errors.Is 可判）。
func hasViolation(viols []error, target error) bool {
	for _, v := range viols {
		if errors.Is(v, target) {
			return true
		}
	}
	return false
}

// TestRun_HashEmbedder_Deterministic 正对照全绿（确定性档）。
func TestRun_HashEmbedder_Deterministic(t *testing.T) {
	Run(t, compliantContract(GuaranteeDeterministic, func() rag.Embedder {
		return NewHashEmbedder(16)
	}))
}

// TestRun_HashEmbedder_CosineRank 正对照全绿（弱档声明不设障）。
func TestRun_HashEmbedder_CosineRank(t *testing.T) {
	Run(t, compliantContract(GuaranteeCosineRank, func() rag.Embedder {
		return NewHashEmbedder(16)
	}))
}

// TestRun_DriftingEmbedder_CosineRank 正对照全绿（排序档）：向量漂移的
// 随机后端形态在排序承诺下合规。
func TestRun_DriftingEmbedder_CosineRank(t *testing.T) {
	Run(t, compliantContract(GuaranteeCosineRank, func() rag.Embedder {
		return NewDriftingEmbedder(16)
	}))
}

// TestGuaranteeTiersAreDistinct 分级口径的两档有真实差异：漂移实现在
// 确定性档判红、排序档全绿；确定性实现在两档都全绿。
func TestGuaranteeTiersAreDistinct(t *testing.T) {
	drift := func() rag.Embedder { return NewDriftingEmbedder(16) }
	hash := func() rag.Embedder { return NewHashEmbedder(16) }

	if viols := Check(context.Background(), compliantContract(GuaranteeDeterministic, drift)); !hasViolation(viols, ErrGuarantee) {
		t.Fatalf("漂移实现在确定性档必须判红（ErrGuarantee），got %v", viols)
	}
	if viols := Check(context.Background(), compliantContract(GuaranteeCosineRank, drift)); len(viols) != 0 {
		t.Fatalf("漂移实现在排序档必须全绿，got %v", viols)
	}
	if viols := Check(context.Background(), compliantContract(GuaranteeDeterministic, hash)); len(viols) != 0 {
		t.Fatalf("确定性实现在确定性档必须全绿，got %v", viols)
	}
	if viols := Check(context.Background(), compliantContract(GuaranteeCosineRank, hash)); len(viols) != 0 {
		t.Fatalf("确定性实现在排序档必须全绿，got %v", viols)
	}
}

// TestNegativeControl_SwallowJudgedRed 负对照判红（套件有牙齿的机器证据）：
// 静默吞错实现（失败以全零向量 + nil error 冒充成功）必须被错误不静默
// 场景判红，命中 ErrSilentFailure 与 ErrZeroVector 两个哨兵。
func TestNegativeControl_SwallowJudgedRed(t *testing.T) {
	c := compliantContract(GuaranteeDeterministic, func() rag.Embedder {
		return NewHashEmbedder(16)
	})
	c.NewFailing = func() rag.Embedder { return NewSwallowFailingEmbedder(16) }

	viols := Check(context.Background(), c)
	if len(viols) == 0 {
		t.Fatal("静默吞错负对照必须判红，got 全绿")
	}
	if !hasViolation(viols, ErrSilentFailure) {
		t.Errorf("应命中 ErrSilentFailure（失败被静默吞掉），got %v", viols)
	}
	if !hasViolation(viols, ErrZeroVector) {
		t.Errorf("应命中 ErrZeroVector（零向量冒充成功），got %v", viols)
	}
}

// dimCheater 负对照（测试内）：声明 Dim()=8 却返回 6 维向量——维度不符形态。
type dimCheater struct{}

// Dim 声明 8 维。
func (dimCheater) Dim() int { return 8 }

// Embed 实际返回 6 维向量（与 Dim() 不符）。
func (dimCheater) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		v := make([]float32, 6)
		v[0] = 1
		out[i] = v
	}
	return out, nil
}

// TestNegativeControl_WrongDimJudgedRed 负对照判红：维度不符以 ErrDimMismatch
// 哨兵报出（「维度不符时的错误形态」判据）。
func TestNegativeControl_WrongDimJudgedRed(t *testing.T) {
	c := compliantContract(GuaranteeDeterministic, func() rag.Embedder {
		return dimCheater{}
	})
	viols := Check(context.Background(), c)
	if !hasViolation(viols, ErrDimMismatch) {
		t.Fatalf("错维实现必须命中 ErrDimMismatch，got %v", viols)
	}
}

// TestContractValidation 缺必填字段的 Contract 必须报 ErrContract（判不了
// 不等于没问题）。
func TestContractValidation(t *testing.T) {
	if viols := Check(context.Background(), Contract{}); !hasViolation(viols, ErrContract) {
		t.Fatalf("空 Contract 应报 ErrContract，got %v", viols)
	}
	c := compliantContract("bogus", func() rag.Embedder { return NewHashEmbedder(16) })
	if viols := Check(context.Background(), c); !hasViolation(viols, ErrContract) {
		t.Fatalf("Guarantee 出词表应报 ErrContract，got %v", viols)
	}
}
