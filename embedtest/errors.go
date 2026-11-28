package embedtest

import "errors"

// 契约违规哨兵（Err 前缀 + stdlib errors，errors.Is 跨包判定成立）。
// 每条钉一种违规形态；场景报错时用 %w 包住对应哨兵，调用方按哨兵归类。
var (
	// ErrContract Contract 配置非法（New / NewFailing 缺失、Guarantee 出词表）。
	ErrContract = errors.New("embedtest: Contract 配置非法")

	// ErrPanic 被测实现 panic。
	ErrPanic = errors.New("embedtest: 实现 panic")

	// ErrUnexpectedError 正常输入（短文本探针）意外返回 error——成功路径必须成功。
	ErrUnexpectedError = errors.New("embedtest: 正常输入意外返回 error")

	// ErrSilentFailure 失败被静默吞掉：必败实现返回 nil error（错误不静默的反面）。
	ErrSilentFailure = errors.New("embedtest: 失败被静默吞掉（未返回 error）")

	// ErrZeroVector 以全零向量冒充成功结果——任何成功路径都不许出全零向量。
	ErrZeroVector = errors.New("embedtest: 全零向量冒充成功结果")

	// ErrEmptyInput 空输入行为不符：应返回空结果且无 error。
	ErrEmptyInput = errors.New("embedtest: 空输入行为不符（应空结果且无 error）")

	// ErrResultShape 返回条数与输入不符。
	ErrResultShape = errors.New("embedtest: 返回条数与输入不符")

	// ErrDimMismatch 维度契约违规：Dim() ≤ 0、Dim() 不稳定、或向量长度 ≠ Dim()。
	// 维度不符时的错误形态即本哨兵（%w 包裹细节，errors.Is 可判）。
	ErrDimMismatch = errors.New("embedtest: 向量维度与 Dim() 不符或不稳定")

	// ErrBatchSingle 批量与单条路径结果不一致（按声明档判等）。
	ErrBatchSingle = errors.New("embedtest: 批量与单条路径结果不一致")

	// ErrGuarantee 违反声明的分级口径（确定性 / 余弦排序一致性）。
	ErrGuarantee = errors.New("embedtest: 违反声明的分级口径")
)
