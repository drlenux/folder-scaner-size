package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
)

// Scanner конкурентно обходит дерево каталогов.
// Симлинки пропускаются; hard link учитывается один раз (по dev+ino), как du.
type Scanner struct {
	cache   *Cache
	sem     chan struct{}
	seen    sync.Map // fileID → struct{}
	scanned atomic.Int64
	bytes   atomic.Int64
	current atomic.Value // string
}

func NewScanner() *Scanner {
	s := &Scanner{
		cache: NewCache(),
		sem:   make(chan struct{}, runtime.NumCPU()*4),
	}
	s.current.Store("")
	return s
}

type Progress struct {
	Files   int64  `json:"files"`
	Bytes   int64  `json:"bytes"`
	Current string `json:"current"`
}

func (s *Scanner) Progress() Progress {
	cur, _ := s.current.Load().(string)
	return Progress{
		Files:   s.scanned.Load(),
		Bytes:   s.bytes.Load(),
		Current: cur,
	}
}

// claim возвращает true, если inode встречен впервые и его размер нужно учесть.
func (s *Scanner) claim(fi os.FileInfo) bool {
	id, ok := idOf(fi)
	if !ok {
		return true
	}
	_, loaded := s.seen.LoadOrStore(id, struct{}{})
	return !loaded
}

func (s *Scanner) Scan(ctx context.Context, root string) (*Node, error) {
	root = filepath.Clean(root)
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, os.ErrInvalid
	}

	s.scanned.Store(0)
	s.bytes.Store(0)
	s.current.Store(root)
	s.seen = sync.Map{}
	s.cache.Reset(root)

	if !info.IsDir() {
		disk, logical := allocatedBytes(info), info.Size()
		s.claim(info)
		n := &Node{
			Path:    root,
			Name:    filepath.Base(root),
			Size:    disk,
			Logical: logical,
			Files:   1,
			ModTime: info.ModTime(),
		}
		return n, nil
	}

	node := s.walk(ctx, root)
	if ctx.Err() != nil {
		s.cache.Reset("")
		return nil, ctx.Err()
	}
	return node, nil
}

func (s *Scanner) walk(ctx context.Context, path string) *Node {
	if ctx.Err() != nil {
		return nil
	}

	info, err := os.Lstat(path)
	node := &Node{Path: path, Name: filepath.Base(path), IsDir: true}
	if err != nil {
		s.cache.Set(path, node)
		return node
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil // симлинк на каталог не разворачиваем
	}

	node.ModTime = info.ModTime()
	if s.claim(info) {
		disk := allocatedBytes(info)
		node.Size = disk
		node.Logical = info.Size()
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		s.cache.Set(path, node)
		return node
	}

	node.Children = make([]*Node, 0, len(entries))
	subdirs := make([]string, 0, len(entries)/4)

	for _, e := range entries {
		// Type() может быть нулевым — дополнительно проверяем через Lstat ниже.
		if e.Type()&os.ModeSymlink != 0 {
			continue
		}
		full := filepath.Join(path, e.Name())
		fi, ierr := os.Lstat(full)
		if ierr != nil {
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if fi.IsDir() {
			subdirs = append(subdirs, full)
			continue
		}

		disk := allocatedBytes(fi)
		logical := fi.Size()
		first := s.claim(fi)
		child := &Node{
			Path:    full,
			Name:    e.Name(),
			Size:    disk,
			Logical: logical,
			Linked:  !first,
			Files:   1,
			ModTime: fi.ModTime(),
		}
		node.Children = append(node.Children, child)
		node.Files++
		s.scanned.Add(1)
		if first {
			node.Size += disk
			node.Logical += logical
			s.bytes.Add(disk)
		}
	}

	if len(subdirs) == 0 {
		sortChildren(node)
		s.current.Store(path)
		s.cache.Set(path, node)
		return node
	}

	results := make([]*Node, len(subdirs))
	var wg sync.WaitGroup
	for i, full := range subdirs {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		select {
		case s.sem <- struct{}{}:
			go func(i int, full string) {
				defer wg.Done()
				defer func() { <-s.sem }()
				results[i] = s.walk(ctx, full)
			}(i, full)
		default:
			func(i int, full string) {
				defer wg.Done()
				results[i] = s.walk(ctx, full)
			}(i, full)
		}
	}
	wg.Wait()

	for _, c := range results {
		if c == nil {
			continue
		}
		node.Children = append(node.Children, c)
		node.Size += c.Size
		node.Logical += c.Logical
		node.Files += c.Files
	}

	sortChildren(node)
	s.current.Store(path)
	s.cache.Set(path, node)
	return node
}

func sortChildren(n *Node) {
	sort.Slice(n.Children, func(i, j int) bool {
		// Hard link’и (Size всё ещё полный для отображения) — по Size;
		// повторные линки с Linked=true тоже имеют Size, но в сумму не входили.
		// Сортируем: сначала по Linked (уникальные выше), затем по Size.
		if n.Children[i].Linked != n.Children[j].Linked {
			return !n.Children[i].Linked
		}
		return n.Children[i].Size > n.Children[j].Size
	})
}
