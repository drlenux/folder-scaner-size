package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1 << 16,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// ---- Протокол --------------------------------------------------------------

type inbound struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

type outMsg struct {
	Type     string    `json:"type"`
	Home     string    `json:"home,omitempty"`
	Sep      string    `json:"sep,omitempty"`
	Roots    []string  `json:"roots,omitempty"`
	Message  string    `json:"message,omitempty"`
	Progress *Progress `json:"progress,omitempty"`
	Done     bool      `json:"done,omitempty"`
	Root     *NodeDTO  `json:"root,omitempty"`
	Parent   *NodeDTO  `json:"parent,omitempty"`
	Children []NodeDTO `json:"children,omitempty"`
	Path     string    `json:"path,omitempty"`
	Freed    int64     `json:"freed,omitempty"`
	Files    int64     `json:"files,omitempty"`
}

// ---- Клиент ----------------------------------------------------------------

type Client struct {
	conn     *websocket.Conn
	scanner  *Scanner
	send     chan outMsg
	mu       sync.Mutex
	scanning bool
	cancel   context.CancelFunc
}

func ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade: %v", err)
		return
	}
	c := &Client{
		conn:    conn,
		scanner: NewScanner(),
		send:    make(chan outMsg, 64),
	}
	go c.writeLoop()
	c.sendInit()
	c.readLoop()
}

func (c *Client) writeLoop() {
	for msg := range c.send {
		if err := c.conn.WriteJSON(msg); err != nil {
			return
		}
	}
}

// enqueue дропает только progress при переполнении; критичные кадры ждут.
func (c *Client) enqueue(msg outMsg) {
	if msg.Type == "progress" && !msg.Done {
		select {
		case c.send <- msg:
		default:
		}
		return
	}
	select {
	case c.send <- msg:
	case <-time.After(5 * time.Second):
		log.Printf("ws send timeout: %s", msg.Type)
	}
}

func (c *Client) readLoop() {
	defer func() {
		c.mu.Lock()
		if c.cancel != nil {
			c.cancel()
		}
		c.mu.Unlock()
		close(c.send)
		c.conn.Close()
	}()

	for {
		var msg inbound
		if err := c.conn.ReadJSON(&msg); err != nil {
			return
		}
		switch msg.Type {
		case "scan":
			c.handleScan(msg.Path)
		case "cancel":
			c.handleCancel()
		case "open":
			c.handleOpen(msg.Path)
		case "delete":
			c.handleDelete(msg.Path)
		case "reveal":
			c.handleReveal(msg.Path)
		}
	}
}

func (c *Client) sendInit() {
	home, _ := os.UserHomeDir()
	roots := []string{home}
	for _, p := range []string{
		filepath.Join(home, "Desktop"),
		filepath.Join(home, "Downloads"),
		filepath.Join(home, "Documents"),
		"/",
	} {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			roots = append(roots, p)
		}
	}
	c.enqueue(outMsg{Type: "init", Home: home, Sep: string(os.PathSeparator), Roots: roots})
}

func (c *Client) handleScan(path string) {
	path = filepath.Clean(path)
	if path == "" || path == "." {
		c.enqueue(outMsg{Type: "error", Message: "Укажите путь для сканирования"})
		return
	}

	c.mu.Lock()
	if c.scanning {
		c.mu.Unlock()
		c.enqueue(outMsg{Type: "error", Message: "Сканирование уже выполняется"})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.scanning = true
	c.mu.Unlock()

	go func() {
		defer func() {
			c.mu.Lock()
			c.scanning = false
			c.cancel = nil
			c.mu.Unlock()
			cancel()
		}()

		type result struct {
			root *Node
			err  error
		}
		done := make(chan result, 1)
		go func() {
			root, err := c.scanner.Scan(ctx, path)
			done <- result{root, err}
		}()

		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case res := <-done:
				if res.err != nil {
					if ctx.Err() != nil {
						c.enqueue(outMsg{Type: "cancelled"})
						return
					}
					c.enqueue(outMsg{Type: "error", Message: "Не удалось прочитать путь: " + res.err.Error()})
					return
				}
				p := c.scanner.Progress()
				rootDTO := toDTO(res.root)
				c.enqueue(outMsg{Type: "progress", Progress: &p, Done: true})
				c.enqueue(outMsg{
					Type:     "scanned",
					Root:     &rootDTO,
					Children: childrenDTO(res.root),
				})
				return
			case <-ticker.C:
				p := c.scanner.Progress()
				c.enqueue(outMsg{Type: "progress", Progress: &p})
			}
		}
	}()
}

func (c *Client) handleCancel() {
	c.mu.Lock()
	cancel := c.cancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (c *Client) handleOpen(path string) {
	path = filepath.Clean(path)
	n, ok := c.scanner.cache.Get(path)
	if !ok {
		c.enqueue(outMsg{Type: "error", Message: "Каталог не найден в кеше: " + path})
		return
	}
	parent := toDTO(n)
	c.enqueue(outMsg{Type: "children", Parent: &parent, Children: childrenDTO(n)})
}

func (c *Client) handleDelete(path string) {
	path = filepath.Clean(path)
	if path == "" {
		return
	}
	if path == c.scanner.cache.Root() {
		c.enqueue(outMsg{Type: "error", Message: "Нельзя удалить корень сканирования"})
		return
	}

	size, files, ok := c.scanner.cache.LookupSize(path)
	if !ok {
		c.enqueue(outMsg{Type: "error", Message: "Путь не найден в кеше: " + path})
		return
	}

	if err := os.RemoveAll(path); err != nil {
		c.enqueue(outMsg{Type: "error", Message: "Не удалось удалить: " + err.Error()})
		return
	}

	c.scanner.cache.Remove(path, size, files)
	parentPath := filepath.Dir(path)
	resp := outMsg{Type: "deleted", Path: path, Freed: size, Files: files}
	if pn, ok := c.scanner.cache.Get(parentPath); ok {
		parent := toDTO(pn)
		resp.Parent = &parent
		resp.Children = childrenDTO(pn)
	}
	c.enqueue(resp)
}

func (c *Client) handleReveal(path string) {
	path = filepath.Clean(path)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	case "windows":
		cmd = exec.Command("explorer", "/select,", path)
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(path))
	}
	if err := cmd.Start(); err != nil {
		c.enqueue(outMsg{Type: "error", Message: "Не удалось открыть в проводнике: " + err.Error()})
	}
}
