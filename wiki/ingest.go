package wiki

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/oekit/pkg/errors"
)

// Source 一次增量摄取的源材料。ID 必填（URL/文件路径/会话 ID），用于
// 来源追溯与幂等判定；Metadata 承载宿主自定义字段（如建议的 slug），内核
// 不解释。
type Source struct {
	ID       string
	Title    string
	Content  string
	Tags     []string
	Metadata map[string]string
}

// Compiler 把源材料编译为页面草稿。提示词与领域强相关，内核不内置：
// 宿主给一个「source + 既有页面 → 页面草稿」的函数。
//
// existing 是存储快照的深拷贝，Compiler 可自由修改；返回值中的 Version 与
// Updated 会被内核覆盖（版本由 Store 维护、更新时间由 Ingest 决定）。
type Compiler func(ctx context.Context, source Source, existing []*Page) ([]*Page, error)

// Result 一次 Ingest 的结果。三个 slug 列表均按字典序排序；Findings 是合并
// 落库后对全库执行 Lint 的结果（幂等重放也会返回当前库的结构问题）。
type Result struct {
	Created   []string  // 新建的 slug
	Updated   []string  // 内容变更的 slug
	Unchanged []string  // 内容未变、未写库的 slug
	Findings  []Finding // 合并后的全库 Lint 结果
}

// IngestOption Ingest 的可选行为。
type IngestOption func(*ingestConfig)

type ingestConfig struct {
	now func() time.Time
}

// WithNow 注入时钟（测试/确定性构建用）。为 nil 时忽略；默认 time.Now。
//
// Ingest 在新建或内容变更时把 Updated 写为 now().UTC()；幂等重放不调用时钟、
// 不改 Updated。
func WithNow(now func() time.Time) IngestOption {
	return func(cfg *ingestConfig) {
		if now != nil {
			cfg.now = now
		}
	}
}

// ingestPutAttempts 乐观写重试上限。冲突会重新 Get + 合并，语义是
// compare-and-swap；持续冲突说明高竞争或 Store 实现异常，交由调用方处理。
const ingestPutAttempts = 5

// Ingest 增量摄取：compile → 合并（同 slug 更新、新 slug 追加）→ Lint。
//
// 合并语义（与 Compiler 的分工）：
//   - Title/Body/Links/Tags 以草稿为准（Compiler 已拿到 existing，可自行保留
//     或改写；Links/Tags 按集合归一：去空白、去重、排序）；
//   - Sources 取既有 ∪ 草稿 ∪ {source.ID} 的并集——来源只增不减，保证引用
//     可回溯；
//   - Updated 只在新建或内容（含 Sources）实际变化时更新；同一 source 重复
//     摄取不产新页、不改 Updated、不涨版本；
//   - Version/Updated 即使草稿给了值也由 Ingest 覆盖。
//
// 写路径使用 Store 的乐观版本：冲突时重读并重新合并，最多 ingestPutAttempts
// 次；并发摄取不同 source 会收敛为 Sources 并集。Lint 在写完后执行，读到的
// 是最终库状态。
//
// Ingest 不是跨页事务：某页写失败会中止并返回错误，此前已写成功的页面保留
// （单页写本身是原子的；重放幂等，宿主可安全重试整个 Ingest）。
func Ingest(ctx context.Context, st Store, c Compiler, src Source, opts ...IngestOption) (Result, error) {
	var res Result
	if err := ctx.Err(); err != nil {
		return res, err
	}
	if st == nil {
		return res, errors.New("wiki: Store 为 nil")
	}
	if c == nil {
		return res, errors.New("wiki: Compiler 为 nil")
	}
	sourceID := strings.TrimSpace(src.ID)
	if sourceID == "" {
		return res, errors.Wrapf(ErrInvalidSource, "Source.ID 为空")
	}

	cfg := ingestConfig{now: time.Now}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	now := cfg.now
	if now == nil {
		now = time.Now
	}

	existing, err := st.List(ctx)
	if err != nil {
		return res, errors.Wrapf(err, "wiki: 读取既有页面失败")
	}
	drafts, err := c(ctx, src, existing)
	if err != nil {
		return res, errors.Wrapf(err, "wiki: 编译 source %q 失败", sourceID)
	}
	if err := ctx.Err(); err != nil {
		return res, err
	}

	drafts, err = validateDrafts(drafts)
	if err != nil {
		return res, err
	}

	for _, draft := range drafts {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		action, err := ingestPage(ctx, st, draft, sourceID, now)
		if err != nil {
			return res, errors.Wrapf(err, "wiki: 摄取 slug %q 失败", draft.Slug)
		}
		switch action {
		case actionCreated:
			res.Created = append(res.Created, draft.Slug)
		case actionUpdated:
			res.Updated = append(res.Updated, draft.Slug)
		case actionUnchanged:
			res.Unchanged = append(res.Unchanged, draft.Slug)
		}
	}

	pages, err := st.List(ctx)
	if err != nil {
		return res, errors.Wrapf(err, "wiki: Lint 读取页面失败")
	}
	res.Findings = Lint(pages)

	slices.Sort(res.Created)
	slices.Sort(res.Updated)
	slices.Sort(res.Unchanged)
	return res, nil
}

// validateDrafts 校验并归一 Compiler 输出：nil 页、非法 slug、重复 slug 都是
// 编程错误，在任何写库之前 fail-fast；输出按 slug 排序保证写序确定。
func validateDrafts(drafts []*Page) ([]*Page, error) {
	out := make([]*Page, 0, len(drafts))
	seen := make(map[string]struct{}, len(drafts))
	for i, draft := range drafts {
		if draft == nil {
			return nil, errors.Newf("wiki: Compiler 返回的第 %d 个页面为 nil", i)
		}
		if err := ValidateSlug(draft.Slug); err != nil {
			return nil, err
		}
		if _, ok := seen[draft.Slug]; ok {
			return nil, errors.Newf("wiki: Compiler 输出重复 slug %q", draft.Slug)
		}
		seen[draft.Slug] = struct{}{}
		out = append(out, clonePage(draft))
	}
	slices.SortFunc(out, func(a, b *Page) int { return cmp.Compare(a.Slug, b.Slug) })
	return out, nil
}

type ingestAction int

const (
	actionUnchanged ingestAction = iota
	actionCreated
	actionUpdated
)

// ingestPage 单页 compare-and-swap：Get → merge → Put；版本冲突则重读重试。
func ingestPage(ctx context.Context, st Store, draft *Page, sourceID string, now func() time.Time) (ingestAction, error) {
	for range ingestPutAttempts {
		if err := ctx.Err(); err != nil {
			return actionUnchanged, err
		}
		cur, err := st.Get(ctx, draft.Slug)
		created := false
		switch {
		case err == nil && cur == nil:
			return actionUnchanged, errors.Newf("wiki: Store 对 slug %q 返回 nil 页面", draft.Slug)
		case err == nil:
		case errors.Is(err, ErrNotFound):
			created = true
		default:
			return actionUnchanged, err
		}

		next, changed := mergeDraft(cur, draft, sourceID, now().UTC())
		if !changed {
			return actionUnchanged, nil
		}
		if created {
			next.Version = 0
		} else {
			next.Version = cur.Version
		}
		if err := st.Put(ctx, next); err != nil {
			if errors.Is(err, ErrVersionConflict) {
				continue
			}
			return actionUnchanged, err
		}
		if created {
			return actionCreated, nil
		}
		return actionUpdated, nil
	}
	return actionUnchanged, errors.Wrapf(ErrVersionConflict, "slug %q 重试 %d 次后仍冲突", draft.Slug, ingestPutAttempts)
}

// mergeDraft 把草稿合并到既有页（cur 为 nil 表示新建）。返回 changed=false
// 时返回 cur 本身——调用方跳过写库，Updated/Version 原样保留。
func mergeDraft(cur, draft *Page, sourceID string, now time.Time) (*Page, bool) {
	sources := make([]string, 0, len(draft.Sources)+1)
	if cur != nil {
		sources = append(sources, cur.Sources...)
	}
	sources = append(sources, draft.Sources...)
	sources = append(sources, sourceID)

	next := &Page{
		Slug:    draft.Slug,
		Title:   draft.Title,
		Body:    draft.Body,
		Links:   normalizeSet(draft.Links),
		Tags:    normalizeSet(draft.Tags),
		Sources: normalizeSet(sources),
	}
	if cur == nil {
		next.Updated = now
		return next, true
	}
	if pageContentEqual(cur, next) {
		return cur, false
	}
	next.Version = cur.Version
	next.Updated = now
	return next, true
}

// pageContentEqual 语义内容比较：集合字段先归一（既有页可能由直接 Put 写入，
// 未必已归一），忽略 Version/Updated。
func pageContentEqual(a, b *Page) bool {
	return a.Title == b.Title &&
		a.Body == b.Body &&
		slices.Equal(normalizeSet(a.Links), b.Links) &&
		slices.Equal(normalizeSet(a.Tags), b.Tags) &&
		slices.Equal(normalizeSet(a.Sources), b.Sources)
}
