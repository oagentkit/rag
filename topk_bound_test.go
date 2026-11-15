package rag

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// #138：topK 只有下界归一、无上界——1e8 入参让 topKChunks 预分配 ~6.4GB
// 直接 OOM。上界收口后超大入参 clamp 到 maxTopK，检索正常返回。
func TestRetrieveTopKUpperBound_Issue138(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.md"), []byte("# 标题\n\nalpha beta gamma 内容。\n"), 0o644)
	l, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	l.Rescan()

	got, err := l.Retrieve(context.Background(), "alpha", 1e8, nil)
	if err != nil {
		t.Fatalf("超大 topK 应 clamp 而非报错/OOM: %v", err)
	}
	if len(got) > maxTopK {
		t.Fatalf("结果条数 %d 越过上界 %d", len(got), maxTopK)
	}

	// Hybrid 同口径（两路候选 ×2 也不越界）。
	h, err := NewHybrid(l, l)
	if err != nil {
		t.Fatal(err)
	}
	hg, err := h.Retrieve(context.Background(), "alpha", 1e8, nil)
	if err != nil {
		t.Fatalf("Hybrid 超大 topK 应 clamp: %v", err)
	}
	if len(hg) > maxTopK {
		t.Fatalf("Hybrid 结果条数 %d 越过上界 %d", len(hg), maxTopK)
	}
}
