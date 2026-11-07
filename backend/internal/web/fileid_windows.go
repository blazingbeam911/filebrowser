//go:build windows

package web

// statFileID isn't implemented on Windows: every path counts as a separate file.
func statFileID(path string) (id fileID, nlink uint64, ok bool) {
	return fileID{}, 0, false
}
