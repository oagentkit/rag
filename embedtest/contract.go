package embedtest

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/oagentkit/rag"
)

// Contract 被测 Embedder 的构造与承诺声明。
type Contract struct {
	// New 返回全新被测 Embedder，每个场景各构造一次。
	// 实现持有的后台资源由实现自持生命周期（套件不代管清理）。
	New func() rag.Embedder

	// Guarantee 分级口径声明（必填，两档之一，见 Guarantee）。
	Guarantee Guarantee

	// NewFailing 返回一个「必然失败」的被测 Embedder（如指向不可达端点、
	// 注入传输错误）——错误不静默场景用它验证失败必须返回 error，
	// 不得零向量冒充成功（静默吞错负对照在此判红）。
	NewFailing func() rag.Embedder
}

// validate 校验 Contract 配置（缺必填字段或 Guarantee 出词表即非法）。
func (c Contract) validate() error {
	switch {
	case c.New == nil:
		return fmt.Errorf("%w: New 未提供", ErrContract)
	case c.NewFailing == nil:
		return fmt.Errorf("%w: NewFailing 未提供", ErrContract)
	case c.Guarantee != GuaranteeDeterministic && c.Guarantee != GuaranteeCosineRank:
		return fmt.Errorf("%w: Guarantee 出词表（两档之一）: %q", ErrContract, c.Guarantee)
	}
	return nil
}

// probeTexts 语义可分的锚文本集（余弦排序判据用）。文本两两不同：
// 区分度守卫（全同向量让排序口径空转）依赖此性质。
var probeTexts = []string{
	"go 语言并发编程与 channel",
	"go 语言接口设计与组合",
	"python 数据分析与 pandas",
	"意大利番茄意面做法",
}

// scenario 单条契约场景：name 是子测试名（Run）/ 违规前缀（Check），
// onlyDeterministic 表示仅在声明确定性档时运行。
type scenario struct {
	name              string
	onlyDeterministic bool
	fn                func(ctx context.Context, c Contract) []error
}

// scenarios 完整场景矩阵。每个场景钉一条必须跨实现一致的语义：
//
//  1. empty_input_returns_empty   空输入返回空结果且无 error
//  2. dim_contract                维度契约：Dim() > 0 且稳定，向量长度恒等于
//     Dim()；维度不符以 ErrDimMismatch 哨兵报出
//  3. batch_matches_single        批量与单条路径结果一致（按声明档判等）
//  4. guarantee_deterministic     声明确定性档时：同输入两次向量分量级近等
//  5. guarantee_cosine_rank       两档共有：同输入两次的两两余弦排序一致
//     （附锚文本区分度守卫）
//  6. long_input_boundary         超长输入不 panic；要么 error 要么合法向量
//  7. errors_not_silent           失败必须返回 error，不得零向量冒充成功
func scenarios() []scenario {
	return []scenario{
		{"empty_input_returns_empty", false, checkEmptyInput},
		{"dim_contract", false, checkDimContract},
		{"batch_matches_single", false, checkBatchSingle},
		{"guarantee_deterministic", true, checkDeterminism},
		{"guarantee_cosine_rank", false, checkCosineRank},
		{"long_input_boundary", false, checkLongInput},
		{"errors_not_silent", false, checkErrorsNotSilent},
	}
}

// Run 以命名子测试跑完整契约场景（任何子测试失败即实现违反契约）。
// 形态对齐 oekit cachetest.Run：Contract 抽象 + 命名子测试矩阵，每子测试
// 钉一条语义。声明确定性档才跑 guarantee_deterministic（其余两档同跑）。
func Run(t *testing.T, c Contract) {
	t.Helper()
	if err := c.validate(); err != nil {
		t.Fatalf("Contract 配置非法: %v", err)
	}
	ctx := context.Background()
	for _, s := range scenarios() {
		s := s
		t.Run(s.name, func(t *testing.T) {
			t.Helper()
			if s.onlyDeterministic && c.Guarantee != GuaranteeDeterministic {
				t.Skipf("未声明 %s（本实现声明 %s）", GuaranteeDeterministic, c.Guarantee)
			}
			for _, err := range safeRun(ctx, s.name, c, s.fn) {
				t.Error(err)
			}
		})
	}
}

// Check 跑与 Run 同一套场景矩阵，返回违规清单（空 = 合规）。
// 这是机器可判形态：负对照测试直接断言「必须判红」（contract_test.go），
// 非 testing 管线也可用它做验收。
func Check(ctx context.Context, c Contract) []error {
	if err := c.validate(); err != nil {
		return []error{err}
	}
	var viols []error
	for _, s := range scenarios() {
		if s.onlyDeterministic && c.Guarantee != GuaranteeDeterministic {
			continue
		}
		viols = append(viols, safeRun(ctx, s.name, c, s.fn)...)
	}
	return viols
}

// safeRun 跑单个场景并兜住 panic（实现 panic 也是一种违规，记 ErrPanic）。
func safeRun(ctx context.Context, name string, c Contract, fn func(context.Context, Contract) []error) (viols []error) {
	defer func() {
		if r := recover(); r != nil {
			viols = append(viols, fmt.Errorf("场景 %s: %w: %v", name, ErrPanic, r))
		}
	}()
	for _, err := range fn(ctx, c) {
		viols = append(viols, fmt.Errorf("场景 %s: %w", name, err))
	}
	return viols
}

// checkEmptyInput 场景 1：空输入（nil 与空切片）必须返回空结果且无 error。
func checkEmptyInput(ctx context.Context, c Contract) []error {
	var viols []error
	e := c.New()
	for i, texts := range [][]string{nil, {}} {
		got, err := e.Embed(ctx, texts)
		if err != nil {
			viols = append(viols, fmt.Errorf("%w: 空输入(%d)不应回错误, got %v", ErrEmptyInput, i, err))
		}
		if len(got) != 0 {
			viols = append(viols, fmt.Errorf("%w: 空输入(%d)应返回空结果, got %d 条", ErrEmptyInput, i, len(got)))
		}
	}
	return viols
}

// checkDimContract 场景 2：维度契约——Dim() > 0 且跨调用稳定，每次返回的
// 每个向量长度恒等于 Dim()。维度不符时的错误形态 = ErrDimMismatch 哨兵。
func checkDimContract(ctx context.Context, c Contract) []error {
	var viols []error
	e := c.New()
	d := e.Dim()
	if d <= 0 {
		return append(viols, fmt.Errorf("%w: Dim() 必须 > 0, got %d", ErrDimMismatch, d))
	}
	for _, texts := range [][]string{probeTexts[:1], probeTexts[:2], probeTexts} {
		got, err := e.Embed(ctx, texts)
		if err != nil {
			viols = append(viols, fmt.Errorf("%w: 正常输入意外失败: %v", ErrUnexpectedError, err))
			continue
		}
		if len(got) != len(texts) {
			viols = append(viols, fmt.Errorf("%w: 输入 %d 条返回 %d 条", ErrResultShape, len(texts), len(got)))
			continue
		}
		for i, v := range got {
			if len(v) != d {
				viols = append(viols, fmt.Errorf("%w: texts[%d] 向量长度 %d != Dim() %d", ErrDimMismatch, i, len(v), d))
			}
		}
	}
	if got := e.Dim(); got != d {
		viols = append(viols, fmt.Errorf("%w: Dim() 不稳定: 先 %d 后 %d", ErrDimMismatch, d, got))
	}
	return viols
}

// checkBatchSingle 场景 3：批量与单条路径结果一致——按声明档判等：
// 确定性档逐分量近等，排序档两两余弦排序一致。
func checkBatchSingle(ctx context.Context, c Contract) []error {
	var viols []error
	e := c.New()
	texts := probeTexts
	batch, err := e.Embed(ctx, texts)
	if err != nil {
		return append(viols, fmt.Errorf("%w: 批量路径正常输入意外失败: %v", ErrUnexpectedError, err))
	}
	if len(batch) != len(texts) {
		return append(viols, fmt.Errorf("%w: 批量路径返回 %d 条 != 输入 %d 条", ErrResultShape, len(batch), len(texts)))
	}
	singles := make([][]float32, len(texts))
	for i, s := range texts {
		got, err := e.Embed(ctx, []string{s})
		if err != nil {
			viols = append(viols, fmt.Errorf("%w: 单条路径 texts[%d] 意外失败: %v", ErrUnexpectedError, i, err))
			continue
		}
		if len(got) != 1 {
			viols = append(viols, fmt.Errorf("%w: 单条路径 texts[%d] 返回 %d 条", ErrResultShape, i, len(got)))
			continue
		}
		singles[i] = got[0]
	}
	if len(viols) > 0 {
		return viols
	}
	switch c.Guarantee {
	case GuaranteeDeterministic:
		for i := range texts {
			if !vecClose(batch[i], singles[i], determinismEps) {
				viols = append(viols, fmt.Errorf("%w: texts[%d] 批量与单条分量级不一致（确定性档）", ErrBatchSingle, i))
			}
		}
	case GuaranteeCosineRank:
		ob, os := pairRankOrder(batch), pairRankOrder(singles)
		if !slices.Equal(ob, os) {
			viols = append(viols, fmt.Errorf("%w: 批量与单条的两两余弦排序不一致（排序档）: %v vs %v", ErrBatchSingle, ob, os))
		}
	}
	return viols
}

// checkDeterminism 场景 4（仅确定性档声明时运行）：同输入两次 Embed 的向量
// 分量级近等（|Δ| ≤ determinismEps）。
func checkDeterminism(ctx context.Context, c Contract) []error {
	e := c.New()
	texts := probeTexts
	r1, err1 := e.Embed(ctx, texts)
	r2, err2 := e.Embed(ctx, texts)
	if err1 != nil || err2 != nil {
		return []error{fmt.Errorf("%w: 同输入重复 embed 意外失败: %v / %v", ErrUnexpectedError, err1, err2)}
	}
	if len(r1) != len(texts) || len(r2) != len(texts) {
		return []error{fmt.Errorf("%w: 返回条数与输入不符（%d/%d != %d）", ErrResultShape, len(r1), len(r2), len(texts))}
	}
	var viols []error
	for i := range texts {
		if !vecClose(r1[i], r2[i], determinismEps) {
			viols = append(viols, fmt.Errorf("%w: texts[%d] 同输入两次向量不一致（确定性档, |Δ| > %g）", ErrGuarantee, i, determinismEps))
		}
	}
	return viols
}

// checkCosineRank 场景 5（两档都跑）：同输入两次 Embed 的锚文本向量，两两
// 余弦相似度排序必须一致。附区分度守卫：锚文本向量全同让排序口径空转。
func checkCosineRank(ctx context.Context, c Contract) []error {
	e := c.New()
	texts := probeTexts
	r1, err := e.Embed(ctx, texts)
	if err != nil {
		return []error{fmt.Errorf("%w: 第一次 embed 意外失败: %v", ErrUnexpectedError, err)}
	}
	r2, err := e.Embed(ctx, texts)
	if err != nil {
		return []error{fmt.Errorf("%w: 第二次 embed 意外失败: %v", ErrUnexpectedError, err)}
	}
	if len(r1) != len(texts) || len(r2) != len(texts) {
		return []error{fmt.Errorf("%w: 返回条数与输入不符（%d/%d != %d）", ErrResultShape, len(r1), len(r2), len(texts))}
	}
	if allSame(r1) || allSame(r2) {
		return []error{fmt.Errorf("%w: 锚文本向量全同——两两余弦排序无从谈起", ErrGuarantee)}
	}
	o1, o2 := pairRankOrder(r1), pairRankOrder(r2)
	if !slices.Equal(o1, o2) {
		return []error{fmt.Errorf("%w: 同输入两次的两两余弦排序不一致: %v vs %v", ErrGuarantee, o1, o2)}
	}
	return nil
}

// checkLongInput 场景 6：超长输入（~2.6MiB）不 panic；要么以 error 拒绝，
// 要么给出合法向量（条数/维度对、非全零）——不得零向量充数。
func checkLongInput(ctx context.Context, c Contract) []error {
	e := c.New()
	long := strings.Repeat("超长输入边界探测", 1<<17)
	got, err := e.Embed(ctx, []string{long})
	if err != nil {
		return nil // 超长输入以 error 拒绝是合法边界行为
	}
	var viols []error
	if len(got) != 1 {
		return append(viols, fmt.Errorf("%w: 超长输入成功时应返回 1 条, got %d", ErrResultShape, len(got)))
	}
	if d := e.Dim(); len(got[0]) != d {
		viols = append(viols, fmt.Errorf("%w: 超长输入向量长度 %d != Dim() %d", ErrDimMismatch, len(got[0]), d))
	}
	if isZeroVector(got[0]) {
		viols = append(viols, fmt.Errorf("%w: 超长输入以全零向量冒充成功", ErrZeroVector))
	}
	return viols
}

// checkErrorsNotSilent 场景 7：错误不静默——必败实现必须返回非 nil error
// （静默吞错 + 零向量充数在此判红）；任何成功结果不得含全零向量。
func checkErrorsNotSilent(ctx context.Context, c Contract) []error {
	var viols []error
	ef := c.NewFailing()
	got, err := ef.Embed(ctx, probeTexts)
	if err == nil {
		viols = append(viols, fmt.Errorf("%w: 必败实现返回 nil error", ErrSilentFailure))
		if isZeroMatrix(got) {
			viols = append(viols, fmt.Errorf("%w: 必败实现以全零向量冒充成功", ErrZeroVector))
		}
	}
	e := c.New()
	got2, err2 := e.Embed(ctx, probeTexts)
	if err2 == nil {
		for i, v := range got2 {
			if isZeroVector(v) {
				viols = append(viols, fmt.Errorf("%w: 正常结果 texts[%d] 是全零向量", ErrZeroVector, i))
			}
		}
	}
	return viols
}
