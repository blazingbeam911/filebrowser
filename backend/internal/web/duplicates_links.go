package web

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing"
	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing/iteminfo"
)

// Hard links to one file are a single file on disk, not duplicates of each other. The
// duplicate finder reads each candidate's identity (device and inode) while it searches,
// checksums one path per file, and counts distinct files, not paths.

// fileID identifies a file on disk: paths with the same fileID are hard links to it.
type fileID struct {
	dev uint64
	ino uint64
}

// linkInfo is what one path's stat said about it.
type linkInfo struct {
	id    fileID
	nlink uint64
	ok    bool // false: identity unknown, the path counts as a file of its own
}

// linkMemo caches linkInfo by full filesystem path for one duplicate search, so no path is
// stat'ed twice.
type linkMemo map[string]linkInfo

// lookup returns the linkInfo for path, reading it on first use. exists is false when the
// path can't be stat'ed at all.
func (m linkMemo) lookup(path string) (info linkInfo, exists bool) {
	if info, found := m[path]; found {
		return info, true
	}
	if id, nlink, ok := statFileID(path); ok {
		info = linkInfo{id: id, nlink: nlink, ok: true}
	} else if _, err := os.Stat(path); err != nil {
		return linkInfo{}, false
	}
	m[path] = info
	return info, true
}

// collapseHardlinks picks one representative per distinct file in files (the first path in
// the group's order) and returns, for each representative, every path of that file with the
// representative first. Paths that can't be stat'ed are dropped. A path whose identity is
// unknown is its own representative.
func collapseHardlinks(files []*iteminfo.FileInfo, root string, memo linkMemo) (reps []*iteminfo.FileInfo, members map[*iteminfo.FileInfo][]*iteminfo.FileInfo) {
	members = make(map[*iteminfo.FileInfo][]*iteminfo.FileInfo, len(files))
	repByID := make(map[fileID]*iteminfo.FileInfo, len(files))
	for _, file := range files {
		info, exists := memo.lookup(filepath.Join(root, file.Path))
		if !exists {
			continue
		}
		if info.ok {
			if rep, seen := repByID[info.id]; seen {
				members[rep] = append(members[rep], file)
				continue
			}
			repByID[info.id] = file
		}
		reps = append(reps, file)
		members[file] = []*iteminfo.FileInfo{file}
	}
	return reps, members
}

// finalizeGroup puts the paths of one file next to each other (files keep the order in which
// they first appear), sets Links to the file's hard link count when it has more than one, and
// numbers each file that has two or more paths in this group (LinkGroup 1, 2, ...). links
// holds each path's linkInfo, in the same order as files. distinct is the number of distinct
// files.
func finalizeGroup(files []*indexing.SearchResult, links []linkInfo) (out []*indexing.SearchResult, distinct int) {
	type fileEntry struct {
		paths []*indexing.SearchResult
		info  linkInfo
	}
	var entries []*fileEntry
	byID := make(map[fileID]*fileEntry, len(files))
	for i, result := range files {
		var info linkInfo
		if i < len(links) {
			info = links[i]
		}
		if info.ok {
			if entry, seen := byID[info.id]; seen {
				entry.paths = append(entry.paths, result)
				continue
			}
		}
		entry := &fileEntry{paths: []*indexing.SearchResult{result}, info: info}
		entries = append(entries, entry)
		if info.ok {
			byID[info.id] = entry
		}
	}

	out = make([]*indexing.SearchResult, 0, len(files))
	linkGroup := 0
	for _, entry := range entries {
		group := 0
		if len(entry.paths) > 1 {
			linkGroup++
			group = linkGroup
		}
		for _, result := range entry.paths {
			result.Links = 0
			if entry.info.ok && entry.info.nlink > 1 {
				result.Links = entry.info.nlink
			}
			result.LinkGroup = group
			out = append(out, result)
		}
	}
	return out, len(entries)
}

// otherLinksListed caps how many other paths of one file a result lists; the rest are counted.
const otherLinksListed = 5

// otherLinksMaxStats caps the stats spent looking for other links in one search. Links not
// found by then are reported as not found.
const otherLinksMaxStats = 10000

// linkGap is a file in a duplicate group that has hard links outside the group.
type linkGap struct {
	first   *indexing.SearchResult // the file's first path in the group, which lists the others
	id      fileID
	size    int64
	missing int // links not in the group
}

// linkGaps returns the files in a group (files and links in the same order, as for
// finalizeGroup) whose link count is higher than the number of their paths in the group.
func linkGaps(files []*indexing.SearchResult, links []linkInfo) []linkGap {
	inGroup := make(map[fileID]int, len(files))
	for i := range files {
		if i < len(links) && links[i].ok {
			inGroup[links[i].id]++
		}
	}
	var gaps []linkGap
	seen := make(map[fileID]bool, len(inGroup))
	for i, result := range files {
		if i >= len(links) || !links[i].ok || seen[links[i].id] {
			continue
		}
		seen[links[i].id] = true
		if count := uint64(inGroup[links[i].id]); links[i].nlink > count {
			gaps = append(gaps, linkGap{first: result, id: links[i].id, size: result.Size, missing: int(links[i].nlink - count)})
		}
	}
	return gaps
}

// attachOtherLinks finds, for each gap, the file's other paths among candidates (indexed files
// by size, already limited to what the user may see) and records them on gap.first: up to
// otherLinksListed paths relative to userPrefix, how many more were found, and how many weren't
// found (links outside the source, in folders that aren't indexed, or outside the user's scope).
// Paths in inResults are already listed in a group and are skipped.
func attachOtherLinks(gaps []linkGap, candidates map[int64][]*iteminfo.FileInfo, inResults map[string]bool, root, userPrefix string, memo linkMemo) {
	stats := 0
	for _, gap := range gaps {
		found := 0
		var listed []string
		for _, file := range candidates[gap.size] {
			if found == gap.missing || stats >= otherLinksMaxStats {
				break
			}
			if inResults[file.Path] {
				continue
			}
			full := filepath.Join(root, file.Path)
			if _, cached := memo[full]; !cached {
				stats++
			}
			info, exists := memo.lookup(full)
			if !exists || !info.ok || info.id != gap.id {
				continue
			}
			found++
			if len(listed) < otherLinksListed {
				listed = append(listed, "/"+strings.TrimPrefix(file.Path, userPrefix))
			}
		}
		gap.first.OtherLinks = listed
		gap.first.OtherLinksMore = found - len(listed)
		gap.first.OtherLinksNotFound = gap.missing - found
	}
}

// freeableCopies returns how many of a group's distinct files can be freed by deleting listed
// paths: a file with hard links outside the group keeps its space when its listed paths are
// deleted, and one copy is always kept.
func freeableCopies(distinct, external int) int {
	contained := distinct - external
	if contained > distinct-1 {
		contained = distinct - 1
	}
	if contained < 0 {
		return 0
	}
	return contained
}
