package ontology

import (
	"cmp"
	"context"
	"slices"
	"sync"

	"github.com/oekit/pkg/errors"
)

// ErrInvalidDepth 邻接查询的深度为负。
var ErrInvalidDepth = errors.New("ontology: depth 不能为负")

// Store 本体三元组的最小查询面。
//
// 刻意最小：只留「写入 + 邻接查询」两个动作，宿主据此接 PG/Neo4j 自己的实现；
// 本包不建图数据库，也不假设宿主用什么存储。
type Store interface {
	// Upsert 写入/覆盖三元组。三元组的身份是 主体+谓词+客体；
	// 身份相同的记录被覆盖（证据与置信度随之更新）。
	// 三元组缺主体/谓词/客体时报错——非法记录不进存储。
	Upsert(ctx context.Context, ts []Triple) error

	// Neighbors 返回与 subject 相邻（depth 跳以内）的三元组。
	//
	// depth 语义：depth=0 返回空；depth=1 返回与 subject **直接相邻**的三元组；
	// depth=n 递归到 n 跳。邻接**无向**——三元组的主体与客体都算与该点相邻。
	// 返回顺序确定（按主体/谓词/客体字典序），便于复现与 golden 断言。
	Neighbors(ctx context.Context, subject string, depth int) ([]Triple, error)
}

// tripleKey 三元组的身份键：主体+谓词+客体。
type tripleKey struct {
	subject   string
	predicate string
	object    string
}

// MemoryStore 进程内参考实现。零值可用；含 RWMutex，不可复制。
//
// 适合测试、单进程宿主与嵌入式场景；跨进程持久化由宿主自实现（PG/Neo4j 等）。
type MemoryStore struct {
	mu      sync.RWMutex
	triples map[tripleKey]Triple
}

// NewMemoryStore 创建空的内存 Store。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{triples: make(map[tripleKey]Triple)}
}

var _ Store = (*MemoryStore)(nil)

// Upsert 实现 Store。
func (s *MemoryStore) Upsert(ctx context.Context, ts []Triple) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, t := range ts {
		if t.Subject == "" || t.Predicate == "" || t.Object == "" {
			return errors.New("ontology: 三元组缺主体/谓词/客体")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.triples == nil {
		s.triples = make(map[tripleKey]Triple)
	}
	for _, t := range ts {
		s.triples[tripleKey{subject: t.Subject, predicate: t.Predicate, object: t.Object}] = t
	}
	return nil
}

// Neighbors 实现 Store。
func (s *MemoryStore) Neighbors(ctx context.Context, subject string, depth int) ([]Triple, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if depth < 0 {
		return nil, errors.Wrapf(ErrInvalidDepth, "depth=%d", depth)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Triple, 0)
	if depth == 0 || len(s.triples) == 0 {
		return out, nil
	}

	// 邻接索引：节点 → 与之相邻的三元组身份。
	adjacency := make(map[string][]tripleKey)
	for key := range s.triples {
		adjacency[key.subject] = append(adjacency[key.subject], key)
		if key.object != key.subject {
			adjacency[key.object] = append(adjacency[key.object], key)
		}
	}

	visited := map[string]bool{subject: true}
	frontier := []string{subject}
	seen := make(map[tripleKey]bool)
	for hop := 0; hop < depth && len(frontier) > 0; hop++ {
		next := make([]string, 0)
		for _, node := range frontier {
			for _, key := range adjacency[node] {
				if !seen[key] {
					seen[key] = true
					out = append(out, s.triples[key])
				}
				for _, endpoint := range []string{key.subject, key.object} {
					if !visited[endpoint] {
						visited[endpoint] = true
						next = append(next, endpoint)
					}
				}
			}
		}
		frontier = next
	}

	slices.SortFunc(out, func(a, b Triple) int {
		if c := cmp.Compare(a.Subject, b.Subject); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Predicate, b.Predicate); c != 0 {
			return c
		}
		return cmp.Compare(a.Object, b.Object)
	})
	return out, nil
}
