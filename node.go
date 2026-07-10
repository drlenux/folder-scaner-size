package main

import "time"

// Node — узел дерева файловой системы.
// Size — вклад в занятое место (0 у повторных hard link, как в du).
// Logical — логический размер (st_size).
type Node struct {
	Path     string
	Name     string
	Size     int64
	Logical  int64
	IsDir    bool
	Linked   bool // повторный hard link: в списке есть, в сумму родителя не входит
	Files    int64
	ModTime  time.Time
	Children []*Node
}

// NodeDTO — облегчённое представление узла для клиента.
type NodeDTO struct {
	Path        string    `json:"path"`
	Name        string    `json:"name"`
	Size        int64     `json:"size"`
	Logical     int64     `json:"logical"`
	IsDir       bool      `json:"isDir"`
	Linked      bool      `json:"linked"`
	Files       int64     `json:"files"`
	ModTime     time.Time `json:"modTime"`
	HasChildren bool      `json:"hasChildren"`
	Sparse      bool      `json:"sparse"`
}

func toDTO(n *Node) NodeDTO {
	return NodeDTO{
		Path:        n.Path,
		Name:        n.Name,
		Size:        n.Size,
		Logical:     n.Logical,
		IsDir:       n.IsDir,
		Linked:      n.Linked,
		Files:       n.Files,
		ModTime:     n.ModTime,
		HasChildren: n.IsDir && len(n.Children) > 0,
		Sparse:      !n.Linked && n.Logical > n.Size && n.Logical-n.Size >= 4096,
	}
}

func childrenDTO(n *Node) []NodeDTO {
	kids := make([]NodeDTO, len(n.Children))
	for i, ch := range n.Children {
		kids[i] = toDTO(ch)
	}
	return kids
}
