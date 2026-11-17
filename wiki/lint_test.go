package wiki

import (
	"math/rand"
	"slices"
	"testing"
)

func TestLintEmptyAndSinglePage(t *testing.T) {
	if got := Lint(nil); len(got) != 0 {
		t.Fatalf("Lint(nil) = %+v", got)
	}
	if got := Lint([]*Page{}); len(got) != 0 {
		t.Fatalf("Lint(empty) = %+v", got)
	}
	if got := Lint([]*Page{{Slug: "only", Title: "Only", Body: "内容"}}); len(got) != 0 {
		t.Fatalf("单页非空库不应报孤儿: %+v", got)
	}
	got := Lint([]*Page{{Slug: "only", Title: "Only"}})
	if len(got) != 1 || got[0].Kind != FindingEmpty || got[0].Slug != "only" {
		t.Fatalf("单页空正文 = %+v", got)
	}
}

func TestLintBrokenLink(t *testing.T) {
	pages := []*Page{
		{Slug: "a", Body: "x", Links: []string{"b", "missing", "missing", ""}},
		{Slug: "b", Body: "y", Links: []string{"a"}},
	}
	got := Lint(pages)
	if len(got) != 1 {
		t.Fatalf("Findings = %+v，期望 1 条断链", got)
	}
	f := got[0]
	if f.Kind != FindingBrokenLink || f.Slug != "a" || f.Related != "missing" {
		t.Fatalf("断链 Finding = %+v", f)
	}
}

func TestLintOrphan(t *testing.T) {
	pages := []*Page{
		{Slug: "a", Body: "x", Links: []string{"b"}},
		{Slug: "b", Body: "y"},
		{Slug: "c", Body: "z"},
		{Slug: "d", Body: "w", Links: []string{"d"}}, // 自环不算入链
	}
	got := Lint(pages)
	var orphans []string
	for _, f := range got {
		if f.Kind == FindingOrphan {
			orphans = append(orphans, f.Slug)
		}
	}
	if !slices.Equal(orphans, []string{"a", "c", "d"}) {
		t.Fatalf("孤儿页 = %v，期望 [a c d]（a 无人入链；d 自环不算）", orphans)
	}
}

func TestLintDuplicateCandidates(t *testing.T) {
	pages := []*Page{
		{Slug: "a", Title: "磁盘告警处置", Body: "检查 inode 与日志轮转，确认磁盘空间。"},
		{Slug: "b", Title: "磁盘告警处置", Body: "检查 inode 与日志轮转，确认磁盘空间。"},
		{Slug: "c", Title: "网络抖动", Body: "检查网卡与上游交换机。"},
	}
	got := Lint(pages)
	var dups []Finding
	for _, f := range got {
		if f.Kind == FindingDuplicate {
			dups = append(dups, f)
		}
	}
	if len(dups) != 1 {
		t.Fatalf("重复候选 = %+v，期望 1 条", dups)
	}
	if dups[0].Slug != "a" || dups[0].Related != "b" {
		t.Fatalf("重复候选详情 = %+v", dups[0])
	}
}

func TestLintDeterministic(t *testing.T) {
	pages := []*Page{
		{Slug: "a", Body: "x", Links: []string{"missing-2", "missing-1"}},
		{Slug: "b", Body: "same text"},
		{Slug: "c", Body: "same text"},
		{Slug: "d", Body: "", Links: []string{"a"}},
	}
	first := Lint(pages)

	r := rand.New(rand.NewSource(144))
	for range 10 {
		shuffled := slices.Clone(pages)
		r.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		next := Lint(shuffled)
		if !slices.Equal(first, next) {
			t.Fatalf("Lint 非确定:\nfirst=%+v\nnext =%+v", first, next)
		}
	}
}

func TestLintNilPagesAndDuplicateSlug(t *testing.T) {
	got := Lint([]*Page{nil, {Slug: "a", Body: "x"}, nil})
	if len(got) != 0 {
		t.Fatalf("nil 页与单页库 = %+v，期望零 Finding", got)
	}

	// 同 slug 不算重复候选（是存储层问题，不是内容重复）。
	got = Lint([]*Page{{Slug: "a", Body: "x"}, {Slug: "a", Body: "x"}})
	for _, f := range got {
		if f.Kind == FindingDuplicate {
			t.Fatalf("同 slug 不应报重复候选: %+v", f)
		}
	}
}
