package fsstore_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oagentkit/rag/wiki"
	"github.com/oagentkit/rag/wiki/fsstore"
)

var _ wiki.Store = (*fsstore.Store)(nil)

func TestRoundTripAndPersistence(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := fsstore.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	now := time.Date(2026, 11, 17, 10, 30, 0, 123456789, time.UTC)
	p := &wiki.Page{
		Slug:    "host-a",
		Title:   "主机 A",
		Body:    "# 处置\n\n第一段。\n---\n最后一段。\n",
		Links:   []string{"host-b", "runbook-disk"},
		Tags:    []string{"host", "disk"},
		Sources: []string{"session-1", "doc://runbook"},
		Updated: now,
	}
	if err := st.Put(ctx, p); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if p.Version != 1 {
		t.Fatalf("Version = %d，期望 1", p.Version)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "host-a.md"))
	if err != nil {
		t.Fatalf("读文件: %v", err)
	}
	if !strings.HasPrefix(string(raw), "---\n") {
		t.Fatalf("文件缺少 front-matter: %q", string(raw[:min(40, len(raw))]))
	}
	if !strings.Contains(string(raw), "slug: host-a") {
		t.Fatalf("front-matter 缺 slug: %s", raw)
	}

	got, err := st.Get(ctx, "host-a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Slug != p.Slug || got.Title != p.Title || got.Body != p.Body {
		t.Fatalf("回读标量不符:\n got=%+v\nwant=%+v", got, p)
	}
	if !slices.Equal(got.Links, p.Links) || !slices.Equal(got.Tags, p.Tags) || !slices.Equal(got.Sources, p.Sources) {
		t.Fatalf("回读集合不符: links=%v tags=%v sources=%v", got.Links, got.Tags, got.Sources)
	}
	if !got.Updated.Equal(now) || got.Version != 1 {
		t.Fatalf("回读 Updated/Version = %v/%d", got.Updated, got.Version)
	}

	// 重开（模拟进程重启）后仍可读。
	st2, err := fsstore.New(dir)
	if err != nil {
		t.Fatalf("New reopen: %v", err)
	}
	got2, err := st2.Get(ctx, "host-a")
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if got2.Body != p.Body || !got2.Updated.Equal(now) || got2.Version != 1 {
		t.Fatalf("重开后回读不符: %+v", got2)
	}

	// 原子写不应遗留临时文件。
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".wiki-") {
			t.Fatalf("遗留临时文件: %s", e.Name())
		}
	}
}

func TestBodyRoundTrip(t *testing.T) {
	ctx := context.Background()
	st, err := fsstore.New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	bodies := map[string]string{
		"empty":     "",
		"plain":     "x",
		"trailing":  "x\n",
		"leading":   "\nleading",
		"separator": "a\n---\nb\n",
		"crlf":      "中文\r\n换行\r\n",
		"fence":     "```\n---\n```\n",
		"long":      strings.Repeat("大", 10000),
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			p := &wiki.Page{Slug: "body-" + name, Body: body, Updated: time.Unix(0, 0).UTC()}
			if err := st.Put(ctx, p); err != nil {
				t.Fatalf("Put: %v", err)
			}
			got, err := st.Get(ctx, p.Slug)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.Body != body {
				t.Fatalf("Body 不等: got=%q want=%q", got.Body, body)
			}
		})
	}
}

func TestListSkipsNonMarkdownAndReportsCorruption(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := fsstore.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, slug := range []string{"b", "a"} {
		if err := st.Put(ctx, &wiki.Page{Slug: slug, Body: slug}); err != nil {
			t.Fatalf("Put %s: %v", slug, err)
		}
	}
	for name, content := range map[string]string{
		"notes.txt": "not a page",
		".tmp-1":    "temp",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("写 %s: %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "x.md"), []byte("---\nslug: x\n---\n"), 0o644); err != nil {
		t.Fatalf("写子目录: %v", err)
	}

	pages, err := st.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var slugs []string
	for _, p := range pages {
		slugs = append(slugs, p.Slug)
	}
	if !slices.Equal(slugs, []string{"a", "b"}) {
		t.Fatalf("List = %v，期望 [a b]", slugs)
	}

	if err := os.WriteFile(filepath.Join(dir, "bad.md"), []byte("no front matter"), 0o644); err != nil {
		t.Fatalf("写坏文件: %v", err)
	}
	if _, err := st.List(ctx); err == nil {
		t.Fatal("损坏文件应让 List 报错（不得静默跳过）")
	}
	if _, err := st.Get(ctx, "bad"); err == nil {
		t.Fatal("损坏文件 Get 应报错")
	}
}

func TestVersionConflictAndDelete(t *testing.T) {
	ctx := context.Background()
	st, err := fsstore.New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := st.Get(ctx, "missing"); !errors.Is(err, wiki.ErrNotFound) {
		t.Fatalf("Get(missing) = %v", err)
	}
	if err := st.Delete(ctx, "missing", 0); !errors.Is(err, wiki.ErrNotFound) {
		t.Fatalf("Delete(missing) = %v", err)
	}

	p := &wiki.Page{Slug: "a", Body: "v1"}
	if err := st.Put(ctx, p); err != nil {
		t.Fatalf("Put create: %v", err)
	}
	v1, err := st.Get(ctx, "a")
	if err != nil {
		t.Fatalf("Get v1: %v", err)
	}

	// 已存在时 Version=0 新建 → 冲突。
	if err := st.Put(ctx, &wiki.Page{Slug: "a", Body: "x"}); !errors.Is(err, wiki.ErrVersionConflict) {
		t.Fatalf("create existing = %v", err)
	}
	// 不存在的页带非 0 版本 → ErrNotFound。
	if err := st.Put(ctx, &wiki.Page{Slug: "b", Body: "x", Version: 2}); !errors.Is(err, wiki.ErrNotFound) {
		t.Fatalf("update missing = %v", err)
	}

	update := *v1
	update.Body = "v2"
	if err := st.Put(ctx, &update); err != nil {
		t.Fatalf("Put update: %v", err)
	}
	if update.Version != 2 {
		t.Fatalf("update Version = %d", update.Version)
	}

	stale := *v1 // Version=1
	stale.Body = "stale"
	err = st.Put(ctx, &stale)
	if !errors.Is(err, wiki.ErrVersionConflict) {
		t.Fatalf("stale Put = %v", err)
	}
	var conflict *wiki.VersionConflictError
	if !errors.As(err, &conflict) || conflict.Expected != 1 || conflict.Actual != 2 {
		t.Fatalf("冲突详情 = %#v", conflict)
	}

	if err := st.Delete(ctx, "a", 1); !errors.Is(err, wiki.ErrVersionConflict) {
		t.Fatalf("Delete stale = %v", err)
	}
	if err := st.Delete(ctx, "a", 2); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := st.Get(ctx, "a"); !errors.Is(err, wiki.ErrNotFound) {
		t.Fatalf("Get after delete = %v", err)
	}
	if _, err := os.Stat(filepath.Join(st.Dir(), "a.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("删除后文件仍存在: %v", err)
	}
}

func TestInvalidSlugAndNilPage(t *testing.T) {
	ctx := context.Background()
	st, err := fsstore.New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, slug := range []string{"", "../escape", "a/b", ".hidden"} {
		if _, err := st.Get(ctx, slug); !errors.Is(err, wiki.ErrInvalidSlug) {
			t.Fatalf("Get(%q) = %v", slug, err)
		}
		if err := st.Delete(ctx, slug, 0); !errors.Is(err, wiki.ErrInvalidSlug) {
			t.Fatalf("Delete(%q) = %v", slug, err)
		}
	}
	if err := st.Put(ctx, &wiki.Page{Slug: "../escape", Body: "x"}); !errors.Is(err, wiki.ErrInvalidSlug) {
		t.Fatalf("Put 非法 slug = %v", err)
	}
	if err := st.Put(ctx, nil); err == nil {
		t.Fatal("Put(nil) 应报错")
	}
}

func TestConcurrentPutConflict(t *testing.T) {
	ctx := context.Background()
	st, err := fsstore.New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p := &wiki.Page{Slug: "a", Body: "base"}
	if err := st.Put(ctx, p); err != nil {
		t.Fatalf("seed: %v", err)
	}
	v := p.Version

	start := make(chan struct{})
	var wg sync.WaitGroup
	bodies := []string{"one", "two"}
	errs := make([]error, len(bodies))
	for i, body := range bodies {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = st.Put(ctx, &wiki.Page{Slug: "a", Body: body, Version: v})
		}()
	}
	close(start)
	wg.Wait()

	successes := 0
	conflicts := 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, wiki.ErrVersionConflict):
			conflicts++
		default:
			t.Fatalf("并发 Put 意外错误: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("并发 Put success=%d conflict=%d，期望 1/1", successes, conflicts)
	}
	got, err := st.Get(ctx, "a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Body != "one" && got.Body != "two" {
		t.Fatalf("最终 Body = %q", got.Body)
	}
	if got.Version != v+1 {
		t.Fatalf("最终 Version = %d，期望 %d", got.Version, v+1)
	}
}

func TestContextCanceled(t *testing.T) {
	st, err := fsstore.New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := st.Get(ctx, "a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Get = %v", err)
	}
	if _, err := st.List(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("List = %v", err)
	}
	if err := st.Put(ctx, &wiki.Page{Slug: "a", Body: "x"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Put = %v", err)
	}
	if err := st.Delete(ctx, "a", 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete = %v", err)
	}
}

func TestNewCreatesDirectoryAndRejectsFile(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "nested", "wiki")
	st, err := fsstore.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if st.Dir() != dir {
		t.Fatalf("Dir() = %q，期望 %q", st.Dir(), dir)
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		t.Fatalf("目录未创建: %v", err)
	}

	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("写文件: %v", err)
	}
	if _, err := fsstore.New(file); err == nil {
		t.Fatal("路径是文件时应报错")
	}
	if _, err := fsstore.New(""); err == nil {
		t.Fatal("空目录应报错")
	}
}

func TestIngestLeavesUnrelatedFileBytesUnchanged(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := fsstore.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	compiler := func(_ context.Context, src wiki.Source, _ []*wiki.Page) ([]*wiki.Page, error) {
		return []*wiki.Page{{Slug: src.Metadata["slug"], Body: src.Content}}, nil
	}

	if _, err := wiki.Ingest(ctx, st, compiler, wiki.Source{
		ID: "s-a", Content: "A", Metadata: map[string]string{"slug": "a"},
	}, wiki.WithNow(clock)); err != nil {
		t.Fatalf("Ingest A: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "a.md"))
	if err != nil {
		t.Fatalf("读 a.md: %v", err)
	}
	beforePage, err := st.Get(ctx, "a")
	if err != nil {
		t.Fatalf("Get a: %v", err)
	}

	now = now.Add(time.Hour)
	if _, err := wiki.Ingest(ctx, st, compiler, wiki.Source{
		ID: "s-b", Content: "B", Metadata: map[string]string{"slug": "b"},
	}, wiki.WithNow(clock)); err != nil {
		t.Fatalf("Ingest B: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "a.md"))
	if err != nil {
		t.Fatalf("再读 a.md: %v", err)
	}
	afterPage, err := st.Get(ctx, "a")
	if err != nil {
		t.Fatalf("Get a after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("无关页文件字节被改写:\nbefore=%s\nafter=%s", before, after)
	}
	if beforePage.Version != afterPage.Version || !beforePage.Updated.Equal(afterPage.Updated) {
		t.Fatalf("无关页版本/时间被改写: %+v -> %+v", beforePage, afterPage)
	}
}
