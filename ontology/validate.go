package ontology

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

// validateConfig 校验判据的可配项。
type validateConfig struct {
	allowMultiObject bool
}

// ValidateOption 配置校验判据的行为。
type ValidateOption func(*validateConfig)

// AllowMultiObject 允许同一 subject+predicate 指向多个 object。
//
// 默认口径是**报候选**（KindConflictingObjects）：一对多是领域决策，
// 宿主显式声明「这里允许」才关闭。关闭后该判据不产出任何结论。
func AllowMultiObject() ValidateOption {
	return func(c *validateConfig) { c.allowMultiObject = true }
}

// Validate 按 Schema 校验一批实体与三元组，返回**全部**结论（零条 = 合法）。
//
// 判据体是**纯函数**：不碰 IO、不读全局状态、不改入参（结论是新切片）。
// 取数面（Store 实现）由宿主注入，判据不依赖它——判据与取数解耦，
// 才能对同一批断言做离线复算与 golden 断言。
//
// 结论类别（每类一项，见 FindingKind）：
//
//	KindUndeclaredType        实体类型 / ref 目标类型未声明
//	KindUndeclaredPredicate   谓词不是主体类型的字段
//	KindMissingRequiredField  缺少必填字段
//	KindInvalidValue          客体取值与字段类型不符
//	KindDuplicateTriple       重复三元组
//	KindMissingEvidence       无证据
//	KindConflictingObjects    同一主体+谓词多客体（AllowMultiObject 可关）
//	KindIncomplete            三元组/实体字段缺失
//	KindInvalidConfidence     置信度越界
//
// 结论顺序确定：schema 级 → 实体级（按实体名排序）→ 三元组级（按入参顺序）
// → 矛盾候选（按主体+谓词排序）。
//
// **实体登记完备性不在本包判据面**：主体或引用目标未登记时，跳过依赖类型的检查
// （谓词/必填/ref 目标类型），其余检查照做。登记了的实体才按类型校验。
func Validate(s Schema, entities []Entity, triples []Triple, opts ...ValidateOption) []Finding {
	cfg := validateConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	v := &validator{
		schema:   s,
		cfg:      cfg,
		byName:   make(map[string]Entity, len(entities)),
		findings: make([]Finding, 0),
	}
	for _, e := range entities {
		if _, exists := v.byName[e.Name]; !exists {
			v.byName[e.Name] = e
		}
	}
	v.checkSchema()
	v.checkEntities(entities, triples)
	v.checkTriples(triples)
	v.checkConflicts(triples)
	return v.findings
}

// validator 一次校验的中间状态。
type validator struct {
	schema   Schema
	cfg      validateConfig
	byName   map[string]Entity
	findings []Finding
}

// add 追加一条结论。
func (v *validator) add(kind FindingKind, subject, predicate, object, message string) {
	v.findings = append(v.findings, Finding{
		Kind:      kind,
		Subject:   subject,
		Predicate: predicate,
		Object:    object,
		Message:   message,
	})
}

// checkSchema 校验声明面自身：字段类型串合法、引用目标类型已声明。
func (v *validator) checkSchema() {
	for _, t := range v.schema.Types {
		for _, f := range t.Fields {
			if IsScalarFieldType(f.Type) {
				continue
			}
			target, isRef := ParseRefType(f.Type)
			if !isRef {
				v.add(KindUndeclaredType, t.Name, f.Name, "",
					"字段类型 "+f.Type+" 非法：既不是标量类型也不是 ref:<Type>")
				continue
			}
			if _, declared := v.schema.Type(target); !declared {
				v.add(KindUndeclaredType, t.Name, f.Name, "",
					"引用目标类型 "+target+" 未在 Schema 声明")
			}
		}
	}
}

// checkEntities 校验实体级结论：实体完整、类型已声明、必填字段齐备。
func (v *validator) checkEntities(entities []Entity, triples []Triple) {
	sorted := slices.Clone(entities)
	slices.SortStableFunc(sorted, func(a, b Entity) int {
		if c := cmp.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return cmp.Compare(a.Type, b.Type)
	})
	for _, e := range sorted {
		if e.Name == "" {
			v.add(KindIncomplete, "", "", "", "实体缺名字")
			continue
		}
		if e.Type == "" {
			v.add(KindIncomplete, e.Name, "", "", "实体 "+e.Name+" 缺类型")
			continue
		}
		t, declared := v.schema.Type(e.Type)
		if !declared {
			v.add(KindUndeclaredType, e.Name, "", "",
				"实体类型 "+e.Type+" 未在 Schema 声明")
			continue
		}
		for _, f := range t.Fields {
			if !f.Required {
				continue
			}
			if !hasFieldValue(triples, e.Name, f.Name) {
				v.add(KindMissingRequiredField, e.Name, f.Name, "",
					"必填字段 "+f.Name+" 缺失")
			}
		}
	}
}

// checkTriples 校验三元组级结论：完整、有证据、置信度合法、不重复、谓词与取值合法。
func (v *validator) checkTriples(triples []Triple) {
	seen := make(map[tripleKey]int, len(triples))
	for _, t := range triples {
		var missing []string
		if t.Subject == "" {
			missing = append(missing, "主体")
		}
		if t.Predicate == "" {
			missing = append(missing, "谓词")
		}
		if t.Object == "" {
			missing = append(missing, "客体")
		}
		if len(missing) > 0 {
			v.add(KindIncomplete, t.Subject, t.Predicate, t.Object,
				"三元组缺"+strings.Join(missing, "/"))
			continue
		}
		if t.Evidence == "" {
			v.add(KindMissingEvidence, t.Subject, t.Predicate, t.Object,
				"三元组没有证据")
		}
		if t.Confidence < 0 || t.Confidence > 1 {
			v.add(KindInvalidConfidence, t.Subject, t.Predicate, t.Object,
				"置信度 "+strconv.FormatFloat(t.Confidence, 'g', -1, 64)+" 不在 [0,1]")
		}
		key := tripleKey{subject: t.Subject, predicate: t.Predicate, object: t.Object}
		seen[key]++
		if seen[key] > 1 {
			v.add(KindDuplicateTriple, t.Subject, t.Predicate, t.Object,
				"重复三元组（第 "+strconv.Itoa(seen[key])+" 次出现）")
		}
		v.checkPredicate(t)
	}
}

// checkPredicate 校验谓词在主体类型下已声明，以及客体取值与字段类型相符。
func (v *validator) checkPredicate(t Triple) {
	e, ok := v.byName[t.Subject]
	if !ok {
		// 主体未登记：跳过依赖类型的检查（登记完备性不在本包判据面）。
		return
	}
	typ, declared := v.schema.Type(e.Type)
	if !declared {
		// 类型未声明已单独报过，不重复报。
		return
	}
	f, ok := typ.Field(t.Predicate)
	if !ok {
		v.add(KindUndeclaredPredicate, t.Subject, t.Predicate, t.Object,
			"谓词不是类型 "+typ.Name+" 的字段")
		return
	}
	v.checkValue(t, f)
}

// checkValue 校验客体取值与字段声明的类型相符。
func (v *validator) checkValue(t Triple, f Field) {
	switch f.Type {
	case FieldTypeNumber:
		if _, err := strconv.ParseFloat(t.Object, 64); err != nil {
			v.add(KindInvalidValue, t.Subject, t.Predicate, t.Object,
				"数值字段取值 "+t.Object+" 不是合法数值")
		}
	case FieldTypeBool:
		if _, err := strconv.ParseBool(t.Object); err != nil {
			v.add(KindInvalidValue, t.Subject, t.Predicate, t.Object,
				"布尔字段取值 "+t.Object+" 不是合法布尔值")
		}
	default:
		target, isRef := ParseRefType(f.Type)
		if !isRef {
			return
		}
		ref, ok := v.byName[t.Object]
		if !ok {
			// 引用目标未登记：跳过（登记完备性不在本包判据面）。
			return
		}
		if ref.Type != target {
			v.add(KindInvalidValue, t.Subject, t.Predicate, t.Object,
				"引用目标类型 "+ref.Type+" 与声明的 "+target+" 不符")
		}
	}
}

// checkConflicts 报矛盾候选：同一主体+谓词指向多个客体。
func (v *validator) checkConflicts(triples []Triple) {
	if v.cfg.allowMultiObject {
		return
	}
	type group struct {
		objects []string
	}
	groups := make(map[subjectPredicate]*group)
	keys := make([]subjectPredicate, 0, len(triples))
	for _, t := range triples {
		if t.Subject == "" || t.Predicate == "" || t.Object == "" {
			continue
		}
		key := subjectPredicate{subject: t.Subject, predicate: t.Predicate}
		g, ok := groups[key]
		if !ok {
			g = &group{}
			groups[key] = g
			keys = append(keys, key)
		}
		if !slices.Contains(g.objects, t.Object) {
			g.objects = append(g.objects, t.Object)
		}
	}
	slices.SortFunc(keys, func(a, b subjectPredicate) int {
		if c := cmp.Compare(a.subject, b.subject); c != 0 {
			return c
		}
		return cmp.Compare(a.predicate, b.predicate)
	})
	for _, key := range keys {
		g := groups[key]
		if len(g.objects) < 2 {
			continue
		}
		objects := slices.Clone(g.objects)
		slices.Sort(objects)
		v.add(KindConflictingObjects, key.subject, key.predicate, "",
			"同一主体+谓词指向多个客体："+strings.Join(objects, "、"))
	}
}

// hasFieldValue 报告实体 name 是否有一条谓词为 field、客体非空的三元组。
func hasFieldValue(triples []Triple, name, field string) bool {
	for _, t := range triples {
		if t.Subject == name && t.Predicate == field && t.Object != "" {
			return true
		}
	}
	return false
}

// subjectPredicate 主体+谓词：矛盾候选的分组键。
type subjectPredicate struct {
	subject   string
	predicate string
}
