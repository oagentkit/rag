package wiki

import (
	"cmp"
	"context"
	"slices"
	"sync"

	"github.com/oekit/pkg/errors"
)

// MemoryStore 进程内参考实现。零值可用；含 RWMutex，不可复制。
//
// 适合测试、单进程宿主与嵌入式场景；跨进程持久化用 fsstore 或宿主自实现。
type MemoryStore struct {
	mu    sync.RWMutex
	pages map[string]*Page
}

// NewMemoryStore 创建空的内存 Store。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{pages: make(map[string]*Page)}
}

var _ Store = (*MemoryStore)(nil)

// Get 实现 Store。
func (s *MemoryStore) Get(ctx context.Context, slug string) (*Page, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.pages[slug]
	if !ok {
		return nil, errors.Wrapf(ErrNotFound, "slug %q", slug)
	}
	return clonePage(p), nil
}

// List 实现 Store。
func (s *MemoryStore) List(ctx context.Context) ([]*Page, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Page, 0, len(s.pages))
	for _, p := range s.pages {
		out = append(out, clonePage(p))
	}
	slices.SortFunc(out, func(a, b *Page) int { return cmp.Compare(a.Slug, b.Slug) })
	return out, nil
}

// Put 实现 Store。
func (s *MemoryStore) Put(ctx context.Context, page *Page) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if page == nil {
		return errors.New("wiki: Page 为 nil")
	}
	if err := ValidateSlug(page.Slug); err != nil {
		return err
	}
	next := clonePage(page)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pages == nil {
		s.pages = make(map[string]*Page)
	}
	cur, exists := s.pages[page.Slug]
	switch {
	case !exists:
		if next.Version != 0 {
			return errors.Wrapf(ErrNotFound, "slug %q 不存在，无法按版本 %d 更新", page.Slug, next.Version)
		}
		next.Version = 1
	case next.Version == 0:
		return &VersionConflictError{Slug: page.Slug, Expected: 0, Actual: cur.Version}
	case next.Version != cur.Version:
		return &VersionConflictError{Slug: page.Slug, Expected: next.Version, Actual: cur.Version}
	default:
		next.Version = cur.Version + 1
	}
	s.pages[page.Slug] = next
	page.Version = next.Version
	return nil
}

// Delete 实现 Store。
func (s *MemoryStore) Delete(ctx context.Context, slug string, expectedVersion uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateSlug(slug); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.pages[slug]
	if !ok {
		return errors.Wrapf(ErrNotFound, "slug %q", slug)
	}
	if expectedVersion != 0 && expectedVersion != cur.Version {
		return &VersionConflictError{Slug: slug, Expected: expectedVersion, Actual: cur.Version}
	}
	delete(s.pages, slug)
	return nil
}
