package wiki

import "context"

// Store 页面存储抽象。存储无关：文件系统 / SQL / 对象存储由宿主实现；
// 本包提供 MemoryStore 与 fsstore 两个零依赖参考实现。
//
// 实现契约：
//   - Get/List 返回深拷贝（调用方修改不得反向影响存储），List 按 Slug 升序；
//   - Put 按 Page.Version 做乐观并发校验并递增版本；
//   - 页面不存在返回 ErrNotFound，版本不符返回 ErrVersionConflict；
//   - 不解释正文（markdown 原样保存）。
type Store interface {
	// Get 取单页；不存在返回 ErrNotFound。
	Get(ctx context.Context, slug string) (*Page, error)

	// List 列出全部页面（按 Slug 升序）；空库返回空切片。
	List(ctx context.Context) ([]*Page, error)

	// Put 新建或更新页面（upsert）。Page.Version 是调用方持有的乐观版本：
	//   - 新建：必须为 0，成功后写回 1；
	//   - 更新：必须等于当前存储版本，成功后写回当前版本 + 1；
	//   - 新建时页面已存在、或更新时版本不符：返回 ErrVersionConflict；
	//   - 更新时页面不存在：返回 ErrNotFound。
	// 实现只改写 Page.Version，其余字段原样持久化。
	Put(ctx context.Context, page *Page) error

	// Delete 删除页面。expectedVersion > 0 时要求与当前版本一致，否则返回
	// ErrVersionConflict；0 表示无条件删除。页面不存在返回 ErrNotFound。
	Delete(ctx context.Context, slug string, expectedVersion uint64) error
}
