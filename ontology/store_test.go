package ontology

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// storeTriples 合成夹具：一条四节点链（node-a → node-b → node-c → node-d）。
func storeTriples() []Triple {
	return []Triple{
		{Subject: "node-a", Predicate: "link", Object: "node-b", Evidence: "chunk-1", Confidence: 0.9},
		{Subject: "node-b", Predicate: "link", Object: "node-c", Evidence: "chunk-2", Confidence: 0.9},
		{Subject: "node-c", Predicate: "link", Object: "node-d", Evidence: "chunk-3", Confidence: 0.9},
	}
}

// golden 把三元组序列渲染成可断言的文本。
func golden(ts []Triple) string {
	lines := make([]string, 0, len(ts))
	for _, t := range ts {
		lines = append(lines, t.Subject+"|"+t.Predicate+"|"+t.Object+"|"+t.Evidence)
	}
	return strings.Join(lines, "\n")
}

func TestMemoryStoreEmptyLibrary(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	got, err := st.Neighbors(ctx, "node-a", 2)
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("空库返回 %d 条，期望 0：%v", len(got), got)
	}
}

func TestMemoryStoreNeighborsDepthSemantics(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	if err := st.Upsert(ctx, storeTriples()); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	cases := []struct {
		depth int
		want  string
	}{
		{depth: 0, want: ""},
		{depth: 1, want: "node-a|link|node-b|chunk-1"},
		{depth: 2, want: "node-a|link|node-b|chunk-1\nnode-b|link|node-c|chunk-2"},
		{
			depth: 3,
			want:  "node-a|link|node-b|chunk-1\nnode-b|link|node-c|chunk-2\nnode-c|link|node-d|chunk-3",
		},
		{
			depth: 9,
			want:  "node-a|link|node-b|chunk-1\nnode-b|link|node-c|chunk-2\nnode-c|link|node-d|chunk-3",
		},
	}
	for _, tc := range cases {
		got, err := st.Neighbors(ctx, "node-a", tc.depth)
		if err != nil {
			t.Fatalf("Neighbors(depth=%d): %v", tc.depth, err)
		}
		if g := golden(got); g != tc.want {
			t.Errorf("Neighbors(depth=%d) =\n%s\n期望\n%s", tc.depth, g, tc.want)
		}
	}
}

func TestMemoryStoreNeighborsIsUndirected(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	if err := st.Upsert(ctx, storeTriples()); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	// 从链路中段出发，向两侧各走一跳。
	got, err := st.Neighbors(ctx, "node-c", 1)
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	want := "node-b|link|node-c|chunk-2\nnode-c|link|node-d|chunk-3"
	if g := golden(got); g != want {
		t.Fatalf("Neighbors(node-c, 1) =\n%s\n期望\n%s", g, want)
	}
}

func TestMemoryStoreNeighborsDeterministicOrder(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	// 乱序写入：结果顺序必须与写入顺序无关。
	ts := storeTriples()
	shuffled := []Triple{ts[2], ts[0], ts[1]}
	if err := st.Upsert(ctx, shuffled); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	first, err := st.Neighbors(ctx, "node-a", 3)
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	for i := 0; i < 5; i++ {
		again, err := st.Neighbors(ctx, "node-a", 3)
		if err != nil {
			t.Fatalf("Neighbors: %v", err)
		}
		if golden(first) != golden(again) {
			t.Fatalf("第 %d 次调用结果不同：\n%s\n%s", i, golden(first), golden(again))
		}
	}
	want := "node-a|link|node-b|chunk-1\nnode-b|link|node-c|chunk-2\nnode-c|link|node-d|chunk-3"
	if g := golden(first); g != want {
		t.Fatalf("Neighbors =\n%s\n期望\n%s", g, want)
	}
}

func TestMemoryStoreUnknownSubject(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	if err := st.Upsert(ctx, storeTriples()); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := st.Neighbors(ctx, "node-x", 3)
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("未知主体返回 %d 条，期望 0：%v", len(got), got)
	}
}

func TestMemoryStoreSelfLoopDoesNotPanic(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	loop := Triple{Subject: "node-a", Predicate: "link", Object: "node-a", Evidence: "chunk-4", Confidence: 0.5}
	if err := st.Upsert(ctx, []Triple{loop}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := st.Neighbors(ctx, "node-a", 5)
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("自环返回 %d 条，期望 1：%v", len(got), got)
	}
}

func TestMemoryStoreUpsertOverwritesSameIdentity(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	first := Triple{Subject: "node-a", Predicate: "link", Object: "node-b", Evidence: "chunk-1", Confidence: 0.4}
	if err := st.Upsert(ctx, []Triple{first}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	second := Triple{Subject: "node-a", Predicate: "link", Object: "node-b", Evidence: "chunk-9", Confidence: 0.9}
	if err := st.Upsert(ctx, []Triple{second}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := st.Neighbors(ctx, "node-a", 1)
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("同身份覆盖后有 %d 条，期望 1：%v", len(got), got)
	}
	if got[0].Evidence != "chunk-9" || got[0].Confidence != 0.9 {
		t.Fatalf("覆盖后内容 = %+v，期望 chunk-9/0.9", got[0])
	}
}

func TestMemoryStoreUpsertRejectsIncompleteTriple(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	err := st.Upsert(ctx, []Triple{{Subject: "node-a", Predicate: "link", Evidence: "chunk-1"}})
	if err == nil {
		t.Fatal("Upsert 缺客体的三元组 = nil，期望报错")
	}
}

func TestMemoryStoreNegativeDepth(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	_, err := st.Neighbors(ctx, "node-a", -1)
	if !errors.Is(err, ErrInvalidDepth) {
		t.Fatalf("Neighbors(depth=-1) = %v，期望 ErrInvalidDepth", err)
	}
}

func TestMemoryStoreRespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	st := NewMemoryStore()
	if err := st.Upsert(ctx, storeTriples()); err == nil {
		t.Fatal("Upsert 在 ctx 取消后 = nil，期望报错")
	}
	if _, err := st.Neighbors(ctx, "node-a", 1); err == nil {
		t.Fatal("Neighbors 在 ctx 取消后 = nil，期望报错")
	}
}

func TestMemoryStoreConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	st := NewMemoryStore()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			suffix := fmt.Sprintf("%c", 'a'+i)
			trip := Triple{
				Subject:    "node-a",
				Predicate:  "link",
				Object:     "node-" + suffix,
				Evidence:   "chunk-" + suffix,
				Confidence: 0.5,
			}
			if err := st.Upsert(ctx, []Triple{trip}); err != nil {
				t.Errorf("Upsert: %v", err)
			}
			if _, err := st.Neighbors(ctx, "node-a", 2); err != nil {
				t.Errorf("Neighbors: %v", err)
			}
		}(i)
	}
	wg.Wait()
}
