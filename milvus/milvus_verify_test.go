package milvus

import (
	"context"
	"strings"
	"testing"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"

	"github.com/oagentkit/rag"
	"github.com/oekit/pkg/errors"
)

// fakeVerifyClient 实现 verifyExistingCollection 用到的面：
// DescribeCollection（形状）、GetLoadState（加载态，返回 Loaded 免补加载）
// 与 DescribeIndex（索引面，#134）。
type fakeVerifyClient struct {
	client.Client
	col      *entity.Collection
	indexes  []entity.Index
	indexErr error
}

func (f *fakeVerifyClient) DescribeCollection(_ context.Context, _ string) (*entity.Collection, error) {
	return f.col, nil
}

func (f *fakeVerifyClient) GetLoadState(_ context.Context, _ string, _ []string) (entity.LoadState, error) {
	return entity.LoadStateLoaded, nil
}

func (f *fakeVerifyClient) DescribeIndex(_ context.Context, _ string, _ string, _ ...client.IndexOption) ([]entity.Index, error) {
	return f.indexes, f.indexErr
}

// goodVectorIndex 创建路径同款索引：IVF_FLAT + COSINE。
func goodVectorIndex() entity.Index {
	return entity.NewGenericIndex("vec_idx", entity.IvfFlat, map[string]string{
		milvusMetricTypeParam: string(entity.COSINE),
	})
}

// verifyStore 以「创建路径同款 schema」为基线构造存在路径校验的 store：
// id 主键 max_length=milvusIDMaxLen、embedding dim=1024、IVF_FLAT/COSINE 索引。
func verifyStore(t *testing.T, mutate func(schema *entity.Schema)) *Store {
	t.Helper()
	schema := entity.NewSchema().
		WithName("c").
		WithField(entity.NewField().WithName(milvusIDField).WithDataType(entity.FieldTypeVarChar).WithMaxLength(milvusIDMaxLen).WithIsPrimaryKey(true)).
		WithField(entity.NewField().WithName(milvusContentField).WithDataType(entity.FieldTypeVarChar).WithMaxLength(milvusContentMaxLen)).
		WithField(entity.NewField().WithName(milvusFileField).WithDataType(entity.FieldTypeVarChar).WithMaxLength(rag.FileStoredMaxLen)).
		WithField(entity.NewField().WithName(milvusHeadingField).WithDataType(entity.FieldTypeVarChar).WithMaxLength(milvusHeadingMaxLen)).
		WithField(entity.NewField().WithName(milvusVectorField).WithDataType(entity.FieldTypeFloatVector).WithDim(1024))
	if mutate != nil {
		mutate(schema)
	}
	return &Store{
		cfg:    Config{Collection: "c", Dimension: 1024, MetricType: entity.COSINE},
		client: &fakeVerifyClient{col: &entity.Collection{Schema: schema}, indexes: []entity.Index{goodVectorIndex()}},
	}
}

// verifyStoreWithIndexes 基线 store + 指定索引面。
func verifyStoreWithIndexes(t *testing.T, indexes ...entity.Index) *Store {
	t.Helper()
	s := verifyStore(t, nil)
	s.client.(*fakeVerifyClient).indexes = indexes
	return s
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

// #134：外部建的 HNSW 索引集合构造期必须拒绝——searchChunks 固定发 IVF_FLAT
// 的 nprobe 参数，索引类型不匹配会在首次 Search 于服务端深处报错。
func TestVerifyExistingCollectionRejectsNonIVFFlatIndex(t *testing.T) {
	hnsw := entity.NewGenericIndex("hnsw_idx", entity.HNSW, map[string]string{
		milvusMetricTypeParam: string(entity.COSINE),
	})
	err := verifyStoreWithIndexes(t, hnsw).verifyExistingCollection(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HNSW") || !strings.Contains(err.Error(), "IVF_FLAT") {
		t.Fatalf("HNSW 索引应拒绝并点名类型差异: %v", err)
	}
}

// #134：索引 metric 与配置不一致（L2 vs 缺省 COSINE）必须拒绝——Search 携带
// cfg.MetricType，与索引度量不匹配同样在服务端深处报错。
func TestVerifyExistingCollectionRejectsMetricMismatch(t *testing.T) {
	l2 := entity.NewGenericIndex("vec_idx", entity.IvfFlat, map[string]string{
		milvusMetricTypeParam: string(entity.L2),
	})
	err := verifyStoreWithIndexes(t, l2).verifyExistingCollection(context.Background())
	if err == nil || !strings.Contains(err.Error(), "L2") || !strings.Contains(err.Error(), "COSINE") {
		t.Fatalf("L2 索引与 COSINE 配置应拒绝并点名度量差异: %v", err)
	}
}

// #134：有 schema 无索引的集合构造期必须拒绝（DescribeCollection 看不到）。
func TestVerifyExistingCollectionRejectsMissingIndex(t *testing.T) {
	err := verifyStoreWithIndexes(t).verifyExistingCollection(context.Background())
	if err == nil || !strings.Contains(err.Error(), "无索引") {
		t.Fatalf("无索引应拒绝: %v", err)
	}
}

// #134：索引缺 metric_type 参数 = 无法确认度量一致性，与漂移同档拒绝（对齐
// dim 的 fail-closed 判据）。
func TestVerifyExistingCollectionRejectsMissingMetricParam(t *testing.T) {
	noMetric := entity.NewGenericIndex("vec_idx", entity.IvfFlat, map[string]string{})
	err := verifyStoreWithIndexes(t, noMetric).verifyExistingCollection(context.Background())
	if err == nil || !strings.Contains(err.Error(), "metric_type") {
		t.Fatalf("缺 metric_type 应拒绝: %v", err)
	}
}

// #134：DescribeIndex 自身失败必须上抛（构造期不能带病放行）。
func TestVerifyExistingCollectionPropagatesIndexError(t *testing.T) {
	s := verifyStore(t, nil)
	s.client.(*fakeVerifyClient).indexErr = errors.New("index rpc down")
	err := s.verifyExistingCollection(context.Background())
	if err == nil || !strings.Contains(err.Error(), "索引") || !strings.Contains(err.Error(), "index rpc down") {
		t.Fatalf("DescribeIndex 失败应上抛: %v", err)
	}
}

// #134 对照组：metric 大小写不敏感（Milvus 度量名大小写不敏感），不应误拦。
func TestVerifyExistingCollectionAcceptsMetricCaseInsensitive(t *testing.T) {
	lower := entity.NewGenericIndex("vec_idx", entity.IvfFlat, map[string]string{
		milvusMetricTypeParam: "cosine",
	})
	if err := verifyStoreWithIndexes(t, lower).verifyExistingCollection(context.Background()); err != nil {
		t.Fatalf("metric 大小写不同应按一致放行: %v", err)
	}
}
