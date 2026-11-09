package rag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTokenizeNonASCIILetters 谚文/假名/西里尔等非 ASCII 字母不得被静默丢弃
// （oagentkit/rag#84：旧实现 default 分支直接 addWord()，整类语言文档索引为零词元
// 且查询恒空——无错误无告警）。修后「CJK 二元组」名副其实：任何非 ASCII 字母
// 与后邻非 ASCII 字母成二元组，末字补单字。
func TestTokenizeNonASCIILetters(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		// 谚文（Hangul）：非 ASCII 字母按 CJK 风格二元组
		{"한국어 텍스트", []string{"한국", "국어", "어", "텍스", "스트", "트"}},
		// 假名（Kana）：与 Han 同列，不再当边界
		{"日本語のテキスト", []string{"日本", "本語", "語の", "のテ", "テキ", "キス", "スト", "ト"}},
		// 纯假名
		{"ほうしん", []string{"ほう", "うし", "しん", "ん"}},
		// 西里尔（空格分词的字母文字）：同口径成二元组，词尾字母补单字
		{"привет мир", []string{"пр", "ри", "ив", "ве", "ет", "т", "ми", "ир", "р"}},
		// 希腊字母
		{"αβγ δ", []string{"αβ", "βγ", "γ", "δ"}},
		// 非 ASCII 字母与 Han 相邻：跨脚本同样成对（两侧一致，不伤召回）
		{"デプロイ規範", []string{"デプ", "プロ", "ロイ", "イ規", "規範", "範"}},
		// ASCII 词边界仍 flush
		{"Go 배포", []string{"go", "배포", "포"}},
		// emoji 仍为边界
		{"😀배", []string{"배"}},
	}
	for _, c := range cases {
		got := tokenize(c.in)
		if len(got) == 0 && len(c.want) == 0 {
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("tokenize(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("tokenize(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

// TestLocalRetrieveNonHanScripts 端到端反证：纯假名/纯谚文文档必须可被本语言查询召回，
// 且不相关查询不误召（oagentkit/rag#84 失败场景 = issue 复现程序的测试化）。
func TestLocalRetrieveNonHanScripts(t *testing.T) {
	dir := t.TempDir()
	docs := map[string]string{
		"jp2.md": "# にほんごぶんしょ\n\nにほんごのほんをよむときほうしんをかくします\n",
		"kr.md":  "# 한국어 문서\n\n배포 규격에 따라 모든 서비스는 통합 게이트웨이를 통해 연결해야합니다.\n",
		"cn.md":  "# 中文文档\n\n部署规范要求所有服务通过统一网关接入。\n",
	}
	for name, body := range docs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	loc, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		file string
		q    string
	}{
		{"jp2.md", "ほうしん"},
		{"kr.md", "배포 규격"},
		{"cn.md", "部署规范"},
	}
	for _, c := range cases {
		chunks, err := loc.Retrieve(t.Context(), c.q, 5, nil)
		if err != nil {
			t.Fatalf("Retrieve(%q): %v", c.q, err)
		}
		if len(chunks) == 0 {
			t.Errorf("query %q hit 0 chunks: document %s is permanently unsearchable", c.q, c.file)
			continue
		}
		if !strings.HasPrefix(chunks[0].ID, c.file+"#") {
			t.Errorf("query %q: top hit = %s, want %s", c.q, chunks[0].ID, c.file)
		}
	}
	// 反证：跨语言不相关查询不召回对方文档
	chunks, err := loc.Retrieve(t.Context(), "ほうしん", 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range chunks {
		if strings.HasPrefix(ch.ID, "kr.md#") {
			t.Errorf("jp query hit kr doc: %s", ch.ID)
		}
	}
}
