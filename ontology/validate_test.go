package ontology

import (
	"reflect"
	"slices"
	"testing"
)

// testSchema 合成夹具：两个抽象类型（与任何具体领域无关）。
func testSchema() Schema {
	return Schema{
		Types: []Type{
			{
				Name: "service",
				Fields: []Field{
					{Name: "owner", Type: FieldTypeString, Required: true},
					{Name: "tier", Type: FieldTypeNumber},
					{Name: "active", Type: FieldTypeBool},
					{Name: "depends_on", Type: RefType("component")},
				},
			},
			{
				Name:   "component",
				Fields: []Field{{Name: "label", Type: FieldTypeString, Required: true}},
			},
		},
	}
}

// testEntities 合成夹具：两个实体。
func testEntities() []Entity {
	return []Entity{
		{Name: "svc-a", Type: "service"},
		{Name: "component-b", Type: "component"},
	}
}

// testTriples 合成夹具：三条合法三元组。
func testTriples() []Triple {
	return []Triple{
		{Subject: "svc-a", Predicate: "owner", Object: "team-x", Evidence: "chunk-1", Confidence: 0.9},
		{Subject: "svc-a", Predicate: "depends_on", Object: "component-b", Evidence: "chunk-2", Confidence: 0.8},
		{Subject: "component-b", Predicate: "label", Object: "part-b", Evidence: "chunk-3", Confidence: 0.7},
	}
}

// kindsOf 抽取结论类别序列（用于顺序断言）。
func kindsOf(findings []Finding) []FindingKind {
	out := make([]FindingKind, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Kind)
	}
	return out
}

// countKind 统计某类结论的条数。
func countKind(findings []Finding, kind FindingKind) int {
	n := 0
	for _, f := range findings {
		if f.Kind == kind {
			n++
		}
	}
	return n
}

func TestValidateValidSetHasNoFindings(t *testing.T) {
	findings := Validate(testSchema(), testEntities(), testTriples())
	if len(findings) != 0 {
		t.Fatalf("合法集合得到 %d 条结论，期望 0：%+v", len(findings), findings)
	}
}

func TestValidateUndeclaredType(t *testing.T) {
	entities := append(testEntities(), Entity{Name: "svc-z", Type: "widget"})
	findings := Validate(testSchema(), entities, testTriples())
	if got := countKind(findings, KindUndeclaredType); got != 1 {
		t.Fatalf("未声明类型结论 %d 条，期望 1：%+v", got, findings)
	}
	f := findings[0]
	if f.Subject != "svc-z" {
		t.Fatalf("结论 Subject = %q，期望 svc-z", f.Subject)
	}
}

func TestValidateUndeclaredRefTargetType(t *testing.T) {
	s := testSchema()
	s.Types[0].Fields = append(s.Types[0].Fields, Field{Name: "peer", Type: RefType("widget")})
	findings := Validate(s, testEntities(), testTriples())
	if got := countKind(findings, KindUndeclaredType); got != 1 {
		t.Fatalf("ref 目标未声明结论 %d 条，期望 1：%+v", got, findings)
	}
	if findings[0].Predicate != "peer" {
		t.Fatalf("结论 Predicate = %q，期望 peer", findings[0].Predicate)
	}
}

func TestValidateInvalidFieldType(t *testing.T) {
	s := testSchema()
	s.Types[0].Fields = append(s.Types[0].Fields, Field{Name: "weird", Type: "wat"})
	findings := Validate(s, testEntities(), testTriples())
	if got := countKind(findings, KindUndeclaredType); got != 1 {
		t.Fatalf("非法字段类型结论 %d 条，期望 1：%+v", got, findings)
	}
}

func TestValidateMissingRequiredField(t *testing.T) {
	triples := testTriples()[1:]
	findings := Validate(testSchema(), testEntities(), triples)
	if got := countKind(findings, KindMissingRequiredField); got != 1 {
		t.Fatalf("缺必填结论 %d 条，期望 1：%+v", got, findings)
	}
	if findings[0].Subject != "svc-a" || findings[0].Predicate != "owner" {
		t.Fatalf("结论 = %+v，期望 svc-a/owner", findings[0])
	}
}

func TestValidateDuplicateTriple(t *testing.T) {
	triples := append(testTriples(), testTriples()[0])
	findings := Validate(testSchema(), testEntities(), triples)
	if got := countKind(findings, KindDuplicateTriple); got != 1 {
		t.Fatalf("重复三元组结论 %d 条，期望 1：%+v", got, findings)
	}
}

func TestValidateMissingEvidence(t *testing.T) {
	triples := testTriples()
	triples[0].Evidence = ""
	findings := Validate(testSchema(), testEntities(), triples)
	if got := countKind(findings, KindMissingEvidence); got != 1 {
		t.Fatalf("无证据结论 %d 条，期望 1：%+v", got, findings)
	}
}

func TestValidateConflictingObjectsDefaultReports(t *testing.T) {
	triples := append(testTriples(),
		Triple{Subject: "svc-a", Predicate: "owner", Object: "team-y", Evidence: "chunk-4", Confidence: 0.6})
	findings := Validate(testSchema(), testEntities(), triples)
	if got := countKind(findings, KindConflictingObjects); got != 1 {
		t.Fatalf("矛盾候选结论 %d 条，期望 1：%+v", got, findings)
	}
	f := findings[0]
	if f.Subject != "svc-a" || f.Predicate != "owner" {
		t.Fatalf("结论 = %+v，期望 svc-a/owner", f)
	}
}

func TestValidateConflictingObjectsDisabled(t *testing.T) {
	triples := append(testTriples(),
		Triple{Subject: "svc-a", Predicate: "owner", Object: "team-y", Evidence: "chunk-4", Confidence: 0.6})
	findings := Validate(testSchema(), testEntities(), triples, AllowMultiObject())
	if len(findings) != 0 {
		t.Fatalf("显式关掉一对多后得到 %d 条结论，期望 0：%+v", len(findings), findings)
	}
}

func TestValidateUndeclaredPredicate(t *testing.T) {
	triples := append(testTriples(),
		Triple{Subject: "svc-a", Predicate: "unknown_field", Object: "x", Evidence: "chunk-5", Confidence: 0.5})
	findings := Validate(testSchema(), testEntities(), triples)
	if got := countKind(findings, KindUndeclaredPredicate); got != 1 {
		t.Fatalf("未声明谓词结论 %d 条，期望 1：%+v", got, findings)
	}
}

func TestValidateInvalidValue(t *testing.T) {
	cases := []struct {
		name string
		trip Triple
	}{
		{
			name: "数值字段取值非法",
			trip: Triple{Subject: "svc-a", Predicate: "tier", Object: "abc", Evidence: "chunk-6", Confidence: 0.5},
		},
		{
			name: "布尔字段取值非法",
			trip: Triple{Subject: "svc-a", Predicate: "active", Object: "maybe", Evidence: "chunk-7", Confidence: 0.5},
		},
		{
			name: "引用目标类型不符",
			trip: Triple{Subject: "svc-a", Predicate: "depends_on", Object: "svc-a", Evidence: "chunk-8", Confidence: 0.5},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			triples := append(testTriples(), tc.trip)
			findings := Validate(testSchema(), testEntities(), triples)
			if got := countKind(findings, KindInvalidValue); got != 1 {
				t.Fatalf("取值非法结论 %d 条，期望 1：%+v", got, findings)
			}
		})
	}
}

func TestValidateIncomplete(t *testing.T) {
	cases := []struct {
		name     string
		entities []Entity
		triples  []Triple
		want     int
	}{
		{
			name:     "三元组缺客体",
			entities: testEntities(),
			triples:  append(testTriples(), Triple{Subject: "svc-a", Predicate: "owner", Evidence: "chunk-9", Confidence: 0.5}),
			want:     1,
		},
		{
			name:     "实体缺类型",
			entities: append(testEntities(), Entity{Name: "svc-z"}),
			triples:  testTriples(),
			want:     1,
		},
		{
			name:     "实体缺名字",
			entities: append(testEntities(), Entity{Type: "service"}),
			triples:  testTriples(),
			want:     1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := Validate(testSchema(), tc.entities, tc.triples)
			if got := countKind(findings, KindIncomplete); got != tc.want {
				t.Fatalf("不完整结论 %d 条，期望 %d：%+v", got, tc.want, findings)
			}
		})
	}
}

func TestValidateInvalidConfidence(t *testing.T) {
	for _, c := range []float64{-0.1, 1.5} {
		triples := append(testTriples(),
			Triple{Subject: "svc-a", Predicate: "tier", Object: "1", Evidence: "chunk-10", Confidence: c})
		findings := Validate(testSchema(), testEntities(), triples)
		if got := countKind(findings, KindInvalidConfidence); got != 1 {
			t.Fatalf("置信度 %v 的越界结论 %d 条，期望 1：%+v", c, got, findings)
		}
	}
}

func TestValidateIsPureFunction(t *testing.T) {
	entities := testEntities()
	triples := testTriples()
	entitiesBefore := slices.Clone(entities)
	triplesBefore := slices.Clone(triples)

	first := Validate(testSchema(), entities, triples)
	second := Validate(testSchema(), entities, triples)

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("两次调用结果不同：\n%+v\n%+v", first, second)
	}
	if !reflect.DeepEqual(entities, entitiesBefore) {
		t.Fatal("Validate 改了入参 entities")
	}
	if !reflect.DeepEqual(triples, triplesBefore) {
		t.Fatal("Validate 改了入参 triples")
	}
}

func TestValidateFindingOrderIsDeterministic(t *testing.T) {
	triples := append(testTriples(),
		Triple{Subject: "svc-a", Predicate: "owner", Object: "team-y", Evidence: "", Confidence: 2},
		Triple{Subject: "svc-a", Predicate: "owner", Object: "team-z", Evidence: "chunk-11", Confidence: 0.4},
	)
	entities := append(testEntities(), Entity{Name: "svc-z", Type: "widget"})

	want := []FindingKind{
		KindUndeclaredType,     // 实体 svc-z 的类型未声明
		KindMissingEvidence,    // 第二条 owner 三元组没有证据
		KindInvalidConfidence,  // 同一条三元组置信度越界
		KindConflictingObjects, // 同一主体+谓词指向三个客体
	}
	for i := 0; i < 5; i++ {
		got := kindsOf(Validate(testSchema(), entities, triples))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("第 %d 次调用的结论顺序 = %v，期望 %v", i, got, want)
		}
	}
}

func TestValidateNilOptionDoesNotPanic(t *testing.T) {
	findings := Validate(testSchema(), testEntities(), testTriples(), nil)
	if len(findings) != 0 {
		t.Fatalf("合法集合得到 %d 条结论，期望 0：%+v", len(findings), findings)
	}
}

func TestValidateWithoutEntitiesSkipsTypeDependentChecks(t *testing.T) {
	// 主体未登记 → 跳过依赖类型的检查（谓词/必填/ref 目标类型），
	// 其余检查（证据/重复/矛盾）照做。
	triples := []Triple{
		{Subject: "svc-a", Predicate: "whatever", Object: "x", Evidence: "", Confidence: 0.5},
	}
	findings := Validate(testSchema(), nil, triples)
	got := kindsOf(findings)
	want := []FindingKind{KindMissingEvidence}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("结论类别 = %v，期望 %v", got, want)
	}
}
