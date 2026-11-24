package rag

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// 本文件钉住「身份键与字节守卫」的单源面（oagentkit/rag#156）：向量后端已挪到
// rag/milvus 子包，但两路共用同一套身份形态与截断规则——单源在根包，用例在
// 根包侧钉住「本地索引路取的就是这个单源」，向量后端侧由该包的
// TestIndexRowsIDsUseSharedIdentity 钉住，两侧相加即跨路一致性。

// TestChunkIDFormat 身份键形态单源：ID = file#chunk_N（两路共用，跨路归并的
// 前提——rag#40）。
func TestChunkIDFormat(t *testing.T) {
	if got := ChunkID("doc.md", 0); got != "doc.md#chunk_0" {
		t.Fatalf("ChunkID = %q, want doc.md#chunk_0", got)
	}
	if got := ChunkID("doc.md", 12); got != "doc.md#chunk_12" {
		t.Fatalf("ChunkID = %q, want doc.md#chunk_12", got)
	}
}

// TestFileStoredIdentity 落库形态单源：限内恒等（既有身份形态不变）；超限截断
// +散列后缀（保持唯一，不劈多字节 rune）。
func TestFileStoredIdentity(t *testing.T) {
	short := "dir/doc.md"
	if got := FileStored(short); got != short {
		t.Fatalf("限内路径应恒等（既有身份形态不变）: %q", got)
	}

	deep1 := strings.Repeat("深目录/", 128) + "one.md"
	deep2 := strings.Repeat("深目录/", 128) + "two.md"
	s1, s2 := FileStored(deep1), FileStored(deep2)
	for _, s := range []string{s1, s2} {
		if len(s) > FileStoredMaxLen {
			t.Fatalf("落库形态 %d 字节超上限 %d", len(s), FileStoredMaxLen)
		}
		if !utf8.ValidString(s) {
			t.Fatalf("落库形态破坏 UTF-8: %q", s)
		}
		// 散列后缀 = "#" + 8 hex（FileStoredHashLen 字节）
		suffix := s[len(s)-FileStoredHashLen:]
		if suffix[0] != '#' {
			t.Fatalf("落库形态应以散列后缀结尾: %q", s)
		}
		for _, c := range suffix[1:] {
			if !strings.ContainsRune("0123456789abcdef", c) {
				t.Fatalf("散列后缀应只含 hex: %q", suffix)
			}
		}
	}
	if s1 == s2 {
		t.Fatalf("共享长前缀的深路径不应碰撞（撞了=互相覆盖=静默丢数据）: %q", s1)
	}
	if FileStored(deep1) != s1 {
		t.Fatal("落库形态应确定性（同输入同输出）")
	}
}

// TestTruncateUTF8 字节守卫单源：超限按字节截断但不劈开多字节 rune（劈半的
// rune 落库即乱码）；不超限原样返回。
func TestTruncateUTF8(t *testing.T) {
	if got := TruncateUTF8("abc", 10); got != "abc" {
		t.Fatalf("不超限应原样: %q", got)
	}
	if got := TruncateUTF8("abc", 3); got != "abc" {
		t.Fatalf("恰好等长应原样: %q", got)
	}
	// "中文abc" 共 9 字节；截到 5 字节时不得劈开 "文"（3 字节）⇒ 回退到 3 字节
	got := TruncateUTF8("中文abc", 5)
	if got != "中" {
		t.Fatalf("截断不应劈开多字节 rune: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("截断结果必须是合法 UTF-8: %q", got)
	}
}

// TestLocalChunkIDAgreesWithSharedIdentity 本地索引路的 chunk ID 必须由根包
// 身份键单源派生（ChunkID + FileStored）——与向量后端侧（rag/milvus 子包的
// TestIndexRowsIDsUseSharedIdentity）指向同一单源，跨路一致性由构造保证，
// 不靠两处各写一份（oagentkit/rag#156）。
func TestLocalChunkIDAgreesWithSharedIdentity(t *testing.T) {
	deep := strings.Repeat("深目录/", 128) + "one.md" // >512 字节 rel 路径
	var idx []indexedChunk
	df := map[string]int{}
	indexMarkdownFile(deep, "# 甲\n\n正文。\n", &idx, df)
	if len(idx) == 0 {
		t.Fatal("应产出 chunk")
	}
	if want := ChunkID(FileStored(deep), 0); idx[0].chunk.ID != want {
		t.Fatalf("Local chunk ID 应取根包身份键单源: got=%q want=%q", idx[0].chunk.ID, want)
	}

	// 限内路径 raw == stored：Local ID 形态保持不变
	var idx2 []indexedChunk
	indexMarkdownFile("doc.md", "# 乙\n\n正文。\n", &idx2, df)
	if idx2[0].chunk.ID != "doc.md#chunk_0" {
		t.Fatalf("限内路径 Local ID 形态不应变化: %q", idx2[0].chunk.ID)
	}
}

// TestValidateFilterContract 过滤契约单源：白名单内的 key 放行，白名单外
// fail-fast 报错（Local 与向量后端同一契约——rag#59 口径）。
func TestValidateFilterContract(t *testing.T) {
	if err := ValidateFilter(nil); err != nil {
		t.Fatalf("nil 过滤应放行: %v", err)
	}
	if err := ValidateFilter(Filter{"file": "a.md"}); err != nil {
		t.Fatalf("file 应放行: %v", err)
	}
	if err := ValidateFilter(Filter{"heading": "小节"}); err != nil {
		t.Fatalf("heading 应放行: %v", err)
	}
	if err := ValidateFilter(Filter{"unknown": "x"}); err == nil {
		t.Fatal("白名单外的 key 应 fail-fast 报错")
	}
}

// TestChunkMarkdownMatchesPreview 切块导出面与预览导出面同结果（切块实现
// 单源）——向量后端写入路径经 ChunkMarkdown 取块，预览面经 ChunkPreview 取块，
// 两者不得分叉。
func TestChunkMarkdownMatchesPreview(t *testing.T) {
	md := "# 标题\n\n正文一。\n\n## 小节\n\n正文二。\n"
	preview := ChunkPreview(md)
	chunks := ChunkMarkdown(md)
	if len(preview) != len(chunks) {
		t.Fatalf("预览与切块块数应一致: preview=%d chunks=%d", len(preview), len(chunks))
	}
	if len(chunks) == 0 {
		t.Fatal("应切出至少一块")
	}
	for i := range chunks {
		if chunks[i].Content != preview[i].Content {
			t.Fatalf("块[%d] 内容应一致: %q vs %q", i, chunks[i].Content, preview[i].Content)
		}
		if chunks[i].Metadata["heading"] != preview[i].Metadata["heading"] {
			t.Fatalf("块[%d] 标题应一致: %q vs %q", i,
				chunks[i].Metadata["heading"], preview[i].Metadata["heading"])
		}
	}
	// 标题随块归属：第二块属 "## 小节"
	found := false
	for _, c := range chunks {
		if c.Metadata["heading"] == "小节" {
			found = true
		}
	}
	if !found {
		t.Fatalf("应有一块归属二级标题: %+v", chunks)
	}
}
