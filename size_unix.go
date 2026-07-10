//go:build unix

package main

import (
	"os"
	"syscall"
)

type fileID struct {
	dev uint64
	ino uint64
}

func idOf(fi os.FileInfo) (fileID, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fileID{}, false
	}
	return fileID{dev: uint64(st.Dev), ino: uint64(st.Ino)}, true
}

// allocatedBytes — место на диске (st_blocks*512), как du, не логический ls-размер.
func allocatedBytes(fi os.FileInfo) int64 {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fi.Size()
	}
	return st.Blocks * 512
}
