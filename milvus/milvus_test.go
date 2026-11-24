package milvus

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"

	"github.com/oagentkit/rag"
	"github.com/oekit/pkg/errors"
)

// mockEmbedder 测试用 embedding 实现：返回固定维度的伪向量。
type mockEmbedder struct {
	dim int
}

func (m *mockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i, text := range texts {
		vec := make([]float32, m.dim)
		// 简单哈希：每个字符的 ASCII 值影响不同维度
		for j, ch := range text {
			vec[j%m.dim] += float32(ch) / 1000.0
		}
		result[i] = vec
	}
	return result, nil
}

func (m *mockEmbedder) Dim() int { return m.dim }

func TestMockEmbedder(t *testing.T) {
	m := &mockEmbedder{dim: 8}
	vecs, err := m.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 {
		t.Fatalf("应返回 2 个向量，得到 %d", len(vecs))
	}
	if len(vecs[0]) != 8 {
		t.Fatalf("向量维度应为 8，得到 %d", len(vecs[0]))
	}
	// 相同文本应产生相同向量
	vecs2, _ := m.Embed(context.Background(), []string{"hello"})
	if vecs[0][0] != vecs2[0][0] {
		t.Fatal("相同文本应产生相同向量")
	}
	// 不同文本应产生不同向量
	if vecs[0][0] == vecs[1][0] && vecs[0][1] == vecs[1][1] {
		// 可能巧合相同，但至少一个维度应不同
		same := true
		for i := range vecs[0] {
			if vecs[0][i] != vecs[1][i] {
				same = false
				break
			}
		}
		if same {
			t.Fatal("不同文本不应产生完全相同的向量")
		}
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := Config{Address: "localhost:19530"}
	m := &mockEmbedder{dim: 64}

	// 默认值填充走生产代码的 withDefaults 纯函数（不再手抄实现）
	cfg = cfg.withDefaults(m.Dim())

	if cfg.Collection != "agentkit_knowledge" {
		t.Fatalf("默认集合名应为 agentkit_knowledge，得到 %s", cfg.Collection)
	}
	if cfg.Dimension != 64 {
		t.Fatalf("维度应从 embedder 获取，得到 %d", cfg.Dimension)
	}
}

func TestEscapeMilvus(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{`he"llo`, `he\"llo`},
		{`he\llo`, `he\\llo`},
		{"", ""},
	}
	for _, c := range cases {
		got := escapeMilvus(c.input)
		if got != c.want {
			t.Errorf("escapeMilvus(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// 构造期 nil embedder 返回 error（rag#34）：此前直接 embedder.Dim() nil 解引用
// panic——包内其他构造面均返回 error，本构造器对齐。
func TestNewStoreNilEmbedder(t *testing.T) {
	if _, err := NewStore(context.Background(), Config{Address: "127.0.0.1:19530"}, nil); err == nil {
		t.Fatal("nil embedder 应返回 error 而非 panic")
	}
}

// TestMilvusHitChunksCarriesID 检索命中行回传稳定主键（rag#40）：Hybrid 的 RRF
// 融合按 ID 跨路归并双路共识——此前 Milvus 路命中 chunk ID 恒空，只能靠内容
// 回退 key，与 Local 路（file#chunk_N）对不上号，跨路共识加权失效。
func TestMilvusHitChunksCarriesID(t *testing.T) {
	res := client.SearchResult{
		ResultCount: 2,
		Fields: []entity.Column{
			entity.NewColumnVarChar(milvusIDField, []string{"a.md#chunk_0", "a.md#chunk_1"}),
			entity.NewColumnVarChar(milvusContentField, []string{"内容零", "内容一"}),
			entity.NewColumnVarChar(milvusFileField, []string{"a.md", "a.md"}),
			entity.NewColumnVarChar(milvusHeadingField, []string{"标题", "标题"}),
		},
		Scores: []float32{0.9, 0.8},
	}
	chunks := milvusHitChunks(res)
	if len(chunks) != 2 {
		t.Fatalf("应 2 条: %d", len(chunks))
	}
	for i, c := range chunks {
		if want := fmt.Sprintf("a.md#chunk_%d", i); c.ID != want {
			t.Fatalf("chunk[%d].ID=%q want %q", i, c.ID, want)
		}
		if c.Metadata["file"] != "a.md" || c.Score == 0 {
			t.Fatalf("chunk[%d] 元数据/分值不符: %+v", i, c)
		}
	}
}

// TestIndexRowsVarCharGuards 回归（rag#43）：所有 VarChar 标量字段按字节守卫。
// 此前守卫只覆盖 content（chunker maxChunkRunes），CJK 标题 ≥171 字（513 字节）
// 或深路径 file 超 512 字节时，错误埋在 Milvus 服务端报错深处、整文件 Upsert
// 失败（含该节全部正常 chunk）。
func TestIndexRowsVarCharGuards(t *testing.T) {
	// CJK 超长标题：600 字节 > 512——按 rune 边界截断（不劈多字节字符），
	// 内容全文保留在 content 字段
	md := "# " + strings.Repeat("标", 200) + "\n\n正文。\n"
	rows, err := indexRows("doc.md", rag.ChunkMarkdown(md))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("应 1 行: %d", len(rows))
	}
	if got := len(rows[0].heading); got > milvusHeadingMaxLen {
		t.Fatalf("heading %d 字节超限 %d", got, milvusHeadingMaxLen)
	}
	full := strings.Repeat("标", 200)
	if !utf8.ValidString(rows[0].heading) || !strings.HasPrefix(full, rows[0].heading) {
		t.Fatalf("heading 截断应保前缀且不破坏 UTF-8: %q", rows[0].heading)
	}
	if rows[0].content != "正文。" {
		t.Fatalf("content 不受守卫影响: %q", rows[0].content)
	}

	// 深路径 file 超限（CJK 路径 >512 字节）：截到上限内并附散列后缀，主键同步有界
	deep1 := strings.Repeat("深目录/", 128) + "one.md"
	deep2 := strings.Repeat("深目录/", 128) + "two.md"
	r1, err := indexRows(deep1, rag.ChunkMarkdown("# 甲\n\n正文一。\n"))
	if err != nil {
		t.Fatal(err)
	}
	r2, err := indexRows(deep2, rag.ChunkMarkdown("# 乙\n\n正文二。\n"))
	if err != nil {
		t.Fatal(err)
	}
	for i, rs := range [][]milvusRow{r1, r2} {
		for j, row := range rs {
			if got := len(row.file); got > rag.FileStoredMaxLen {
				t.Fatalf("rows[%d][%d] file %d 字节超限 %d", i, j, got, rag.FileStoredMaxLen)
			}
			if got := len(row.id); got > milvusIDMaxLen {
				t.Fatalf("rows[%d][%d] id %d 字节超限 %d", i, j, got, milvusIDMaxLen)
			}
			if !utf8.ValidString(row.file) {
				t.Fatalf("file 截断破坏 UTF-8: %q", row.file)
			}
		}
	}
	// 共享长前缀的两个深路径：截断+散列后缀保持 file 与主键互异（朴素截断
	// 会让两文件主键碰撞，Upsert 互相覆盖=静默丢数据）
	if r1[0].file == r2[0].file || r1[0].id == r2[0].id {
		t.Fatalf("共享前缀深路径不应碰撞: file1=%q file2=%q id1=%q id2=%q",
			r1[0].file, r2[0].file, r1[0].id, r2[0].id)
	}

	// 短路径原样透传（不截断、不散列），主键保持 file#chunk_N 形态
	r3, err := indexRows("doc.md", rag.ChunkMarkdown("# 丙\n\n正文三。\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r3[0].file != "doc.md" || r3[0].id != "doc.md#chunk_0" {
		t.Fatalf("短路径应原样: %+v", r3[0])
	}
}

// TestIndexRowsContentByteGuard 回归（rag#85）：content 独立字节守卫——此前
// content 的正确性外包给 chunker 的 maxChunkRunes 不变量，围栏行豁免证明该
// 不变量并不成立（单块实测 70003 字节），超限错误埋进 Milvus 服务端报错深处、
// 整文件 Upsert 被拒。对齐 heading/file 的处理：超限按字节边界截断（不劈
// 多字节字符），不再假设上游不变量恒真。
func TestIndexRowsContentByteGuard(t *testing.T) {
	over := strings.Repeat("长", 30000) // 90000 字节 > 65535
	rows, err := indexRows("doc.md", []rag.Chunk{{Content: over, Metadata: map[string]string{"heading": "标题"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("应 1 行: %d", len(rows))
	}
	if got := len(rows[0].content); got > milvusContentMaxLen {
		t.Fatalf("content %d 字节超限 %d", got, milvusContentMaxLen)
	}
	if !utf8.ValidString(rows[0].content) || !strings.HasPrefix(over, rows[0].content) {
		t.Fatal("content 截断应保前缀且不破坏 UTF-8")
	}

	// 限内原样透传（不截断，既有形态不变）
	rows2, err := indexRows("doc.md", []rag.Chunk{{Content: "正文。"}})
	if err != nil {
		t.Fatal(err)
	}
	if rows2[0].content != "正文。" {
		t.Fatalf("限内 content 应原样: %q", rows2[0].content)
	}
}

// TestMilvusRowIDGuard 主键字节守卫（rag#43）：主键不可截断——截断会跨文件
// 碰撞覆盖；超限 fail-fast 报错注明字段与上限。file 存储形态 ≤512 字节时
// id 上界=512+len("#chunk_N")，序号达 10 位才触界（单文件 1e9 块不存在，
// 此处直接构造边界序号验证守卫本身）。
func TestMilvusRowIDGuard(t *testing.T) {
	file := strings.Repeat("a", rag.FileStoredMaxLen)
	// 9 位序号恰好压线：512+7+9=528
	id, err := milvusRowID(file, 999999999)
	if err != nil {
		t.Fatalf("9 位序号应在限内: %v", err)
	}
	if len(id) != milvusIDMaxLen {
		t.Fatalf("9 位序号应恰好压线 %d: %d", milvusIDMaxLen, len(id))
	}
	// 10 位序号越界：报错而非截断（截断=碰撞）
	if _, err := milvusRowID(file, 9999999999); err == nil {
		t.Fatal("主键超限应报错（截断会碰撞）")
	}
}

// fakeDeleteClient 捕获 Delete 表达式的假客户端（DeleteFile 表达式构造的
// 行为面测试——表达式拼错在真实 Milvus 上是静默 no-op，从错误码感知不到）。
type fakeDeleteClient struct {
	client.Client
	exprs []string
}

func (f *fakeDeleteClient) Delete(_ context.Context, _ string, _ string, expr string) error {
	f.exprs = append(f.exprs, expr)
	return nil
}

func newCaptureStore(fc *fakeDeleteClient) *Store {
	return &Store{cfg: Config{Collection: "t"}, client: fc, embedder: &mockEmbedder{dim: 4}}
}

// TestMilvusDeleteFileMatchesStoredForm 回归（rag#59）：超限路径落库为
// 截断+散列形态（indexRows→milvusFileStored），DeleteFile 表达式必须命中该
// 形态——此前按调用方原始路径拼表达式，对已落库行永远命不中，删除静默
// no-op，每次重索引累积 stale 行污染检索。
func TestMilvusDeleteFileMatchesStoredForm(t *testing.T) {
	deep := strings.Repeat("深目录/", 128) + "one.md" // >512 字节深路径
	fc := &fakeDeleteClient{}
	if err := newCaptureStore(fc).DeleteFile(context.Background(), deep); err != nil {
		t.Fatal(err)
	}
	if len(fc.exprs) != 1 {
		t.Fatalf("应执行 1 次删除，得到 %d", len(fc.exprs))
	}
	stored := escapeMilvus(rag.FileStored(deep))
	if !strings.Contains(fc.exprs[0], stored) {
		t.Fatalf("删除表达式未命中落库形态 %q: %s", stored, fc.exprs[0])
	}
	// 迁移期双删：raw 形态也保留在表达式中（兜底历史/外部写入的 raw 行）
	if !strings.Contains(fc.exprs[0], escapeMilvus(deep)) {
		t.Fatalf("删除表达式应保留 raw 形态（迁移双删）: %s", fc.exprs[0])
	}

	// 限内路径 stored == raw：表达式退化为单条件等值，形态与既有一致
	fc2 := &fakeDeleteClient{}
	if err := newCaptureStore(fc2).DeleteFile(context.Background(), "doc.md"); err != nil {
		t.Fatal(err)
	}
	if fc2.exprs[0] != `file == "doc.md"` {
		t.Fatalf("限内路径删除表达式形态不应变化: %s", fc2.exprs[0])
	}
}

// TestMilvusFilterExprFileIdentity 回归（rag#59）：file 过滤值必须归一到落库
// 形态再与列值比对——此前 caller 传原始路径、列里是截断+散列形态，超限路径
// 的 Filter 静默恒空。
func TestMilvusFilterExprFileIdentity(t *testing.T) {
	deep := strings.Repeat("深目录/", 128) + "one.md"
	expr, err := milvusFilterExpr(rag.Filter{"file": deep})
	if err != nil {
		t.Fatal(err)
	}
	stored := escapeMilvus(rag.FileStored(deep))
	if !strings.Contains(expr, stored) {
		t.Fatalf("过滤表达式未归一到落库形态 %q: %s", stored, expr)
	}
	// 限内路径归一为恒等：表达式形态不变
	expr2, err := milvusFilterExpr(rag.Filter{"file": "doc.md"})
	if err != nil {
		t.Fatal(err)
	}
	if expr2 != `file == "doc.md"` {
		t.Fatalf("限内路径过滤表达式形态不应变化: %s", expr2)
	}
}

// TestIndexRowsIDsUseSharedIdentity 回归（rag#59 / oagentkit/rag#156）：主键 ID
// 必须由**根包导出的身份键单源**派生（rag.FileStored + rag.ChunkID）——向量后端
// 与本地索引两路共用同一身份形态，跨路归并（FuseRRF 按 ID）才成立。
// 本用例钉向量后端一侧；本地索引一侧由根包 identity_test.go 的对应用例钉住
// （两路都指向同一单源 ⇒ 跨路一致性由构造保证，不靠两处各写一份）。
func TestIndexRowsIDsUseSharedIdentity(t *testing.T) {
	deep := strings.Repeat("深目录/", 128) + "one.md" // >512 字节 rel 路径
	rows, err := indexRows(deep, rag.ChunkMarkdown("# 甲\n\n正文。\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("应产出至少一行")
	}
	if want := rag.ChunkID(rag.FileStored(deep), 0); rows[0].id != want {
		t.Fatalf("主键 ID 应取根包身份键单源: got=%q want=%q", rows[0].id, want)
	}
	// 限内路径 raw == stored：ID 形态保持不变
	rows2, err := indexRows("doc.md", rag.ChunkMarkdown("# 乙\n\n正文。\n"))
	if err != nil {
		t.Fatal(err)
	}
	if rows2[0].id != "doc.md#chunk_0" {
		t.Fatalf("限内路径 ID 形态不应变化: %q", rows2[0].id)
	}
}

// fakeRowIterator 对账游标桩（oagentkit/rag#137）：按批次吐 ResultSet，
// 耗尽返回 io.EOF（与 client.QueryIterator 契约一致）。
type fakeRowIterator struct {
	batches []client.ResultSet
	err     error
	i       int
}

func (f *fakeRowIterator) Next(_ context.Context) (client.ResultSet, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.i >= len(f.batches) {
		return nil, io.EOF
	}
	b := f.batches[f.i]
	f.i++
	return b, nil
}

func fileBatch(files ...string) client.ResultSet {
	return client.ResultSet{entity.NewColumnVarChar(milvusFileField, files)}
}

func newSweepStore(fc *fakeDeleteClient, iter *fakeRowIterator) *Store {
	s := newCaptureStore(fc)
	s.iterFactory = func(context.Context, *client.QueryIteratorOption) (milvusRowIterator, error) {
		return iter, nil
	}
	return s
}

// TestMilvusSweepDeletesStaleFiles 回归（rag#137）：文件删除面与 Local.Rescan
// 对齐——对账扫描多批游标，只删 file 不在 liveFiles 的行（此前没有任何对账
// 入口，删掉的文件行永不过期，Hybrid 下幽灵片段被 RRF 当跨路共识放大）。
func TestMilvusSweepDeletesStaleFiles(t *testing.T) {
	fc := &fakeDeleteClient{}
	iter := &fakeRowIterator{batches: []client.ResultSet{
		fileBatch("a.md", "b.md"),
		fileBatch("b.md", "c.md"), // 跨批重复的 b.md 只删一次
	}}
	if err := newSweepStore(fc, iter).Sweep(context.Background(), []string{"a.md", "c.md"}); err != nil {
		t.Fatal(err)
	}
	if len(fc.exprs) != 1 {
		t.Fatalf("应只删 stale 文件一次，得到 %d 次: %v", len(fc.exprs), fc.exprs)
	}
	if fc.exprs[0] != `file == "b.md"` {
		t.Fatalf("删除表达式应精确命中 stale 文件: %s", fc.exprs[0])
	}
}

// TestMilvusSweepMatchesStoredIdentity 回归（rag#137/rag#59）：列里存的是落库
// 形态（超限路径截断+散列），liveFiles 传原始路径也必须按同一身份键比对——
// 否则深路径文件每轮都被误判 stale 删除。
func TestMilvusSweepMatchesStoredIdentity(t *testing.T) {
	deep := strings.Repeat("深目录/", 128) + "one.md" // >512 字节
	fc := &fakeDeleteClient{}
	iter := &fakeRowIterator{batches: []client.ResultSet{
		fileBatch(rag.FileStored(deep), "stale.md"),
	}}
	if err := newSweepStore(fc, iter).Sweep(context.Background(), []string{deep}); err != nil {
		t.Fatal(err)
	}
	if len(fc.exprs) != 1 || !strings.Contains(fc.exprs[0], `"stale.md"`) {
		t.Fatalf("只应删除 stale.md（深路径 live 文件按落库形态匹配）: %v", fc.exprs)
	}
}

// TestMilvusSweepEmptyLiveClearsCollection 空清单 = 集合应清空（调用方全量
// 重扫后目录为空），删除顺序确定。
func TestMilvusSweepEmptyLiveClearsCollection(t *testing.T) {
	fc := &fakeDeleteClient{}
	iter := &fakeRowIterator{batches: []client.ResultSet{fileBatch("b.md", "a.md")}}
	if err := newSweepStore(fc, iter).Sweep(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(fc.exprs) != 2 || fc.exprs[0] != `file == "a.md"` || fc.exprs[1] != `file == "b.md"` {
		t.Fatalf("空清单应清空全部（字典序确定）: %v", fc.exprs)
	}
}

// TestMilvusSweepIteratorError 游标失败必须上抛，且不得静默删任何文件。
func TestMilvusSweepIteratorError(t *testing.T) {
	fc := &fakeDeleteClient{}
	iter := &fakeRowIterator{err: errors.New("query down")}
	err := newSweepStore(fc, iter).Sweep(context.Background(), []string{"a.md"})
	if err == nil || !strings.Contains(err.Error(), "query down") {
		t.Fatalf("游标失败应上抛: %v", err)
	}
	if len(fc.exprs) != 0 {
		t.Fatalf("游标失败时不得删除: %v", fc.exprs)
	}
}

// TestMilvusSweepRejectsMissingFileColumn 查询结果缺 file 列 = 无法对账，
// 显式报错而非把集合当成空（否则一次查询形态漂移就会误清全库）。
func TestMilvusSweepRejectsMissingFileColumn(t *testing.T) {
	fc := &fakeDeleteClient{}
	iter := &fakeRowIterator{batches: []client.ResultSet{
		{entity.NewColumnVarChar(milvusContentField, []string{"x"})},
	}}
	err := newSweepStore(fc, iter).Sweep(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), milvusFileField) {
		t.Fatalf("缺 file 列应报错: %v", err)
	}
	if len(fc.exprs) != 0 {
		t.Fatalf("缺列时不得删除: %v", fc.exprs)
	}
}
