package ontology

import "testing"

func TestRefTypeAndParseRefType(t *testing.T) {
	got := RefType("component")
	if got != "ref:component" {
		t.Fatalf("RefType(component) = %q，期望 ref:component", got)
	}
	name, ok := ParseRefType(got)
	if !ok || name != "component" {
		t.Fatalf("ParseRefType(%q) = (%q, %v)，期望 (component, true)", got, name, ok)
	}
	if _, ok := ParseRefType(FieldTypeString); ok {
		t.Fatal("ParseRefType(string) = ok，期望 false")
	}
	if _, ok := ParseRefType("ref:"); ok {
		t.Fatal("ParseRefType(ref:) = ok，期望 false（目标类型为空）")
	}
}

func TestIsScalarFieldType(t *testing.T) {
	for _, ft := range []string{FieldTypeString, FieldTypeNumber, FieldTypeBool} {
		if !IsScalarFieldType(ft) {
			t.Errorf("IsScalarFieldType(%q) = false，期望 true", ft)
		}
	}
	for _, ft := range []string{"", RefType("component"), "wat"} {
		if IsScalarFieldType(ft) {
			t.Errorf("IsScalarFieldType(%q) = true，期望 false", ft)
		}
	}
}

func TestSchemaTypeLookup(t *testing.T) {
	s := testSchema()
	typ, ok := s.Type("service")
	if !ok {
		t.Fatal("Type(service) 未找到")
	}
	if typ.Name != "service" || len(typ.Fields) != 4 {
		t.Fatalf("Type(service) = %+v，期望 service 且 4 个字段", typ)
	}
	if _, ok := s.Type("missing"); ok {
		t.Fatal("Type(missing) = ok，期望 false")
	}
}

func TestTypeFieldLookup(t *testing.T) {
	typ, ok := testSchema().Type("service")
	if !ok {
		t.Fatal("Type(service) 未找到")
	}
	f, ok := typ.Field("owner")
	if !ok {
		t.Fatal("Field(owner) 未找到")
	}
	if !f.Required || f.Type != FieldTypeString {
		t.Fatalf("Field(owner) = %+v，期望必填 string", f)
	}
	if _, ok := typ.Field("missing"); ok {
		t.Fatal("Field(missing) = ok，期望 false")
	}
}
