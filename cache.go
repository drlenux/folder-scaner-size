package main

import (
	"path/filepath"
	"sync"
)

// Cache — потокобезопасный кеш узлов-директорий с быстрым доступом по пути.
// Файлы отдельно не хранятся (их могут быть миллионы) — они лежат в Children родителя.
type Cache struct {
	mu    sync.RWMutex
	nodes map[string]*Node
	root  string
}

func NewCache() *Cache {
	return &Cache{nodes: make(map[string]*Node)}
}

func (c *Cache) Reset(root string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nodes = make(map[string]*Node)
	c.root = root
}

func (c *Cache) Root() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.root
}

func (c *Cache) Set(path string, n *Node) {
	c.mu.Lock()
	c.nodes[path] = n
	c.mu.Unlock()
}

func (c *Cache) Get(path string) (*Node, bool) {
	c.mu.RLock()
	n, ok := c.nodes[path]
	c.mu.RUnlock()
	return n, ok
}

// LookupSize возвращает размер и число файлов узла из кеша (директория)
// или из Children родителя (файл) — до физического удаления с диска.
func (c *Cache) LookupSize(path string) (size, files int64, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if n, found := c.nodes[path]; found {
		return n.Size, n.Files, true
	}
	parent := filepath.Dir(path)
	pn, found := c.nodes[parent]
	if !found {
		return 0, 0, false
	}
	for _, ch := range pn.Children {
		if ch.Path == path {
			return ch.Size, ch.Files, true
		}
	}
	return 0, 0, false
}

// Remove удаляет путь из кеша и вычитает размер у всех предков в пределах корня.
// size/files должны быть известны заранее (см. LookupSize) — после RemoveAll
// файл уже недоступен на диске.
func (c *Cache) Remove(path string, size, files int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if n, ok := c.nodes[path]; ok {
		c.removeSubtree(n)
	}

	parent := filepath.Dir(path)
	if pn, ok := c.nodes[parent]; ok {
		filtered := pn.Children[:0]
		for _, ch := range pn.Children {
			if ch.Path != path {
				filtered = append(filtered, ch)
			}
		}
		pn.Children = filtered
		sortChildren(pn)
	}

	for a := parent; ; {
		if pn, ok := c.nodes[a]; ok {
			pn.Size -= size
			pn.Files -= files
			if pn.Size < 0 {
				pn.Size = 0
			}
			if pn.Files < 0 {
				pn.Files = 0
			}
		}
		if a == c.root {
			break
		}
		up := filepath.Dir(a)
		if up == a {
			break
		}
		a = up
	}
}

func (c *Cache) removeSubtree(n *Node) {
	delete(c.nodes, n.Path)
	for _, ch := range n.Children {
		if ch.IsDir {
			c.removeSubtree(ch)
		}
	}
}
