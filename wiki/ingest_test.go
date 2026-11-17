package wiki

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

// slugFromSource 测试编译器：slug 取 Source.Metadata["slug"]，正文取 Source.Content。
func slugFromSource(_ context.Context, src Source, _ []*Page) ([]*Page, error) {
	return []*Page{{
		Slug:  src.Metadata["slug"],
		Title: src.Title,
		Body:  src.Content,
		Tags:  src.Tags,
	}}, nil
}

func TestIngestCreateAndIdempotent(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	src := Source{
		ID:      "session-1",
		Title:   "主机 A 磁盘告警",
		Content: "检查 inode 与日志轮转。",
		Tags:    []string{"disk"},
		Metadata: map[string]string{
			"slug": "host-a",
		},
	}

	res, err := Ingest(ctx, st, slugFromSource, src, WithNow(clock))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if !slices.Equal(res.Created, []string{"host-a"}) || len(res.Updated) != 0 || len(res.Unchanged) != 0 {
		t.Fatalf("首次 Ingest Result = %+v", res)
	}

	p, err := st.Get(ctx, "host-a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.Version != 1 || !p.Updated.Equal(now) {
		t.Fatalf("首次写入 Version/Updated = %d/%v", p.Version, p.Updated)
	}
	if !slices.Equal(p.Sources, []string{"session-1"}) {
		t.Fatalf("Sources = %v，期望自动带上 source ID", p.Sources)
	}

	// 同一 source 重复摄取：内容未变 → 不产新页、不改 Updated、不涨版本。
	now = now.Add(time.Hour)
	res, err = Ingest(ctx, st, slugFromSource, src, WithNow(clock))
	if err != nil {
		t.Fatalf("重复 Ingest: %v", err)
	}
	if len(res.Created) != 0 || len(res.Updated) != 0 || !slices.Equal(res.Unchanged, []string{"host-a"}) {
		t.Fatalf("重复 Ingest Result = %+v", res)
	}
	p2, err := st.Get(ctx, "host-a")
	if err != nil {
		t.Fatalf("Get after repeated: %v", err)
	}
	if p2.Version != 1 {
		t.Fatalf("幂等重放涨版本: %d", p2.Version)
	}
	if !p2.Updated.Equal(now.Add(-time.Hour)) {
		t.Fatalf("幂等重放改了 Updated: %v", p2.Updated)
	}
	if !slices.Equal(p2.Sources, []string{"session-1"}) {
		t.Fatalf("幂等重放重复追加 Sources: %v", p2.Sources)
	}
}

func TestIngestUpdateAddsSourceAndBumpsVersion(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	body := "v1"
	compiler := func(_ context.Context, _ Source, _ []*Page) ([]*Page, error) {
		return []*Page{{Slug: "host-a", Title: "A", Body: body, Sources: []string{"doc://runbook"}}}, nil
	}

	if _, err := Ingest(ctx, st, compiler, Source{ID: "session-1", Content: body}, WithNow(clock)); err != nil {
		t.Fatalf("首次 Ingest: %v", err)
	}

	now = now.Add(time.Minute)
	body = "v2"
	res, err := Ingest(ctx, st, compiler, Source{ID: "session-2", Content: body}, WithNow(clock))
	if err != nil {
		t.Fatalf("更新 Ingest: %v", err)
	}
	if !slices.Equal(res.Updated, []string{"host-a"}) {
		t.Fatalf("更新 Result = %+v", res)
	}

	p, err := st.Get(ctx, "host-a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.Body != "v2" || p.Version != 2 || !p.Updated.Equal(now) {
		t.Fatalf("更新后页面 = %+v", p)
	}
	if !slices.Equal(p.Sources, []string{"doc://runbook", "session-1", "session-2"}) {
		t.Fatalf("Sources 并集 = %v", p.Sources)
	}
}

func TestIngestIncrementalLeavesUnrelatedPageUntouched(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	if _, err := Ingest(ctx, st, slugFromSource, Source{
		ID: "session-a", Content: "A", Metadata: map[string]string{"slug": "a"},
	}, WithNow(clock)); err != nil {
		t.Fatalf("Ingest A: %v", err)
	}
	aBefore, err := st.Get(ctx, "a")
	if err != nil {
		t.Fatalf("Get A: %v", err)
	}

	now = now.Add(time.Hour)
	if _, err := Ingest(ctx, st, slugFromSource, Source{
		ID: "session-b", Content: "B", Metadata: map[string]string{"slug": "b"},
	}, WithNow(clock)); err != nil {
		t.Fatalf("Ingest B: %v", err)
	}

	aAfter, err := st.Get(ctx, "a")
	if err != nil {
		t.Fatalf("Get A after: %v", err)
	}
	if aAfter.Version != aBefore.Version || !aAfter.Updated.Equal(aBefore.Updated) || aAfter.Body != aBefore.Body {
		t.Fatalf("无关页被改写: before=%+v after=%+v", aBefore, aAfter)
	}
	pages, err := st.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("页面数 = %d，期望 2", len(pages))
	}
}

func TestIngestNormalizesSetsForIdempotency(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	links := []string{"b", "a", "a", " "}
	compiler := func(_ context.Context, _ Source, _ []*Page) ([]*Page, error) {
		return []*Page{{
			Slug: "a", Title: "A", Body: "body",
			Links: links, Tags: []string{"t2", "t1", "t1"},
		}}, nil
	}
	if _, err := Ingest(ctx, st, compiler, Source{ID: "s1"}, WithNow(clock)); err != nil {
		t.Fatalf("首次 Ingest: %v", err)
	}
	p, err := st.Get(ctx, "a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !slices.Equal(p.Links, []string{"a", "b"}) || !slices.Equal(p.Tags, []string{"t1", "t2"}) {
		t.Fatalf("集合未归一: links=%v tags=%v", p.Links, p.Tags)
	}

	// 同一集合换顺序：不算内容变化。
	links = []string{"a", "b"}
	now = now.Add(time.Hour)
	res, err := Ingest(ctx, st, compiler, Source{ID: "s1"}, WithNow(clock))
	if err != nil {
		t.Fatalf("重复 Ingest: %v", err)
	}
	if !slices.Equal(res.Unchanged, []string{"a"}) {
		t.Fatalf("顺序变化被当成内容变化: %+v", res)
	}
}

func TestIngestCompilerSeesExistingPages(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	if err := st.Put(ctx, &Page{Slug: "existing", Body: "old"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var seen []*Page
	compiler := func(_ context.Context, _ Source, existing []*Page) ([]*Page, error) {
		seen = existing
		return nil, nil
	}
	if _, err := Ingest(ctx, st, compiler, Source{ID: "s1"}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(seen) != 1 || seen[0].Slug != "existing" || seen[0].Body != "old" {
		t.Fatalf("compiler 未拿到既有页: %+v", seen)
	}
}

func TestIngestRunsLintEvenWithoutDrafts(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	if err := st.Put(ctx, &Page{Slug: "a", Body: "x", Links: []string{"missing"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	res, err := Ingest(ctx, st, func(context.Context, Source, []*Page) ([]*Page, error) {
		return nil, nil
	}, Source{ID: "s1"})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(res.Findings) != 1 || res.Findings[0].Kind != FindingBrokenLink {
		t.Fatalf("Findings = %+v，期望断链", res.Findings)
	}
}

func TestIngestResultSorted(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	compiler := func(context.Context, Source, []*Page) ([]*Page, error) {
		return []*Page{{Slug: "c", Body: "c"}, {Slug: "a", Body: "a"}, {Slug: "b", Body: "b"}}, nil
	}
	res, err := Ingest(ctx, st, compiler, Source{ID: "s1"})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if !slices.Equal(res.Created, []string{"a", "b", "c"}) {
		t.Fatalf("Created 未排序: %v", res.Created)
	}
}

func TestIngestValidation(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	okCompiler := func(context.Context, Source, []*Page) ([]*Page, error) { return nil, nil }

	t.Run("nil store", func(t *testing.T) {
		if _, err := Ingest(ctx, nil, okCompiler, Source{ID: "s1"}); err == nil {
			t.Fatal("nil store 应报错")
		}
	})
	t.Run("nil compiler", func(t *testing.T) {
		if _, err := Ingest(ctx, st, nil, Source{ID: "s1"}); err == nil {
			t.Fatal("nil compiler 应报错")
		}
	})
	t.Run("empty source id", func(t *testing.T) {
		if _, err := Ingest(ctx, st, okCompiler, Source{ID: "  "}); !errors.Is(err, ErrInvalidSource) {
			t.Fatalf("空 source ID = %v，期望 ErrInvalidSource", err)
		}
	})
	t.Run("invalid slug", func(t *testing.T) {
		compiler := func(context.Context, Source, []*Page) ([]*Page, error) {
			return []*Page{{Slug: "../escape", Body: "x"}}, nil
		}
		if _, err := Ingest(ctx, st, compiler, Source{ID: "s1"}); !errors.Is(err, ErrInvalidSlug) {
			t.Fatalf("非法 slug = %v，期望 ErrInvalidSlug", err)
		}
	})
	t.Run("duplicate slug", func(t *testing.T) {
		compiler := func(context.Context, Source, []*Page) ([]*Page, error) {
			return []*Page{{Slug: "a", Body: "1"}, {Slug: "a", Body: "2"}}, nil
		}
		if _, err := Ingest(ctx, st, compiler, Source{ID: "s1"}); err == nil {
			t.Fatal("重复 slug 应报错")
		}
		pages, _ := st.List(ctx)
		if len(pages) != 0 {
			t.Fatalf("校验失败后不应落库: %+v", pages)
		}
	})
	t.Run("nil page", func(t *testing.T) {
		compiler := func(context.Context, Source, []*Page) ([]*Page, error) {
			return []*Page{nil}, nil
		}
		if _, err := Ingest(ctx, st, compiler, Source{ID: "s1"}); err == nil {
			t.Fatal("nil page 应报错")
		}
	})
	t.Run("compiler error", func(t *testing.T) {
		boom := errors.New("boom")
		compiler := func(context.Context, Source, []*Page) ([]*Page, error) { return nil, boom }
		if _, err := Ingest(ctx, st, compiler, Source{ID: "s1"}); !errors.Is(err, boom) {
			t.Fatalf("compiler 错误 = %v，期望透传 boom", err)
		}
	})
}

func TestIngestContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	compiler := func(context.Context, Source, []*Page) ([]*Page, error) {
		called = true
		return nil, nil
	}
	if _, err := Ingest(ctx, NewMemoryStore(), compiler, Source{ID: "s1"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("已取消 ctx = %v，期望 context.Canceled", err)
	}
	if called {
		t.Fatal("ctx 已取消仍调用了 compiler")
	}
}

// alwaysConflictStore Put 永远冲突，用于验证重试耗尽路径。
type alwaysConflictStore struct {
	MemoryStore
	puts int
}

func (s *alwaysConflictStore) Put(ctx context.Context, p *Page) error {
	s.puts++
	return &VersionConflictError{Slug: p.Slug, Expected: p.Version, Actual: p.Version + 10}
}

func TestIngestRetryExhaustion(t *testing.T) {
	st := &alwaysConflictStore{}
	if err := st.MemoryStore.Put(context.Background(), &Page{Slug: "a", Body: "old"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	compiler := func(context.Context, Source, []*Page) ([]*Page, error) {
		return []*Page{{Slug: "a", Body: "new"}}, nil
	}
	_, err := Ingest(context.Background(), st, compiler, Source{ID: "s1"})
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("重试耗尽 = %v，期望 ErrVersionConflict", err)
	}
	if st.puts != 5 {
		t.Fatalf("Put 尝试次数 = %d，期望 5", st.puts)
	}
}

func TestIngestConcurrentSameSource(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	src := Source{ID: "s1", Content: "same", Metadata: map[string]string{"slug": "a"}}

	var wg sync.WaitGroup
	results := make([]Result, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = Ingest(ctx, st, slugFromSource, src, WithNow(func() time.Time { return time.Unix(0, 0) }))
		}()
	}
	wg.Wait()

	created, unchanged := 0, 0
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("并发 Ingest[%d]: %v", i, errs[i])
		}
		created += len(results[i].Created)
		unchanged += len(results[i].Unchanged)
		if len(results[i].Updated) != 0 {
			t.Fatalf("并发同 source 不应产生更新: %+v", results[i])
		}
	}
	if created != 1 || unchanged != 1 {
		t.Fatalf("created=%d unchanged=%d，期望 1/1", created, unchanged)
	}
	p, err := st.Get(ctx, "a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.Version != 1 || !slices.Equal(p.Sources, []string{"s1"}) {
		t.Fatalf("并发幂等结果 = %+v", p)
	}
}

func TestIngestConcurrentDifferentSourcesMerge(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	compiler := func(_ context.Context, src Source, _ []*Page) ([]*Page, error) {
		return []*Page{{Slug: "shared", Body: src.Content}}, nil
	}
	sources := []Source{
		{ID: "s1", Content: "body-from-s1"},
		{ID: "s2", Content: "body-from-s2"},
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, src := range sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = Ingest(ctx, st, compiler, src)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("并发 Ingest[%d]: %v", i, err)
		}
	}

	p, err := st.Get(ctx, "shared")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.Version != 2 {
		t.Fatalf("两个 source 各写一次，版本 = %d，期望 2", p.Version)
	}
	if !slices.Equal(p.Sources, []string{"s1", "s2"}) {
		t.Fatalf("Sources 丢并集: %v", p.Sources)
	}
	if p.Body != "body-from-s1" && p.Body != "body-from-s2" {
		t.Fatalf("Body = %q，期望其中一个 source 的正文", p.Body)
	}
}
