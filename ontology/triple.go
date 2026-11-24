package ontology

// Triple 抽取结果契约：一条断言（主体—谓词—客体）连同它的依据。
//
// 本包只定义**契约**，不负责产出三元组——抽取实现（提示词、模型、批处理）
// 与领域强相关，留在宿主；「抽取实现可换、契约不变」是本层存在的理由。
//
// 语义（与 Schema 的对应关系）：
//
//   - Subject 是实体名；
//   - Predicate 是该实体类型的**字段名**（Schema.Type.Fields）；
//   - Object 是该字段的取值：标量字段是取值的字符串形式；
//     "ref:<Type>" 字段是**另一个实体的名字**；
//   - Evidence 是依据（片段/URL/会话 ID）——**没有证据的三元组不收**
//     （Validate 报 KindMissingEvidence）；
//   - Confidence 是置信度，取值区间 [0, 1]。
type Triple struct {
	// Subject 主体实体名。
	Subject string
	// Predicate 谓词（主体类型的字段名）。
	Predicate string
	// Object 客体：标量取值的字符串形式，或被引用实体的名字。
	Object string
	// Evidence 依据（片段/URL/会话 ID）。为空即不合法。
	Evidence string
	// Confidence 置信度，区间 [0, 1]。
	Confidence float64
}
