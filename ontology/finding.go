package ontology

// FindingKind 判据结论的类别。
type FindingKind string

const (
	// KindUndeclaredType 实体类型、或 ref 目标类型未在 Schema 内声明
	//（也覆盖字段类型串既非标量又非合法引用的情况）。
	KindUndeclaredType FindingKind = "undeclared_type"
	// KindUndeclaredPredicate 谓词不是主体类型声明的字段。
	KindUndeclaredPredicate FindingKind = "undeclared_predicate"
	// KindMissingRequiredField 实体缺少 Schema 声明的必填字段。
	KindMissingRequiredField FindingKind = "missing_required_field"
	// KindInvalidValue 客体取值与字段声明的类型不符。
	KindInvalidValue FindingKind = "invalid_value"
	// KindDuplicateTriple 重复三元组：主体+谓词+客体完全相同。
	KindDuplicateTriple FindingKind = "duplicate_triple"
	// KindMissingEvidence 三元组没有证据。
	KindMissingEvidence FindingKind = "missing_evidence"
	// KindConflictingObjects 同一主体+谓词指向多个客体（矛盾候选）。
	KindConflictingObjects FindingKind = "conflicting_objects"
	// KindIncomplete 三元组缺主体/谓词/客体，或实体缺名字/类型。
	KindIncomplete FindingKind = "incomplete"
	// KindInvalidConfidence 置信度不在 [0, 1]。
	KindInvalidConfidence FindingKind = "invalid_confidence"
)

// Finding 一条判据结论。
type Finding struct {
	// Kind 结论类别。
	Kind FindingKind
	// Subject 涉及的主体（实体名或三元组主体）；无则为空。
	Subject string
	// Predicate 涉及的谓词/字段名；无则为空。
	Predicate string
	// Object 涉及的客体；无则为空。
	Object string
	// Message 人类可读说明（判定依据）。
	Message string
}
