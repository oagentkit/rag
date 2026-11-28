// Package milvus Milvus 向量检索后端。
//
// 使用 Milvus Go SDK v2 实现 rag.KnowledgeService 接口，支持向量相似度检索
// （余弦相似度 / L2 距离）。
//
// ⚠️ 本子包是**唯一**链接 milvus sdk 的地方（oagentkit/rag#156）：此前 sdk 只被
// 根目录 milvus.go import，而根目录五个文件全是 package rag ⇒ import 根包任何
// 符号（哪怕只用本地索引/混合召回）都会把 sdk 及其传递依赖
// （cockroachdb/errors、sentry-go、gogo/protobuf 等，矩阵最重传递链）拖进下游
// 二进制；README「向量后端可插拔」此前只是接口层面的承诺。
//
// 依赖方向：**子包 → 根包**（身份键/切块/过滤契约取自根包单源），根包不反向
// 依赖子包——根包因此不链 sdk，隔离才成立。
//
// 生命周期: Experimental——接口缝与配置面随 Milvus sdk 版本升级（v0.13 破坏点）
// 仍会调整，未过 api-compat.md 晋级三维，初值如实记为不承诺兼容。
package milvus

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"

	"github.com/oagentkit/rag"
	"github.com/oekit/logx"
	"github.com/oekit/pkg/errors"
)

// milvusLog 包级组件 logger（ekit v0.31 Delegating 纪律：包归属用 component 属性
// 标识、懒解析到 logx.Default()——消费方可 SetDefault 统一接管级别/后端）。
// 与根包的 ragLog 分组件标识（各自面各自告警）。
var milvusLog = logx.Delegating("oagentkit-rag-milvus")

const (
	milvusDefaultCollection = "agentkit_knowledge"
	milvusIDField           = "id"
	milvusContentField      = "content"
	milvusFileField         = "file"
	milvusHeadingField      = "heading"
	milvusVectorField       = "embedding"

	// VarChar 字段字节上限（Milvus 按字节计长）——schema 定义与写入守卫共用
	// 单源。此前守卫只覆盖 content（chunker maxChunkRunes），heading/file/
	// 主键 id 按字节超限时错误埋在 Milvus 服务端报错深处、整文件 Upsert 失败
	// （rag#43）。
	// file 列宽取根包 rag.FileStoredMaxLen——落库形态归一（rag.FileStored）与
	// 列宽同源，根包与本子包不得各写一份长度常量。
	milvusIDMaxLen      = 528 // 主键 = file + "#chunk_N"：file 上限 + 序号后缀余量
	milvusContentMaxLen = 65535
	milvusHeadingMaxLen = 512

	// milvusMetricTypeParam 索引描述里的度量参数键（SDK 未导出常量）。
	milvusMetricTypeParam = "metric_type"
	// milvusSweepBatchSize 对账游标单批行数。
	milvusSweepBatchSize = 512
)

// Config Milvus 连接配置。
type Config struct {
	Address    string            // Milvus 地址，如 "localhost:19530"
	Collection string            // 集合名，默认 "agentkit_knowledge"
	Dimension  int               // 向量维度（需与 Embedder.Dim() 一致）
	MetricType entity.MetricType // 距离度量，默认 COSINE
	NProbe     int               // 搜索时的 nprobe，默认 16
}

// Store Milvus 向量检索后端。
type Store struct {
	cfg      Config
	client   client.Client
	embedder rag.Embedder
	// iterFactory 对账查询游标工厂（oagentkit/rag#137）：nil 走 SDK
	// QueryIterator；测试注入桩以验证 Sweep 对账逻辑，不依赖真库。
	iterFactory func(ctx context.Context, opt *client.QueryIteratorOption) (milvusRowIterator, error)
}

// 本后端满足根包的检索契约（Chunk/Filter 形态与接口形状均取自根包）。
var _ rag.KnowledgeService = (*Store)(nil)

// withDefaults 填充缺省配置（纯函数，构造器与测试共用）。
func (c Config) withDefaults(dim int) Config {
	if c.Collection == "" {
		c.Collection = milvusDefaultCollection
	}
	if c.Dimension == 0 {
		c.Dimension = dim
	}
	if c.MetricType == "" {
		c.MetricType = entity.COSINE
	}
	if c.NProbe <= 0 { // 负数同样回退默认（透传给 SDK 会被运行期拒绝）
		c.NProbe = 16
	}
	return c
}

// NewStore 创建 Milvus 向量检索后端。embedder 为 nil 返回 error
// （rag#34：此前直接 embedder.Dim() 构造期 nil 解引用 panic，且 doc 未声明——
// 包内其他构造面均返回 error，本构造器对齐）。
func NewStore(ctx context.Context, cfg Config, embedder rag.Embedder) (*Store, error) {
	if embedder == nil {
		return nil, errors.New("milvus: embedder 不能为空")
	}
	cfg = cfg.withDefaults(embedder.Dim())

	c, err := client.NewClient(ctx, client.Config{Address: cfg.Address})
	if err != nil {
		return nil, fmt.Errorf("milvus: 连接失败 %s: %w", cfg.Address, err)
	}

	s := &Store{cfg: cfg, client: c, embedder: embedder}

	// 自动建表
	if err := s.ensureCollection(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}

	return s, nil
}

// ensureCollection 确保集合存在，不存在则创建。
func (s *Store) ensureCollection(ctx context.Context) error {
	has, err := s.client.HasCollection(ctx, s.cfg.Collection)
	if err != nil {
		return fmt.Errorf("milvus: 检查集合失败: %w", err)
	}
	if has {
		return s.verifyExistingCollection(ctx)
	}

	schema := entity.NewSchema().
		WithName(s.cfg.Collection).
		WithDescription("agentkit knowledge base").
		// 主键 = file + "#chunk_N"：上限须 ≥ file 上限 + 后缀余量（此前 128
		// 与 file 字段 512 不一致，深路径文件的 Upsert 必因主键超长失败）
		WithField(entity.NewField().WithName(milvusIDField).WithDataType(entity.FieldTypeVarChar).WithMaxLength(milvusIDMaxLen).WithIsPrimaryKey(true)).
		WithField(entity.NewField().WithName(milvusContentField).WithDataType(entity.FieldTypeVarChar).WithMaxLength(milvusContentMaxLen)).
		WithField(entity.NewField().WithName(milvusFileField).WithDataType(entity.FieldTypeVarChar).WithMaxLength(rag.FileStoredMaxLen)).
		WithField(entity.NewField().WithName(milvusHeadingField).WithDataType(entity.FieldTypeVarChar).WithMaxLength(milvusHeadingMaxLen)).
		WithField(entity.NewField().WithName(milvusVectorField).WithDataType(entity.FieldTypeFloatVector).WithDim(int64(s.cfg.Dimension)))

	if err := s.client.CreateCollection(ctx, schema, entity.DefaultShardNumber); err != nil {
		return fmt.Errorf("milvus: 创建集合失败: %w", err)
	}

	// 创建向量索引
	idx, err := entity.NewIndexIvfFlat(s.cfg.MetricType, 128)
	if err != nil {
		return fmt.Errorf("milvus: 创建索引参数失败: %w", err)
	}
	if err := s.client.CreateIndex(ctx, s.cfg.Collection, milvusVectorField, idx, false); err != nil {
		return fmt.Errorf("milvus: 创建索引失败: %w", err)
	}

	// 加载集合到内存
	if err := s.client.LoadCollection(ctx, s.cfg.Collection, false); err != nil {
		return fmt.Errorf("milvus: 加载集合失败: %w", err)
	}

	return nil
}

// verifyExistingCollection 存在路径的形状校验（oagentkit/rag#97）：集合由旧版本
// /外部工具建出时「存在即返回」会让构造期全绿，首次读写才在 Milvus 服务端深处
// 报错（rag#43 修「错误埋在服务端深处」只覆盖了创建路径）。三件事：
//
//  1. 向量字段维度与本仓配置一致（dim 漂移 = 写入/检索期必炸，且写入期炸点在
//     IndexQuiet 的 delete-已执行窗口——文件从检索面消失）；dim 参数缺失或
//     不可解析时无法确认一致性，与漂移同档拒绝（#117 收口两条 fail-open 旁路）；
//  2. 主键/字段形状与本仓 schema 兼容（字段名集合 + 主键字段名 + 主键
//     max_length）——旧 id MaxLen=128 之类的形状漂移在此显式暴露而非写入期
//     深报（#118 补 TypeParams 比对）；
//  3. 集合已加载——外部建而不 load / 手工 release 过的集合，Search 直接
//     "collection not loaded"，未加载时补 LoadCollection（与创建路径同语义）；
//  4. 向量索引面与本仓检索参数一致（oagentkit/rag#134）——searchChunks 固定发
//     IVF_FLAT 的 nprobe 参数并携带 cfg.MetricType，索引缺失/类型不符/metric
//     不符都会让构造期全绿、首次 Search 才在服务端深处报错。
func (s *Store) verifyExistingCollection(ctx context.Context) error {
	col, err := s.client.DescribeCollection(ctx, s.cfg.Collection)
	if err != nil {
		return fmt.Errorf("milvus: 描述已有集合失败: %w", err)
	}
	var vec *entity.Field
	var pk *entity.Field
	names := make(map[string]bool, len(col.Schema.Fields))
	for _, f := range col.Schema.Fields {
		names[f.Name] = true
		if f.Name == milvusVectorField {
			vec = f
		}
		if f.PrimaryKey {
			pk = f
		}
	}
	if vec == nil {
		return fmt.Errorf("milvus: 已有集合 %q 缺向量字段 %s（外部建的集合与本仓 schema 不兼容，换集合名或清理后重建）",
			s.cfg.Collection, milvusVectorField)
	}
	if pk == nil || pk.Name != milvusIDField {
		pkName := ""
		if pk != nil {
			pkName = pk.Name
		}
		return fmt.Errorf("milvus: 已有集合 %q 主键字段为 %q，本仓 schema 主键是 %q（外部建的集合与本仓 schema 不兼容，换集合名或清理后重建）",
			s.cfg.Collection, pkName, milvusIDField)
	}
	for _, want := range []string{milvusContentField, milvusFileField, milvusHeadingField} {
		if !names[want] {
			return fmt.Errorf("milvus: 已有集合 %q 缺字段 %s（外部建的集合与本仓 schema 不兼容，换集合名或清理后重建）",
				s.cfg.Collection, want)
		}
	}
	// dim 校验（#117 收口 fail-open）：缺 dim 参数与 dim 不可解析此前都让整块
	// 跳过——#97 修的是「完全没有校验」，这两条旁路让「校验存在但对两类输入
	// 不设防」。无法确认维度一致性 = 与漂移同档拒绝，不做放行。
	dimStr, ok := vec.TypeParams[entity.TypeParamDim]
	if !ok {
		return fmt.Errorf("milvus: 已有集合 %q 向量字段 %s 缺 dim 参数，无法确认维度一致性（形状漂移拒绝带病运行，与维度不一致同档）",
			s.cfg.Collection, milvusVectorField)
	}
	got, err := strconv.Atoi(dimStr)
	if err != nil {
		return fmt.Errorf("milvus: 已有集合 %q 向量字段 dim=%q 不可解析，无法确认维度一致性（形状漂移拒绝带病运行）",
			s.cfg.Collection, dimStr)
	}
	if got != s.cfg.Dimension {
		return fmt.Errorf("milvus: 已有集合 %q 向量维度 %d 与配置维度 %d 不一致（dim 漂移会在写入期炸掉并触发 delete-已执行窗口，拒绝带病运行）",
			s.cfg.Collection, got, s.cfg.Dimension)
	}
	// 主键 max_length 校验（#118：契约三件事之 2 承诺的形状漂移暴露）：
	// 期望值取创建路径写入的同源常量 milvusIDMaxLen（避免两处字面量漂移）。
	// 判据取「小于本仓上限」而非全等：写入侧由 milvusRowID 把 id 钉在
	// milvusIDMaxLen 内，**更小**的 max_length（旧 Milvus 默认 128 之类）会让
	// 本仓 id 被服务端拒绝——这才是「与本仓 schema 不兼容」；更大的值只是
	// 余量富余（外部建集合常见 65535），不构成写入风险，全等判会把能正常
	// 工作的集合误拦在构造期。
	maxLenStr, hasMaxLen := pk.TypeParams[entity.TypeParamMaxLength]
	if hasMaxLen {
		maxLen, err := strconv.Atoi(maxLenStr)
		if err != nil {
			return fmt.Errorf("milvus: 已有集合 %q 主键 max_length=%q 不可解析，无法确认主键宽度（形状漂移拒绝带病运行）",
				s.cfg.Collection, maxLenStr)
		}
		if maxLen < milvusIDMaxLen {
			return fmt.Errorf("milvus: 已有集合 %q 主键 max_length=%d 小于本仓所需 %d（旧 id MaxLen 之类的形状漂移，长 id 会在写入期被服务端拒绝，拒绝带病运行）",
				s.cfg.Collection, maxLen, milvusIDMaxLen)
		}
	}
	// 缺 max_length 不拦：Milvus 对 VarChar 恒回填该参数（服务端缺省 65535 ≥
	// milvusIDMaxLen，本仓 id 又被 milvusRowID 钉在 milvusIDMaxLen 内，写入
	// 安全），缺失按服务端缺省判定为兼容——见 #118 的判据方向说明。

	// 索引面校验（rag#134）：DescribeCollection 看不到索引存在性/类型/metric。
	if err := s.verifyVectorIndex(ctx); err != nil {
		return err
	}

	// 未加载补 Load：外部建而不 load / 手工 release 过的集合，Search 直接
	// "collection not loaded"——与创建路径的收尾加载同语义。
	state, err := s.client.GetLoadState(ctx, s.cfg.Collection, nil)
	if err != nil {
		return fmt.Errorf("milvus: 查询已有集合加载态失败: %w", err)
	}
	if state != entity.LoadStateLoaded {
		if err := s.client.LoadCollection(ctx, s.cfg.Collection, false); err != nil {
			return fmt.Errorf("milvus: 已有集合未加载，补加载失败: %w", err)
		}
	}
	return nil
}

// verifyVectorIndex 索引面校验（oagentkit/rag#134）：已有集合的向量索引缺失、
// index type 不是 IVF_FLAT、或 metric 与 cfg.MetricType 不一致时，构造期必须
// 拒绝——本仓 searchChunks 固定按 IVF_FLAT 的 nprobe 参数搜索并携带
// cfg.MetricType，这三类差异 DescribeCollection 都看不到，首次 Retrieve 才会
// 由服务端深处报错，错误文案也不指向本仓配置项。
//
// metric 判定大小写不敏感（Milvus 度量名大小写不敏感）；metric_type 参数缺失
// = 无法确认一致性，与漂移同档拒绝（对齐 dim 的 fail-closed 判据，#117）。
func (s *Store) verifyVectorIndex(ctx context.Context) error {
	indexes, err := s.client.DescribeIndex(ctx, s.cfg.Collection, milvusVectorField)
	if err != nil {
		return fmt.Errorf("milvus: 描述已有集合索引失败: %w", err)
	}
	if len(indexes) == 0 {
		return fmt.Errorf("milvus: 已有集合 %q 向量字段 %s 无索引（本仓固定按 IVF_FLAT 检索，无索引/索引缺失会在首次 Search 于服务端深处报错，换集合名或清理后重建）",
			s.cfg.Collection, milvusVectorField)
	}
	wantMetric := s.cfg.MetricType
	if wantMetric == "" {
		// 与 withDefaults 同口径：直接构造 store 的路径（测试/包内装配）兜底。
		wantMetric = entity.COSINE
	}
	observed := make([]string, 0, len(indexes))
	for _, idx := range indexes {
		metric := idx.Params()[milvusMetricTypeParam]
		observed = append(observed, fmt.Sprintf("%s(index_type=%s, metric_type=%s)", idx.Name(), idx.IndexType(), metric))
		if idx.IndexType() != entity.IvfFlat || metric == "" {
			continue
		}
		if strings.EqualFold(metric, string(wantMetric)) {
			return nil
		}
	}
	if len(indexes) == 1 && indexes[0].IndexType() == entity.IvfFlat && indexes[0].Params()[milvusMetricTypeParam] == "" {
		return fmt.Errorf("milvus: 已有集合 %q 的 IVF_FLAT 索引 %q 缺 metric_type 参数，无法确认度量一致性（与度量漂移同档拒绝带病运行，换集合名或清理后重建）",
			s.cfg.Collection, indexes[0].Name())
	}
	return fmt.Errorf("milvus: 已有集合 %q 的向量索引面与本仓检索参数不兼容（需要 IVF_FLAT 且 metric_type=%s，实际 %s）——HNSW/L2/无匹配索引会在首次 Search 于服务端深处报错，换集合名或清理后重建",
		s.cfg.Collection, wantMetric, strings.Join(observed, "; "))
}

// Index 将文档分块并索引到 Milvus（单文件写=embed→delete→upsert→flush）。
// 批量写入请用 IndexQuiet+收尾 Flush——per-file Flush 实测 ~20s/文件（对象存储
// 落盘 RPC 主导，向量计算亚秒），千级文件会把全量重索引拖成小时级（2026-10-08
// bianque 对拍实弹）。
// 幂等：成功路径上先清该文件旧数据再写入——文件变短（删章节/内容精简）时
// chunk 数变少，只 Upsert 新块会残留 chunk_M..N-1 旧行携带过期内容参与检索
// 挤占 topK。**与 Local.Rescan 重建的等价只覆盖同一文件的内容变更**
// （oagentkit/rag#137）：文件被删除时 Local 由全量重建自动剔除，而本后端没有
// 自动感知删除的通道，删掉的文件其行会永不过期——需要调用方在全量重扫后调
// Sweep 传入存活文件清单对账（此前 doc 泛称「与 Local.Rescan 的全量重建同
// 语义」，删除面不成立）。
// 顺序：chunk → Embed 成功 → Delete → Upsert。Embed 是外部网络调用（抖动/
// 限流/超时是常规事件），先删后嵌会把旧索引一并清掉；倒序后最常见的失败
// （embedding）不再动旧数据。残余窗口：Delete 成功而 Upsert/Flush 失败时旧
// 数据已删新数据未写（该文件暂时从检索面消失，直到下次 Index 重建）——
// 该窗口远小于原形态且失败以错误上抛，调用方可重试。
func (s *Store) Index(ctx context.Context, file string, content string) error {
	if err := s.IndexQuiet(ctx, file, content); err != nil {
		return err
	}
	return s.Flush(ctx)
}

// IndexQuiet 单文件写入但不 Flush（批量收尾点由调用方 Flush 一次）。数据在
// flush 前已可检索（growing segments），Flush 语义=持久化到 sealed segments——
// 调用方须保证批次终态调 Flush，否则进程崩溃窗内未 flush 写入丢失（重跑幂等）。
func (s *Store) IndexQuiet(ctx context.Context, file string, content string) error {
	chunks := rag.ChunkMarkdown(content)
	if len(chunks) == 0 {
		// 无新内容：清旧即全量（文件已空，索引中不残留该文件数据）
		if err := s.DeleteFile(ctx, file); err != nil {
			return fmt.Errorf("milvus: 清理旧索引失败: %w", err)
		}
		return nil
	}
	rows, err := indexRows(file, chunks)
	if err != nil {
		return fmt.Errorf("milvus: 索引行准备失败: %w", err)
	}
	texts := make([]string, len(rows))
	for i, r := range rows {
		texts[i] = r.content
	}

	// 批量 embedding（在动旧数据之前完成——失败时旧索引原样保留）
	vectors, err := s.embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("milvus: embedding 失败: %w", err)
	}
	if err := s.DeleteFile(ctx, file); err != nil {
		return fmt.Errorf("milvus: 清理旧索引失败: %w", err)
	}
	return s.upsertChunks(ctx, rows, vectors)
}

// Flush 持久化本集合全部未落盘写入（批量写入收尾点）。
func (s *Store) Flush(ctx context.Context) error {
	if err := s.client.Flush(ctx, s.cfg.Collection, false); err != nil {
		return fmt.Errorf("milvus: flush 失败: %w", err)
	}
	return nil
}

// milvusRow 单块索引行（id/content/heading/file 一体——此前四个同长切片靠
// 约定对齐，存在下标错位面；收口轮结构化）。
type milvusRow struct {
	id      string // 主键 file#chunk_N（确定性，同文件重刷可 Upsert 覆盖）
	content string
	heading string
	file    string
}

// milvusRowID 主键构造 + 字节守卫：主键不可截断——截断=跨文件碰撞覆盖，超限
// fail-fast 报错并注明字段与上限，而非把失败埋进 Milvus 服务端报错深处（rag#43）。
// ID 形态取根包 rag.ChunkID 单源——与 Local 路同格式（rag#59 跨路归并的前提）。
func milvusRowID(fileStored string, seq int) (string, error) {
	id := rag.ChunkID(fileStored, seq)
	if len(id) > milvusIDMaxLen {
		return "", fmt.Errorf("milvus: 主键 id %d 字节超过 VarChar 上限 %d（file=%q, chunk=%d），主键不可截断", len(id), milvusIDMaxLen, fileStored, seq)
	}
	return id, nil
}

// indexRows 索引行准备——所有 VarChar 标量字段按字节守卫（rag#43）：content
// 由 chunker 的 maxChunkRunes 保证（≤4000 rune ≤16000 字节），并按字节独立
// 兜底截断（issue#85：围栏行豁免曾让单块 70003 字节——正确性外包给 chunker
// 不变量已被证伪一次，守卫就地兜底不再假设上游恒真；超限告警后照常落库，
// 避免错误埋进 Milvus 服务端深处、整文件 Upsert 被拒）；heading 超限按 rune
// 边界截断（标题仅是展示/过滤元数据，内容全文保留在 content 字段）；file
// 超限截断+散列后缀保唯一；id 超限报错。
//
// 切块输入取根包 rag.ChunkMarkdown 的 Chunk 形态（切块规则单源），身份键取
// rag.FileStored / rag.ChunkID（身份形态单源）——本子包不另写一份。
func indexRows(file string, chunks []rag.Chunk) ([]milvusRow, error) {
	file = rag.FileStored(file)
	rows := make([]milvusRow, len(chunks))
	for i, c := range chunks {
		id, err := milvusRowID(file, i)
		if err != nil {
			return nil, err
		}
		content := c.Content
		if len(content) > milvusContentMaxLen {
			milvusLog.Warn("agentkit.rag.milvus_content_overlimit",
				"chunk", i, "bytes", len(content), "max", milvusContentMaxLen)
			content = rag.TruncateUTF8(content, milvusContentMaxLen)
		}
		rows[i] = milvusRow{
			id:      id,
			content: content,
			heading: rag.TruncateUTF8(c.Metadata["heading"], milvusHeadingMaxLen),
			file:    file,
		}
	}
	return rows, nil
}

// upsertChunks 列构建 + Upsert（写入收尾阶段，与 Embed/Delete 解耦；Flush 由
// Index/调用方批末统一执行）。
func (s *Store) upsertChunks(ctx context.Context, rows []milvusRow, vectors [][]float32) error {
	ids := make([]string, len(rows))
	texts := make([]string, len(rows))
	files := make([]string, len(rows))
	headings := make([]string, len(rows))
	for i, r := range rows {
		ids[i], texts[i], files[i], headings[i] = r.id, r.content, r.file, r.heading
	}
	idCol := entity.NewColumnVarChar(milvusIDField, ids)
	contentCol := entity.NewColumnVarChar(milvusContentField, texts)
	fileCol := entity.NewColumnVarChar(milvusFileField, files)
	headingCol := entity.NewColumnVarChar(milvusHeadingField, headings)
	vectorCol := entity.NewColumnFloatVector(milvusVectorField, s.cfg.Dimension, vectors)

	// Upsert 而非 Insert：主键确定性（file#chunk_N），重复 Index 同一文件时
	// 覆盖旧行——否则消费方每次重启重刷都会累积重复主键行挤占 topK 名额
	if _, err := s.client.Upsert(ctx, s.cfg.Collection, "", idCol, contentCol, fileCol, headingCol, vectorCol); err != nil {
		return fmt.Errorf("milvus: 插入失败: %w", err)
	}
	return nil
}

// DeleteFile 删除指定文件的所有索引数据。身份按落库形态归一（rag#59）：
// 表达式必须命中截断+散列落库形态与调用方原始路径两种形态——此前只按 raw
// 路径拼表达式，对超限截断落库的行永远命不中，删除静默 no-op，每次重索引
// 累积 stale 行污染检索。
func (s *Store) DeleteFile(ctx context.Context, file string) error {
	return s.client.Delete(ctx, s.cfg.Collection, "", milvusDeleteExpr(file))
}

// milvusDeleteExpr 删除表达式：命中 file 字段的两种身份形态——落库形态
// （rag.FileStored，indexRows 写入的即此形态）与原始路径（迁移期兜底历史/
// 外部写入的 raw 行；表达式命不中的删除是静默 no-op）。限内路径两形态同串，
// 退化为单条件等值（形态与历史版本一致）。
func milvusDeleteExpr(file string) string {
	stored := rag.FileStored(file)
	if stored == file {
		return fmt.Sprintf(`%s == "%s"`, milvusFileField, escapeMilvus(file))
	}
	return fmt.Sprintf(`(%s == "%s" or %s == "%s")`,
		milvusFileField, escapeMilvus(stored), milvusFileField, escapeMilvus(file))
}

// milvusRowIterator 对账查询游标的最小读面（client.QueryIterator 满足，
// 测试可注入桩，oagentkit/rag#137）。
type milvusRowIterator interface {
	Next(ctx context.Context) (client.ResultSet, error)
}

// Sweep 对账删除：集合中 file 不在 liveFiles 里的行全部删除（oagentkit/rag#137）。
//
// Index/IndexQuiet 的「先清后写」只覆盖**同一文件的内容变更**；文件被删除后
// 没有任何入口能让它的行过期（Local.Rescan 是全量重建、会自动剔除，向量后端
// 没有对应面）。调用方在全量重扫后传入「当前仍存在的文件清单」调用本入口，
// 删除面即与 Local.Rescan 对齐；Hybrid 装配下不调用会让 vector 路持续返回
// 幽灵片段，且 RRF 会把跨路幽灵当共识放大。
//
// liveFiles 用调用方原始路径（内部归一到落库形态 rag.FileStored，超限深
// 路径也能命中）；空清单表示集合应清空。只删 file 维度——调用方必须保证
// 清单完整，漏传 = 误删。删除走 DeleteFile（含 raw/落库双形态表达式）。
func (s *Store) Sweep(ctx context.Context, liveFiles []string) error {
	live := make(map[string]struct{}, len(liveFiles))
	for _, f := range liveFiles {
		live[rag.FileStored(f)] = struct{}{}
	}
	stale, err := s.staleFiles(ctx, live)
	if err != nil {
		return err
	}
	files := make([]string, 0, len(stale))
	for f := range stale {
		files = append(files, f)
	}
	slices.Sort(files) // 删除顺序确定：中途失败时已删集合可复现、日志可比对
	for _, f := range files {
		if err := s.DeleteFile(ctx, f); err != nil {
			return fmt.Errorf("milvus: 对账删除文件 %q 失败: %w", f, err)
		}
	}
	return nil
}

// staleFiles 游标遍历集合中出现的全部 file 值，返回不在 live 里的集合。
func (s *Store) staleFiles(ctx context.Context, live map[string]struct{}) (map[string]struct{}, error) {
	it, err := s.newRowIterator(ctx)
	if err != nil {
		return nil, fmt.Errorf("milvus: 对账查询初始化失败: %w", err)
	}
	stale := map[string]struct{}{}
	for {
		rs, err := it.Next(ctx)
		if errors.Is(err, io.EOF) {
			return stale, nil
		}
		if err != nil {
			return nil, fmt.Errorf("milvus: 对账查询失败: %w", err)
		}
		var fileCol entity.Column
		for _, col := range rs {
			if col.Name() == milvusFileField {
				fileCol = col
				break
			}
		}
		if fileCol == nil {
			return nil, fmt.Errorf("milvus: 对账查询结果缺 %s 列", milvusFileField)
		}
		for i := 0; i < fileCol.Len(); i++ {
			f, err := fileCol.GetAsString(i)
			if err != nil {
				return nil, fmt.Errorf("milvus: 对账读取 %s 列失败: %w", milvusFileField, err)
			}
			if _, ok := live[f]; !ok {
				stale[f] = struct{}{}
			}
		}
	}
}

// newRowIterator 打开对账游标（SDK QueryIterator 分批读全集合 file 列）。
func (s *Store) newRowIterator(ctx context.Context) (milvusRowIterator, error) {
	opt := client.NewQueryIteratorOption(s.cfg.Collection).
		WithExpr(fmt.Sprintf(`%s != ""`, milvusIDField)).
		WithOutputFields(milvusFileField).
		WithBatchSize(milvusSweepBatchSize)
	if s.iterFactory != nil {
		return s.iterFactory(ctx, opt)
	}
	return s.client.QueryIterator(ctx, opt)
}

// Retrieve 实现 rag.KnowledgeService 接口：向量相似度检索。
func (s *Store) Retrieve(ctx context.Context, query string, topK int, filter rag.Filter) ([]rag.Chunk, error) {
	if topK <= 0 {
		topK = rag.DefaultTopK
	}
	vec, err := s.embedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	expr, err := milvusFilterExpr(filter)
	if err != nil {
		return nil, err
	}
	results, err := s.searchChunks(ctx, expr, vec, topK)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return milvusHitChunks(results[0]), nil
}

// embedQuery 查询文本向量化（空向量=服务/配置异常，显式报错不静默空检索）。
func (s *Store) embedQuery(ctx context.Context, query string) ([]float32, error) {
	vectors, err := s.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("milvus: 查询 embedding 失败: %w", err)
	}
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return nil, fmt.Errorf("milvus: 查询 embedding 为空")
	}
	return vectors[0], nil
}

// milvusFilterExpr 构建过滤表达式：key 白名单校验走 rag.ValidateFilter 单源
// （Local/向量后端同一契约），value 已转义。file 过滤值归一到落库形态（rag#59）：
// 列里存的是 rag.FileStored 形态，caller 传原始路径直接比对对超限路径恒空
// ——查询形态与存储形态必须在边界统一。heading 不归一（展示元数据、无身份
// 语义）：归一会把超限标题的过滤静默放宽为截断前缀匹配。
func milvusFilterExpr(filter rag.Filter) (string, error) {
	if err := rag.ValidateFilter(filter); err != nil {
		return "", err
	}
	if filter == nil {
		return "", nil
	}
	var parts []string
	for k, v := range filter {
		if k == milvusFileField {
			v = rag.FileStored(v)
		}
		parts = append(parts, fmt.Sprintf(`%s == "%s"`, k, escapeMilvus(v)))
	}
	return strings.Join(parts, " and "), nil
}

// searchChunks 执行向量检索（参数构造错误显式返回——吞掉后 nil sp 的报错晦涩难排查）。
func (s *Store) searchChunks(ctx context.Context, expr string, vec []float32, topK int) ([]client.SearchResult, error) {
	sp, err := entity.NewIndexIvfFlatSearchParam(s.cfg.NProbe)
	if err != nil {
		return nil, fmt.Errorf("milvus: 搜索参数构造失败（nprobe=%d）: %w", s.cfg.NProbe, err)
	}
	results, err := s.client.Search(ctx, s.cfg.Collection, nil, expr,
		[]string{milvusIDField, milvusContentField, milvusFileField, milvusHeadingField},
		[]entity.Vector{entity.FloatVector(vec)}, milvusVectorField, s.cfg.MetricType, topK, sp)
	if err != nil {
		return nil, fmt.Errorf("milvus: 搜索失败: %w", err)
	}
	return results, nil
}

// milvusHitChunks 检索命中行 → rag.Chunk（列缺失取空串，score 越界取 0）。
// 回传主键到 Chunk.ID：Hybrid 的 RRF 融合按 ID 跨路归并共识——此前命中 chunk
// ID 恒空，只能靠内容回退 key，与 Local 路（file#chunk_N）对不上号，跨路共识
// 加权失效（rag#40）。
func milvusHitChunks(res client.SearchResult) []rag.Chunk {
	chunks := make([]rag.Chunk, 0, res.ResultCount)

	var idCol *entity.ColumnVarChar
	var contentCol *entity.ColumnVarChar
	var fileCol *entity.ColumnVarChar
	var headingCol *entity.ColumnVarChar

	for _, field := range res.Fields {
		switch field.Name() {
		case milvusIDField:
			idCol, _ = field.(*entity.ColumnVarChar)
		case milvusContentField:
			contentCol, _ = field.(*entity.ColumnVarChar)
		case milvusFileField:
			fileCol, _ = field.(*entity.ColumnVarChar)
		case milvusHeadingField:
			headingCol, _ = field.(*entity.ColumnVarChar)
		}
	}

	scores := res.Scores
	for i := range res.ResultCount {
		content := columnValueAt(contentCol, i)
		file := columnValueAt(fileCol, i)
		heading := columnValueAt(headingCol, i)

		var score float64
		if i < len(scores) {
			score = float64(scores[i])
		}

		chunks = append(chunks, rag.Chunk{
			ID:      columnValueAt(idCol, i),
			Content: content,
			Score:   score,
			Metadata: map[string]string{
				"file":    file,
				"heading": heading,
			},
		})
	}
	return chunks
}

// AsTool 实现 rag.KnowledgeService 接口。
//
// ⚠️ panic 契约（oagentkit/rag#31）：schema 反射构造失败时 panic（接口签名
// 无 error 通道，构造失败视为编程错误）；需要 error 通道的装配路径用
// rag.NewSearchTool。
func (s *Store) AsTool() tool.BaseTool {
	t, err := rag.NewSearchTool(s)
	if err != nil {
		panic(err)
	}
	return t
}

// Close 关闭 Milvus 连接。ctx 为生命周期契约参数（oekit Close(ctx) error
// 同型）：SDK 的 client.Close 是立即路径，ctx 当前不阻塞任何步骤——为宿主
// 收口钩子（bootstrap AddResource 同型）直连而设。
func (s *Store) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// columnValueAt 从VarChar列中安全取值。
func columnValueAt(col *entity.ColumnVarChar, idx int) string {
	if col == nil {
		return ""
	}
	val, err := col.ValueByIdx(idx)
	if err != nil {
		return ""
	}
	return val
}

// escapeMilvus 转义 Milvus 表达式中的特殊字符。
func escapeMilvus(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
