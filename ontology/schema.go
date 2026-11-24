package ontology

import "strings"

// 标量字段类型取值。
const (
	// FieldTypeString 字符串字段。
	FieldTypeString = "string"
	// FieldTypeNumber 数值字段。
	FieldTypeNumber = "number"
	// FieldTypeBool 布尔字段。
	FieldTypeBool = "bool"
)

// refPrefix 引用类型字段的类型前缀。
const refPrefix = "ref:"

// Field 实体字段声明。Type 的取值三选一：
//
//   - FieldTypeString / FieldTypeNumber / FieldTypeBool：标量字段；
//   - RefType(name) 形如 "ref:<Type>"：引用另一个实体类型。
//
// Required 为真表示该字段必须出现——实体缺少它即判不合法
// （Validate 报 KindMissingRequiredField）。
type Field struct {
	// Name 字段名（三元组的谓词）。
	Name string
	// Type 字段类型：标量类型或 "ref:<Type>"。
	Type string
	// Required 是否必填。
	Required bool
}

// Type 实体类型声明。字段名在类型内唯一。
type Type struct {
	// Name 类型名。
	Name string
	// Fields 字段声明。
	Fields []Field
}

// Schema 本体类型声明面。类型名在 Schema 内唯一。
type Schema struct {
	// Types 类型声明。
	Types []Type
}

// RefType 返回引用类型的字段类型串：RefType("component") == "ref:component"。
func RefType(name string) string {
	return refPrefix + name
}

// ParseRefType 解析 "ref:<Type>" 形式的字段类型；不是引用类型返回 ("", false)。
func ParseRefType(fieldType string) (string, bool) {
	if !strings.HasPrefix(fieldType, refPrefix) {
		return "", false
	}
	name := strings.TrimPrefix(fieldType, refPrefix)
	if name == "" {
		return "", false
	}
	return name, true
}

// IsScalarFieldType 报告 fieldType 是否是内建标量类型。
func IsScalarFieldType(fieldType string) bool {
	switch fieldType {
	case FieldTypeString, FieldTypeNumber, FieldTypeBool:
		return true
	default:
		return false
	}
}

// Type 按名取类型声明；未声明返回 ("", false)。
func (s Schema) Type(name string) (Type, bool) {
	for _, t := range s.Types {
		if t.Name == name {
			return t, true
		}
	}
	return Type{}, false
}

// Field 按名取字段声明；未声明返回 ("", false)。
func (t Type) Field(name string) (Field, bool) {
	for _, f := range t.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}
