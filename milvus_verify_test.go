package rag

import (
	"context"
	"strings"
	"testing"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

// fakeVerifyClient 只实现 verifyExistingCollection 用到的两个面：
// DescribeCollection（形状）与 GetLoadState（加载态，返回 Loaded 免补加载）。
type fakeVerifyClient struct {
	client.Client
	col *entity.Collection
}

func (f *fakeVerifyClient) DescribeCollection(_ context.Context, _ string) (*entity.Collection, error) {
	return f.col, nil
}

func (f *fakeVerifyClient) GetLoadState(_ context.Context, _ string, _ []string) (entity.LoadState, error) {
	return entity.LoadStateLoaded, nil
}

// verifyStore 以「创建路径同款 schema」为基线构造存在路径校验的 store：
// id 主键 max_length=milvusIDMaxLen、embedding dim=1024。
func verifyStore(t *testing.T, mutate func(schema *entity.Schema)) *MilvusStore {
	t.Helper()
	schema := entity.NewSchema().
		WithName("c").
		WithField(entity.NewField().WithName(milvusIDField).WithDataType(entity.FieldTypeVarChar).WithMaxLength(milvusIDMaxLen).WithIsPrimaryKey(true)).
		WithField(entity.NewField().WithName(milvusContentField).WithDataType(entity.FieldTypeVarChar).WithMaxLength(milvusContentMaxLen)).
		WithField(entity.NewField().WithName(milvusFileField).WithDataType(entity.FieldTypeVarChar).WithMaxLength(milvusFileMaxLen)).
		WithField(entity.NewField().WithName(milvusHeadingField).WithDataType(entity.FieldTypeVarChar).WithMaxLength(milvusHeadingMaxLen)).
		WithField(entity.NewField().WithName(milvusVectorField).WithDataType(entity.FieldTypeFloatVector).WithDim(1024))
	if mutate != nil {
		mutate(schema)
	}
	return &MilvusStore{
		cfg:    MilvusConfig{Collection: "c", Dimension: 1024},
		client: &fakeVerifyClient{col: &entity.Collection{Schema: schema}},
	}
}

// 字段定位辅助：按名改 TypeParams。
func fieldByName(schema *entity.Schema, name string) *entity.Field {
	for _, f := range schema.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// 基线：创建路径同款形状 → 构造期放行（防止闸门把合法集合也拦掉）。
func TestVerifyExistingCollectionAcceptsCreatePathShape(t *testing.T) {
	if err := verifyStore(t, nil).verifyExistingCollection(context.Background()); err != nil {
		t.Fatalf("创建路径同款形状应放行: %v", err)
	}
}

// #117 对照组：dim 漂移必须拒绝（#97 主路径，收口前就该在位）。
func TestVerifyExistingCollectionRejectsDimDrift(t *testing.T) {
	err := verifyStore(t, func(s *entity.Schema) {
		fieldByName(s, milvusVectorField).TypeParams[entity.TypeParamDim] = "999"
	}).verifyExistingCollection(context.Background())
	if err == nil || !strings.Contains(err.Error(), "维度") {
		t.Fatalf("dim 漂移应拒绝并点名维度: %v", err)
	}
}

// #117 旁路 A：向量字段无 dim TypeParams → 此前整块跳过（fail-open），现在拒绝。
func TestVerifyExistingCollectionRejectsMissingDim(t *testing.T) {
	err := verifyStore(t, func(s *entity.Schema) {
		delete(fieldByName(s, milvusVectorField).TypeParams, entity.TypeParamDim)
	}).verifyExistingCollection(context.Background())
	if err == nil || !strings.Contains(err.Error(), "dim") {
		t.Fatalf("缺 dim 参数应拒绝（无法确认维度一致性）: %v", err)
	}
}

// #117 旁路 B：dim 不可解析 → 此前 Sscanf 失败即跳过（fail-open），现在拒绝。
func TestVerifyExistingCollectionRejectsUnparsableDim(t *testing.T) {
	for _, bad := range []string{"abc", "", "1024x"} {
		err := verifyStore(t, func(s *entity.Schema) {
			fieldByName(s, milvusVectorField).TypeParams[entity.TypeParamDim] = bad
		}).verifyExistingCollection(context.Background())
		if err == nil || !strings.Contains(err.Error(), "dim") {
			t.Fatalf("dim=%q 不可解析应拒绝: %v", bad, err)
		}
	}
}

// #118：主键 max_length=128（旧 Milvus 默认）→ 契约承诺的形状漂移必须在
// 构造期显式暴露，而非首次写长 id 时在服务端深处报错。
func TestVerifyExistingCollectionRejectsShortIDMaxLength(t *testing.T) {
	err := verifyStore(t, func(s *entity.Schema) {
		fieldByName(s, milvusIDField).TypeParams[entity.TypeParamMaxLength] = "128"
	}).verifyExistingCollection(context.Background())
	if err == nil || !strings.Contains(err.Error(), "max_length") {
		t.Fatalf("id max_length=128 应拒绝并点名 max_length: %v", err)
	}
}

// #118：max_length 不可解析同档拒绝（无法确认主键宽度）。
func TestVerifyExistingCollectionRejectsUnparsableIDMaxLength(t *testing.T) {
	err := verifyStore(t, func(s *entity.Schema) {
		fieldByName(s, milvusIDField).TypeParams[entity.TypeParamMaxLength] = "abc"
	}).verifyExistingCollection(context.Background())
	if err == nil || !strings.Contains(err.Error(), "max_length") {
		t.Fatalf("max_length 不可解析应拒绝: %v", err)
	}
}

// #118 判据方向：更大的 max_length（外部建集合常见 65535）与缺失（Milvus
// 服务端缺省）都按兼容放行——写入侧 id 已被 milvusRowID 钉在 milvusIDMaxLen
// 内，更宽只意味着余量富余，全等判会误拦能正常工作的集合。
func TestVerifyExistingCollectionAcceptsWiderOrMissingIDMaxLength(t *testing.T) {
	t.Run("更宽", func(t *testing.T) {
		err := verifyStore(t, func(s *entity.Schema) {
			fieldByName(s, milvusIDField).TypeParams[entity.TypeParamMaxLength] = "65535"
		}).verifyExistingCollection(context.Background())
		if err != nil {
			t.Fatalf("更宽的 max_length 应放行: %v", err)
		}
	})
	t.Run("缺失", func(t *testing.T) {
		err := verifyStore(t, func(s *entity.Schema) {
			delete(fieldByName(s, milvusIDField).TypeParams, entity.TypeParamMaxLength)
		}).verifyExistingCollection(context.Background())
		if err != nil {
			t.Fatalf("缺 max_length 按服务端缺省判定应放行: %v", err)
		}
	})
}
