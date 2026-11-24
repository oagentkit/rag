package wiki_test

import (
	"context"
	"strings"
	"testing"

	"github.com/oagentkit/rag"
	"github.com/oagentkit/rag/milvus"
	"github.com/oagentkit/rag/wiki"
	"github.com/oagentkit/rag/wiki/fsstore"
)

// rescanIndexer 把 wiki 写入桥接到 rag.Local：Local 只在构造时扫描一次，
// 显式 Rescan 后立即可检索（不等 10 分钟自动过期）。
type rescanIndexer struct{ local *rag.Local }

func (r rescanIndexer) Index(context.Context, string, string) error {
	r.local.Rescan()
	return nil
}

func (r rescanIndexer) DeleteFile(context.Context, string) error {
	r.local.Rescan()
	return nil
}

// 向量后端（rag/milvus 子包）的 Index/DeleteFile 方法集应直接满足
// wiki.Indexer——接缝不要求为既有后端写适配器。
var _ wiki.Indexer = (*milvus.Store)(nil)

func TestWikiWriteIsImmediatelyRetrievable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	fs, err := fsstore.New(dir)
	if err != nil {
		t.Fatalf("fsstore.New: %v", err)
	}
	local, err := rag.NewLocal(dir)
	if err != nil {
		t.Fatalf("rag.NewLocal: %v", err)
	}
	st := wiki.NewIndexedStore(fs, rescanIndexer{local: local})

	compiler := func(_ context.Context, src wiki.Source, _ []*wiki.Page) ([]*wiki.Page, error) {
		return []*wiki.Page{{
			Slug:  src.Metadata["slug"],
			Title: src.Title,
			Body:  src.Content,
		}}, nil
	}

	if chunks, err := local.Retrieve(ctx, "zebracorn", 3, nil); err != nil || len(chunks) != 0 {
		t.Fatalf("写入前检索 = %d, %v，期望空", len(chunks), err)
	}

	src := wiki.Source{
		ID:      "session-144",
		Title:   "磁盘告警处置",
		Content: "zebracorn 磁盘空间不足时检查 inode 与日志轮转。",
		Metadata: map[string]string{
			"slug": "disk-runbook",
		},
	}
	if _, err := wiki.Ingest(ctx, st, compiler, src); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	chunks, err := local.Retrieve(ctx, "zebracorn", 3, nil)
	if err != nil {
		t.Fatalf("写入后检索: %v", err)
	}
	if len(chunks) == 0 || !strings.Contains(chunks[0].Content, "zebracorn") {
		t.Fatalf("写入后未立即检索到: %+v", chunks)
	}

	// 更新后新内容立即可见，旧内容不再命中。
	src.Content = "quuxmander 磁盘告警需要先确认挂载点。"
	if _, err := wiki.Ingest(ctx, st, compiler, src); err != nil {
		t.Fatalf("更新 Ingest: %v", err)
	}
	chunks, err = local.Retrieve(ctx, "quuxmander", 3, nil)
	if err != nil || len(chunks) == 0 {
		t.Fatalf("更新后未检索到新内容: %d, %v", len(chunks), err)
	}
	chunks, err = local.Retrieve(ctx, "zebracorn", 3, nil)
	if err != nil {
		t.Fatalf("旧内容检索: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("旧内容仍被检索到: %+v", chunks)
	}

	// 删除后立即不可见。
	if err := st.Delete(ctx, "disk-runbook", 0); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	chunks, err = local.Retrieve(ctx, "quuxmander", 3, nil)
	if err != nil {
		t.Fatalf("删除后检索: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("删除后仍可检索: %+v", chunks)
	}
}
