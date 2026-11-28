// Package fsstore 提供 wiki.Store 的文件系统参考实现：一页一个 markdown
// 文件（<slug>.md），元数据存 YAML front-matter，正文原样保存。
//
// 零外部服务依赖（CI 零依赖）。写入用「同目录临时文件 + os.Rename」原子替换
// （Unix 语义）；并发控制是进程内互斥，跨进程写入需要宿主自己做锁——
// 本实现是参考实现，不是分布式存储。
//
// 文件由 Store.Put 生成；手工放入的 markdown 也必须带 version ≥ 1 的
// front-matter（updated 可省略，视为零值时间）。
//
// 生命周期: Experimental——参考实现形态随 wiki 契约演进，未过
// api-compat.md 晋级三维，初值如实记为不承诺兼容。
package fsstore

import (
	"bytes"
	"cmp"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/oekit/pkg/errors"

	"github.com/oagentkit/rag/wiki"
)

// Store 文件系统 Store。含 RWMutex，不可复制。
type Store struct {
	dir string
	mu  sync.RWMutex
}

var _ wiki.Store = (*Store)(nil)

// New 创建/打开目录 dir 作为 wiki 存储。目录不存在时创建；路径是文件时报错。
func New(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("fsstore: 目录为空")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, errors.Wrapf(err, "fsstore: 创建目录 %q 失败", dir)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return nil, errors.Wrapf(err, "fsstore: 检查目录 %q 失败", dir)
	}
	if !fi.IsDir() {
		return nil, errors.Newf("fsstore: %q 不是目录", dir)
	}
	return &Store{dir: dir}, nil
}

// Dir 返回存储根目录。
func (s *Store) Dir() string { return s.dir }

// Get 实现 wiki.Store。
func (s *Store) Get(ctx context.Context, slug string) (*wiki.Page, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.filePath(slug)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readPageLocked(slug, path)
}

// List 实现 wiki.Store。只认目录顶层的 *.md；子目录、隐藏文件（含临时文件）
// 与非 markdown 文件忽略。任一 markdown 损坏都返回错误（不静默跳过）。
func (s *Store) List(ctx context.Context) ([]*wiki.Page, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, errors.Wrapf(err, "fsstore: 读取目录 %q 失败", s.dir)
	}
	pages := make([]*wiki.Page, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") {
			continue
		}
		slug := strings.TrimSuffix(name, ".md")
		if err := wiki.ValidateSlug(slug); err != nil {
			return nil, errors.Wrapf(err, "fsstore: 文件名 %q 不是合法 slug", name)
		}
		p, err := s.readPageLocked(slug, filepath.Join(s.dir, name))
		if err != nil {
			return nil, err
		}
		pages = append(pages, p)
	}
	slices.SortFunc(pages, func(a, b *wiki.Page) int { return cmp.Compare(a.Slug, b.Slug) })
	return pages, nil
}

// Put 实现 wiki.Store：乐观版本校验 + 原子替换。
func (s *Store) Put(ctx context.Context, page *wiki.Page) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if page == nil {
		return errors.New("fsstore: Page 为 nil")
	}
	path, err := s.filePath(page.Slug)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cur, err := s.readPageLocked(page.Slug, path)
	created := false
	switch {
	case err == nil:
		switch {
		case page.Version == 0:
			return &wiki.VersionConflictError{Slug: page.Slug, Expected: 0, Actual: cur.Version}
		case page.Version != cur.Version:
			return &wiki.VersionConflictError{Slug: page.Slug, Expected: page.Version, Actual: cur.Version}
		}
	case errors.Is(err, wiki.ErrNotFound):
		if page.Version != 0 {
			return errors.Wrapf(wiki.ErrNotFound, "fsstore: slug %q 不存在，无法按版本 %d 更新", page.Slug, page.Version)
		}
		created = true
	default:
		return err
	}

	next := *page
	if created {
		next.Version = 1
	} else {
		next.Version = cur.Version + 1
	}
	data, err := marshalPage(&next)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, data); err != nil {
		return err
	}
	page.Version = next.Version
	return nil
}

// Delete 实现 wiki.Store。
func (s *Store) Delete(ctx context.Context, slug string, expectedVersion uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.filePath(slug)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cur, err := s.readPageLocked(slug, path)
	if err != nil {
		return err
	}
	if expectedVersion != 0 && expectedVersion != cur.Version {
		return &wiki.VersionConflictError{Slug: slug, Expected: expectedVersion, Actual: cur.Version}
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return errors.Wrapf(wiki.ErrNotFound, "slug %q", slug)
		}
		return errors.Wrapf(err, "fsstore: 删除 %q 失败", path)
	}
	return nil
}

// filePath 把 slug 映射到安全路径（ValidateSlug 拒绝路径分隔符）。
func (s *Store) filePath(slug string) (string, error) {
	if err := wiki.ValidateSlug(slug); err != nil {
		return "", err
	}
	return filepath.Join(s.dir, wiki.PageFileName(slug)), nil
}

// readPageLocked 读并解析单页。调用方须持锁（Put/Delete 的读-改-写序列
// 需要互斥）。
func (s *Store) readPageLocked(slug, path string) (*wiki.Page, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errors.Wrapf(wiki.ErrNotFound, "slug %q", slug)
		}
		return nil, errors.Wrapf(err, "fsstore: 读取 %q 失败", path)
	}
	p, err := parsePage(slug, data)
	if err != nil {
		return nil, errors.Wrapf(err, "fsstore: 解析 %q 失败", path)
	}
	return p, nil
}

// frontMatter 落盘元数据。slug 是冗余字段（文件名已是身份），写出来便于
// 人工阅读与外部工具消费；回读时以文件名为准。
type frontMatter struct {
	Slug    string   `yaml:"slug"`
	Title   string   `yaml:"title,omitempty"`
	Links   []string `yaml:"links,omitempty"`
	Tags    []string `yaml:"tags,omitempty"`
	Sources []string `yaml:"sources,omitempty"`
	Updated string   `yaml:"updated,omitempty"`
	Version uint64   `yaml:"version"`
}

// marshalPage 渲染 front-matter + 正文。正文原样追加（不做换行归一）。
func marshalPage(p *wiki.Page) ([]byte, error) {
	fm := frontMatter{
		Slug:    p.Slug,
		Title:   p.Title,
		Links:   p.Links,
		Tags:    p.Tags,
		Sources: p.Sources,
		Version: p.Version,
	}
	if !p.Updated.IsZero() {
		fm.Updated = p.Updated.UTC().Format(time.RFC3339Nano)
	}
	head, err := yaml.Marshal(fm)
	if err != nil {
		return nil, errors.Wrapf(err, "fsstore: 序列化 front-matter 失败")
	}
	var buf bytes.Buffer
	buf.Grow(len(head) + len(p.Body) + 8)
	buf.WriteString("---\n")
	buf.Write(head)
	buf.WriteString("---\n\n")
	buf.WriteString(p.Body)
	return buf.Bytes(), nil
}

// parsePage 解析 front-matter + 正文。围栏必须出现在首行（允许 BOM）；
// 闭合围栏是第一行独立的 "---"。正文与闭合围栏之间的一个分隔换行会被剥掉，
// 其余字节原样保留。
func parsePage(slug string, data []byte) (*wiki.Page, error) {
	s := strings.TrimPrefix(string(data), "\ufeff")
	lines := strings.SplitAfter(s, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r\n") != "---" {
		return nil, errors.New("fsstore: 缺少 front-matter 起始围栏")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r\n") == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, errors.New("fsstore: front-matter 未闭合")
	}

	var fm frontMatter
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "")), &fm); err != nil {
		return nil, errors.Wrapf(err, "fsstore: front-matter YAML 非法")
	}
	if fm.Version == 0 {
		return nil, errors.New("fsstore: front-matter version 缺失或为 0")
	}

	p := &wiki.Page{
		Slug:    slug, // 文件名是身份，front-matter 的 slug 只作展示
		Title:   fm.Title,
		Links:   fm.Links,
		Tags:    fm.Tags,
		Sources: fm.Sources,
		Version: fm.Version,
		Body:    trimOneNewline(strings.Join(lines[end+1:], "")),
	}
	if fm.Updated != "" {
		updated, err := time.Parse(time.RFC3339Nano, fm.Updated)
		if err != nil {
			return nil, errors.Wrapf(err, "fsstore: front-matter updated 时间非法")
		}
		p.Updated = updated
	}
	return p, nil
}

// trimOneNewline 剥掉闭合围栏后的分隔换行（writer 固定写一个空行）。
func trimOneNewline(s string) string {
	switch {
	case strings.HasPrefix(s, "\r\n"):
		return s[2:]
	case strings.HasPrefix(s, "\n"):
		return s[1:]
	default:
		return s
	}
}

// writeFileAtomic 同目录临时文件 + fsync + rename。临时文件名以 "." 开头，
// List 会忽略崩溃残留。
func writeFileAtomic(path string, data []byte) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".wiki-*.tmp")
	if err != nil {
		return errors.Wrapf(err, "fsstore: 创建临时文件失败")
	}
	name := f.Name()
	defer func() {
		if rerr := os.Remove(name); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) && err == nil {
			err = errors.Wrapf(rerr, "fsstore: 清理临时文件 %q 失败", name)
		}
	}()

	if _, werr := f.Write(data); werr != nil {
		return errors.Wrapf(closeWith(werr, f.Close()), "fsstore: 写临时文件 %q 失败", name)
	}
	if serr := f.Sync(); serr != nil {
		return errors.Wrapf(closeWith(serr, f.Close()), "fsstore: fsync 临时文件 %q 失败", name)
	}
	if cerr := f.Close(); cerr != nil {
		return errors.Wrapf(cerr, "fsstore: 关闭临时文件 %q 失败", name)
	}
	if cerr := os.Chmod(name, 0o644); cerr != nil {
		return errors.Wrapf(cerr, "fsstore: 设置临时文件权限失败")
	}
	if rerr := os.Rename(name, path); rerr != nil {
		return errors.Wrapf(rerr, "fsstore: 原子替换 %q 失败", path)
	}
	return nil
}

// closeWith 把关闭错误并入主错误消息（不覆盖主错误类型，errors.Is 仍可判定
// 主错误）。
func closeWith(primary, closeErr error) error {
	if closeErr == nil {
		return primary
	}
	return errors.Wrapf(primary, "关闭临时文件也失败: %v", closeErr)
}
