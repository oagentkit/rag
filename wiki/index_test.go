package wiki

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type indexCall struct {
	op      string
	file    string
	content string
}

type fakeIndexer struct {
	calls []indexCall
	err   error
}

func (f *fakeIndexer) Index(_ context.Context, file string, content string) error {
	f.calls = append(f.calls, indexCall{op: "index", file: file, content: content})
	return f.err
}

func (f *fakeIndexer) DeleteFile(_ context.Context, file string) error {
	f.calls = append(f.calls, indexCall{op: "delete", file: file})
	return f.err
}

func TestIndexedStorePutAndDelete(t *testing.T) {
	ctx := context.Background()
	inner := NewMemoryStore()
	idx := &fakeIndexer{}
	st := NewIndexedStore(inner, idx)

	p := &Page{Slug: "host-a", Body: "body-v1"}
	if err := st.Put(ctx, p); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if len(idx.calls) != 1 || idx.calls[0].op != "index" || idx.calls[0].file != "host-a.md" || idx.calls[0].content != "body-v1" {
		t.Fatalf("索引调用 = %+v", idx.calls)
	}
	if p.Version != 1 {
		t.Fatalf("Version = %d，期望 1", p.Version)
	}

	p.Body = "body-v2"
	if err := st.Put(ctx, p); err != nil {
		t.Fatalf("Put update: %v", err)
	}
	if len(idx.calls) != 2 || idx.calls[1].content != "body-v2" {
		t.Fatalf("更新索引调用 = %+v", idx.calls)
	}

	if err := st.Delete(ctx, "host-a", 0); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(idx.calls) != 3 || idx.calls[2].op != "delete" || idx.calls[2].file != "host-a.md" {
		t.Fatalf("删除索引调用 = %+v", idx.calls)
	}

	// Get/List 委托内层。
	if _, err := st.Get(ctx, "host-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after delete = %v", err)
	}
}

func TestIndexedStorePropagatesIndexError(t *testing.T) {
	ctx := context.Background()
	inner := NewMemoryStore()
	idx := &fakeIndexer{err: errors.New("index down")}
	st := NewIndexedStore(inner, idx)

	p := &Page{Slug: "a", Body: "x"}
	err := st.Put(ctx, p)
	if err == nil || !errors.Is(err, idx.err) {
		t.Fatalf("Put = %v，期望透传索引错误", err)
	}
	// 存储写入已生效（至少一次语义），版本已递增。
	got, getErr := inner.Get(ctx, "a")
	if getErr != nil || got.Version != 1 {
		t.Fatalf("索引失败后存储状态 = %+v, %v", got, getErr)
	}

	if err := st.Delete(ctx, "a", 0); err == nil {
		t.Fatal("Delete 索引失败应报错")
	}
}

func TestNewIndexedStoreNilIndexer(t *testing.T) {
	inner := NewMemoryStore()
	if got := NewIndexedStore(inner, nil); got != inner {
		t.Fatal("nil indexer 应退化为内层 Store")
	}
	if got := NewIndexedStore(nil, &fakeIndexer{}); got != nil {
		t.Fatal("nil inner 应返回 nil")
	}
}

func TestSyncAll(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	for _, slug := range []string{"b", "a"} {
		if err := st.Put(ctx, &Page{Slug: slug, Body: "body-" + slug}); err != nil {
			t.Fatalf("seed %s: %v", slug, err)
		}
	}
	idx := &fakeIndexer{}
	if err := SyncAll(ctx, st, idx); err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	var files []string
	for _, c := range idx.calls {
		files = append(files, c.file)
	}
	if !slices.Equal(files, []string{"a.md", "b.md"}) {
		t.Fatalf("SyncAll 顺序 = %v", files)
	}

	if err := SyncAll(ctx, nil, idx); err == nil {
		t.Fatal("nil store 应报错")
	}
	if err := SyncAll(ctx, st, nil); err == nil {
		t.Fatal("nil indexer 应报错")
	}
}
