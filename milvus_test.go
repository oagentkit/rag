package rag

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
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

func TestOpenAIEmbedderDim(t *testing.T) {
	e := NewOpenAIEmbedder("https://api.example.com/v1", "key", "model", 128)
	if e.Dim() != 128 {
		t.Fatalf("Dim = %d, want 128", e.Dim())
	}
}

func TestOpenAIEmbedderString(t *testing.T) {
	e := NewOpenAIEmbedder("https://api.example.com/v1", "key", "model", 128)
	s := e.String()
	if s == "" {
		t.Fatal("String() 不应为空")
	}
}

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

func TestMilvusConfigDefaults(t *testing.T) {
	cfg := MilvusConfig{Address: "localhost:19530"}
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
func TestNewMilvusStoreNilEmbedder(t *testing.T) {
	if _, err := NewMilvusStore(context.Background(), MilvusConfig{Address: "127.0.0.1:19530"}, nil); err == nil {
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
	rows, err := indexRows("doc.md", chunkMarkdown(md))
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
	r1, err := indexRows(deep1, chunkMarkdown("# 甲\n\n正文一。\n"))
	if err != nil {
		t.Fatal(err)
	}
	r2, err := indexRows(deep2, chunkMarkdown("# 乙\n\n正文二。\n"))
	if err != nil {
		t.Fatal(err)
	}
	for i, rs := range [][]milvusRow{r1, r2} {
		for j, row := range rs {
			if got := len(row.file); got > milvusFileMaxLen {
				t.Fatalf("rows[%d][%d] file %d 字节超限 %d", i, j, got, milvusFileMaxLen)
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
	r3, err := indexRows("doc.md", chunkMarkdown("# 丙\n\n正文三。\n"))
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
	rows, err := indexRows("doc.md", []mdChunk{{heading: "标题", content: over}})
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
	rows2, err := indexRows("doc.md", []mdChunk{{content: "正文。"}})
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
	file := strings.Repeat("a", milvusFileMaxLen)
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

func newCaptureStore(fc *fakeDeleteClient) *MilvusStore {
	return &MilvusStore{cfg: MilvusConfig{Collection: "t"}, client: fc, embedder: &mockEmbedder{dim: 4}}
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
	stored := escapeMilvus(milvusFileStored(deep))
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
	expr, err := milvusFilterExpr(Filter{"file": deep})
	if err != nil {
		t.Fatal(err)
	}
	stored := escapeMilvus(milvusFileStored(deep))
	if !strings.Contains(expr, stored) {
		t.Fatalf("过滤表达式未归一到落库形态 %q: %s", stored, expr)
	}
	// 限内路径归一为恒等：表达式形态不变
	expr2, err := milvusFilterExpr(Filter{"file": "doc.md"})
	if err != nil {
		t.Fatal(err)
	}
	if expr2 != `file == "doc.md"` {
		t.Fatalf("限内路径过滤表达式形态不应变化: %s", expr2)
	}
}

// TestCrossLaneChunkIDAgreeOnStoredIdentity 回归（rag#59）：同一文件的 chunk
// ID 在两条索引路必须一致——Milvus 主键由落库形态派生，Local 此前由原始 rel
// 路径派生，超限路径双路 ID 永不相等，FuseRRF 按 ID 归并双路共识静默失效
// （同源副本无法归并，混合检索退化为两路各自计分）。
func TestCrossLaneChunkIDAgreeOnStoredIdentity(t *testing.T) {
	deep := strings.Repeat("深目录/", 128) + "one.md" // >512 字节 rel 路径
	rows, err := indexRows(deep, chunkMarkdown("# 甲\n\n正文。\n"))
	if err != nil {
		t.Fatal(err)
	}
	var idx []indexedChunk
	df := map[string]int{}
	indexMarkdownFile(deep, "# 甲\n\n正文。\n", &idx, df)
	if len(rows) == 0 || len(idx) == 0 {
		t.Fatalf("两路都应产出 chunk: milvus=%d local=%d", len(rows), len(idx))
	}
	if idx[0].chunk.ID != rows[0].id {
		t.Fatalf("双路 ID 分裂: local=%q milvus=%q", idx[0].chunk.ID, rows[0].id)
	}
	// 限内路径 raw == stored：Local ID 形态保持不变
	var idx2 []indexedChunk
	indexMarkdownFile("doc.md", "# 乙\n\n正文。\n", &idx2, df)
	if idx2[0].chunk.ID != "doc.md#chunk_0" {
		t.Fatalf("限内路径 Local ID 形态不应变化: %q", idx2[0].chunk.ID)
	}
}
