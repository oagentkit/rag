package wiki

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/oekit/pkg/errors"
)

// 错误哨兵。存储实现须保证 errors.Is 可判定（包装时带上下文即可）。
var (
	// ErrNotFound 页面不存在。
	ErrNotFound = errors.New("wiki: 页面不存在")
	// ErrVersionConflict 乐观版本冲突：调用方持有的版本与存储不一致。
	ErrVersionConflict = errors.New("wiki: 页面版本冲突")
	// ErrInvalidSlug slug 不合法（空、路径分隔符、控制字符等）。
	ErrInvalidSlug = errors.New("wiki: 非法 slug")
	// ErrInvalidSource Ingest 的 Source 不合法（目前仅要求 ID 非空）。
	ErrInvalidSource = errors.New("wiki: 非法 source")
)

// maxSlugBytes slug 字节上限。一页一文件后端会追加 ".md"，留足常见
// 文件系统 255 字节文件名的余量。
const maxSlugBytes = 200

// Page wiki 页。
//
// Slug 是稳定 ID（链接目标），Title/Body 是叙事内容，Links/Tags/Sources 是
// 结构元数据。Version 由 Store 维护：新建为 1，每次成功更新 +1；调用方不应
// 自行递增。
type Page struct {
	Slug    string    // 稳定 ID（链接目标；一页一文件后端同时是文件名主干）
	Title   string    // 标题
	Body    string    // markdown 正文
	Links   []string  // 出链（slug）
	Tags    []string  // 标签
	Sources []string  // 依据（URL/文件/会话 ID）——引用可回溯
	Updated time.Time // 内容最后变更时间
	Version uint64    // 存储维护的乐观版本号
}

// PageFileName 页面在「一页一文件」后端里的文件名（<slug>.md）。
// fsstore 与 rag.Local 共目录、Indexer 的 file 键都使用这一单源命名。
func PageFileName(slug string) string { return slug + ".md" }

// ValidateSlug 校验 slug 可用作稳定 ID 与一页一文件后端的文件名主干。
//
// 规则：非空、≤ 200 字节、不是 "."/".."、不以 "." 开头、首尾无空白、
// 不以 "." 结尾、不含控制字符与路径/跨平台文件名保留字符（/ \ : * ? " < > |）。
// Unicode 字母/数字允许（中文 slug 合法）。
func ValidateSlug(slug string) error {
	switch {
	case slug == "":
		return errors.Wrapf(ErrInvalidSlug, "slug 为空")
	case len(slug) > maxSlugBytes:
		return errors.Wrapf(ErrInvalidSlug, "slug 超过 %d 字节", maxSlugBytes)
	case slug == "." || slug == "..":
		return errors.Wrapf(ErrInvalidSlug, "slug %q 是路径占位符", slug)
	case strings.HasPrefix(slug, "."):
		return errors.Wrapf(ErrInvalidSlug, "slug %q 不得以 '.' 开头", slug)
	case strings.TrimSpace(slug) != slug:
		return errors.Wrapf(ErrInvalidSlug, "slug %q 首尾不得有空白", slug)
	case strings.HasSuffix(slug, "."):
		return errors.Wrapf(ErrInvalidSlug, "slug %q 不得以 '.' 结尾", slug)
	}
	for _, r := range slug {
		if unicode.IsControl(r) {
			return errors.Wrapf(ErrInvalidSlug, "slug %q 含控制字符", slug)
		}
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return errors.Wrapf(ErrInvalidSlug, "slug %q 含文件名保留字符 %q", slug, r)
		}
	}
	return nil
}

// VersionConflictError 乐观版本冲突详情。errors.Is(err, ErrVersionConflict)
// 可判定；errors.As 可取 Expected/Actual 做重试或诊断。
type VersionConflictError struct {
	Slug     string
	Expected uint64
	Actual   uint64
}

// Error 实现 error。
func (e *VersionConflictError) Error() string {
	return fmt.Sprintf("wiki: 页面 %q 版本冲突（期望 %d，实际 %d）", e.Slug, e.Expected, e.Actual)
}

// Unwrap 让 errors.Is(err, ErrVersionConflict) 成立。
func (e *VersionConflictError) Unwrap() error { return ErrVersionConflict }

// clonePage 深拷贝页面，隔离存储与调用方持有的切片。
func clonePage(p *Page) *Page {
	if p == nil {
		return nil
	}
	cp := *p
	cp.Links = slices.Clone(p.Links)
	cp.Tags = slices.Clone(p.Tags)
	cp.Sources = slices.Clone(p.Sources)
	return &cp
}

// normalizeSet 去空白、去重、排序。Links/Tags/Sources 是集合语义：归一后
// 编译器换顺序不会造成假更新，Lint 输出也确定。
func normalizeSet(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	slices.Sort(out)
	return out
}
