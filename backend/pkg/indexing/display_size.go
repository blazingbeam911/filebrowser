package indexing

// fufu: the size FileInfo reports for a file (disk usage unless UseLogicalSize), which is part
// of a thumbnail's cache key; the thumbnail job's quick check builds the key from a plain stat.

import "os"

// DisplaySize returns the size this index reports for a file with the given stat.
func (idx *Index) DisplaySize(fi os.FileInfo) int64 {
	return idx.getFileSizeForDisplay(fi)
}
