//go:build !unix

package main

import "os"

type fileID struct {
	dev uint64
	ino uint64
}

func idOf(fi os.FileInfo) (fileID, bool) {
	return fileID{}, false
}

func allocatedBytes(fi os.FileInfo) int64 {
	return fi.Size()
}
