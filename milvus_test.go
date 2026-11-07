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
