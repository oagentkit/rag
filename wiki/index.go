package wiki

import (
	"context"

	"github.com/oekit/pkg/errors"
)

// Indexer 是 wiki 与 rag 读侧索引的写入接缝。wiki 不做检索：页面写入后由
// 宿主通过 Indexer 同步给既有索引后端，保证「写入返回即可检索」。
//
// *milvus.Store（向量后端子包）的方法集天然满足本接口（Index/DeleteFile）；文件后端与
// rag.Local 共目录时，可用调用 Local.Rescan 的适配器实现。
type Indexer interface {
	// Index 写入/覆盖一个页面的索引。file 使用 PageFileName(slug)；返回 nil
	// 后该页面必须已可检索（同步可见，不得依赖后台重扫）。
	Index(ctx context.Context, file string, content string) error
	// DeleteFile 删除该 file 的全部索引数据。
	DeleteFile(ctx context.Context, file string) error
}

// IndexedStore 在 Store 写成功后同步索引。Get/List 直接委托内层；
// Put/Delete 先落库、后索引。
//
// 失败语义：存储写成功而索引失败时返回 error，但**存储已提交**（页面版本
// 已递增）。调用方可用 SyncAll 做全量对账/重放，索引实现应尽量幂等。
type IndexedStore struct {
	inner Store
	idx   Indexer
}

// NewIndexedStore 用 Indexer 装饰 Store。
//
// inner 为 nil 返回 nil；idx 为 nil 时退化为 inner（不索引）。
func NewIndexedStore(inner Store, idx Indexer) Store {
	if inner == nil {
		return nil
	}
	if idx == nil {
		return inner
	}
	return &IndexedStore{inner: inner, idx: idx}
}

var _ Store = (*IndexedStore)(nil)

// Get 实现 Store（委托）。
func (s *IndexedStore) Get(ctx context.Context, slug string) (*Page, error) {
	return s.inner.Get(ctx, slug)
}

// List 实现 Store（委托）。
func (s *IndexedStore) List(ctx context.Context) ([]*Page, error) {
	return s.inner.List(ctx)
}

// Put 实现 Store：内层写成功后按 PageFileName(slug)/Body 同步索引。
func (s *IndexedStore) Put(ctx context.Context, page *Page) error {
	if err := s.inner.Put(ctx, page); err != nil {
		return err
	}
	if err := s.idx.Index(ctx, PageFileName(page.Slug), page.Body); err != nil {
		return errors.Wrapf(err, "wiki: 页面 %q 已写入但索引失败", page.Slug)
	}
	return nil
}

// Delete 实现 Store：内层删除成功后清理索引。
func (s *IndexedStore) Delete(ctx context.Context, slug string, expectedVersion uint64) error {
	if err := s.inner.Delete(ctx, slug, expectedVersion); err != nil {
		return err
	}
	if err := s.idx.DeleteFile(ctx, PageFileName(slug)); err != nil {
		return errors.Wrapf(err, "wiki: 页面 %q 已删除但索引清理失败", slug)
	}
	return nil
}

// SyncAll 把存储中的全部页面按 slug 升序重新推给索引，用于首次接入或索引
// 失败后的对账/重放。删除对账不在此列：调用方应结合后端能力（如 Milvus 的
// Sweep）清理已不存在的页面。
func SyncAll(ctx context.Context, st Store, idx Indexer) error {
	if st == nil {
		return errors.New("wiki: SyncAll 需要非 nil Store")
	}
	if idx == nil {
		return errors.New("wiki: SyncAll 需要非 nil Indexer")
	}
	pages, err := st.List(ctx)
	if err != nil {
		return errors.Wrapf(err, "wiki: SyncAll 读取页面失败")
	}
	for _, p := range pages {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := idx.Index(ctx, PageFileName(p.Slug), p.Body); err != nil {
			return errors.Wrapf(err, "wiki: SyncAll 同步页面 %q 失败", p.Slug)
		}
	}
	return nil
}
