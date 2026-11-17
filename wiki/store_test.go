package wiki

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestValidateSlug(t *testing.T) {
	valid := []string{"host-a", "Host_A.1", "主机-甲", "v1.0", "a..b", "ns-page"}
	for _, slug := range valid {
		if err := ValidateSlug(slug); err != nil {
			t.Errorf("ValidateSlug(%q) = %v，期望合法", slug, err)
		}
	}

	invalid := []string{
		"", ".", "..", ".hidden", "a/b", `a\b`, "a:b", "a*b", "a?b",
		"a|b", `a"b`, "a<b", "a>b", " a", "a ", "a.", "a\x00b", "a\nb",
	}
	for _, slug := range invalid {
		if err := ValidateSlug(slug); !errors.Is(err, ErrInvalidSlug) {
			t.Errorf("ValidateSlug(%q) = %v，期望 ErrInvalidSlug", slug, err)
		}
	}
}

func TestMemoryStoreCRUD(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)

	if _, err := st.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(missing) = %v，期望 ErrNotFound", err)
	}

	p := &Page{
		Slug:    "host-a",
		Title:   "主机 A",
		Body:    "body-v1",
		Links:   []string{"host-b"},
		Tags:    []string{"host"},
		Sources: []string{"session-1"},
		Updated: now,
	}
	if err := st.Put(ctx, p); err != nil {
		t.Fatalf("Put 新建: %v", err)
	}
	if p.Version != 1 {
		t.Fatalf("新建后 Version = %d，期望 1", p.Version)
	}

	got, err := st.Get(ctx, "host-a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "主机 A" || got.Body != "body-v1" || got.Version != 1 {
		t.Fatalf("Get 回读不符: %+v", got)
	}
	if !got.Updated.Equal(now) {
		t.Fatalf("Updated = %v，期望 %v", got.Updated, now)
	}

	// 返回副本：修改回读结果不得污染存储。
	got.Title = "被篡改"
	again, err := st.Get(ctx, "host-a")
	if err != nil {
		t.Fatalf("Get again: %v", err)
	}
	if again.Title != "主机 A" {
		t.Fatalf("Get 未返回副本，Title = %q", again.Title)
	}

	// 正确版本更新 → 版本递增。
	update := *again
	update.Body = "body-v2"
	if err := st.Put(ctx, &update); err != nil {
		t.Fatalf("Put 更新: %v", err)
	}
	if update.Version != 2 {
		t.Fatalf("更新后 Version = %d，期望 2", update.Version)
	}

	// 过期版本更新 → 冲突且不落库。
	stale := *again // Version=1
	stale.Body = "stale"
	err = st.Put(ctx, &stale)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("过期 Put = %v，期望 ErrVersionConflict", err)
	}
	var conflict *VersionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("过期 Put 错误类型 = %T，期望 *VersionConflictError", err)
	}
	if conflict.Slug != "host-a" || conflict.Expected != 1 || conflict.Actual != 2 {
		t.Fatalf("冲突详情 = %+v", conflict)
	}
	afterConflict, err := st.Get(ctx, "host-a")
	if err != nil {
		t.Fatalf("Get after conflict: %v", err)
	}
	if afterConflict.Body != "body-v2" || afterConflict.Version != 2 {
		t.Fatalf("冲突写入污染了存储: %+v", afterConflict)
	}

	// 新建时 Version 非 0 且已存在 → 冲突。
	createExisting := &Page{Slug: "host-a", Body: "x", Version: 0}
	err = st.Put(ctx, createExisting)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("已存在时 Version=0 Put = %v，期望 ErrVersionConflict", err)
	}

	// 删除版本校验。
	if err := st.Delete(ctx, "host-a", 1); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("Delete 过期版本 = %v，期望 ErrVersionConflict", err)
	}
	if err := st.Delete(ctx, "host-a", 2); err != nil {
		t.Fatalf("Delete 正确版本: %v", err)
	}
	if _, err := st.Get(ctx, "host-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后 Get = %v，期望 ErrNotFound", err)
	}
	if err := st.Delete(ctx, "host-a", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复 Delete = %v，期望 ErrNotFound", err)
	}
}

func TestMemoryStoreCreateWithNonZeroVersion(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	p := &Page{Slug: "a", Body: "x", Version: 3}
	if err := st.Put(ctx, p); !errors.Is(err, ErrNotFound) {
		t.Fatalf("新建携带 Version=3 = %v，期望 ErrNotFound", err)
	}
}

func TestMemoryStoreListSortedAndIsolated(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	for _, slug := range []string{"b", "a", "c"} {
		if err := st.Put(ctx, &Page{Slug: slug, Body: slug}); err != nil {
			t.Fatalf("Put %s: %v", slug, err)
		}
	}
	pages, err := st.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var slugs []string
	for _, p := range pages {
		slugs = append(slugs, p.Slug)
	}
	if !slices.Equal(slugs, []string{"a", "b", "c"}) {
		t.Fatalf("List 顺序 = %v，期望 [a b c]", slugs)
	}
	// 修改 List 返回值不影响存储。
	pages[0].Body = "mutated"
	got, err := st.Get(ctx, "a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Body != "a" {
		t.Fatalf("List 未返回副本，Body = %q", got.Body)
	}
}

func TestMemoryStoreZeroValueAndValidation(t *testing.T) {
	ctx := context.Background()
	var st MemoryStore // 零值可用

	if err := st.Put(ctx, nil); err == nil {
		t.Fatal("Put(nil) 应报错")
	}
	if err := st.Put(ctx, &Page{Slug: "../escape", Body: "x"}); !errors.Is(err, ErrInvalidSlug) {
		t.Fatalf("非法 slug Put = %v，期望 ErrInvalidSlug", err)
	}
	if _, err := st.Get(ctx, "a/b"); !errors.Is(err, ErrInvalidSlug) {
		t.Fatalf("非法 slug Get = %v，期望 ErrInvalidSlug", err)
	}
	if err := st.Delete(ctx, "a/b", 0); !errors.Is(err, ErrInvalidSlug) {
		t.Fatalf("非法 slug Delete = %v，期望 ErrInvalidSlug", err)
	}

	if err := st.Put(ctx, &Page{Slug: "ok", Body: "x"}); err != nil {
		t.Fatalf("零值 Put: %v", err)
	}
	got, err := st.Get(ctx, "ok")
	if err != nil || got.Version != 1 {
		t.Fatalf("零值 Get = %+v, %v", got, err)
	}
}

func TestVersionConflictErrorMessageAndPageFileName(t *testing.T) {
	err := &VersionConflictError{Slug: "host-a", Expected: 1, Actual: 2}
	msg := err.Error()
	for _, want := range []string{"host-a", "1", "2"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("Error() = %q，缺 %q", msg, want)
		}
	}
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatal("VersionConflictError 应可判定为 ErrVersionConflict")
	}
	if got := PageFileName("host-a"); got != "host-a.md" {
		t.Fatalf("PageFileName = %q", got)
	}
}
