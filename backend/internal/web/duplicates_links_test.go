//go:build !windows

package web

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing"
	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing/iteminfo"
)

// writeRandom writes 64 KB of random data to dir/name and returns the data.
func writeRandom(t *testing.T, dir, name string) []byte {
	t.Helper()
	data := make([]byte, 64*1024)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return data
}

func link(t *testing.T, dir, from, to string) {
	t.Helper()
	if err := os.Link(filepath.Join(dir, from), filepath.Join(dir, to)); err != nil {
		t.Fatal(err)
	}
}

func copyData(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func infos(names ...string) []*iteminfo.FileInfo {
	out := make([]*iteminfo.FileInfo, 0, len(names))
	for _, name := range names {
		fi := &iteminfo.FileInfo{}
		fi.Name = name
		fi.Path = "/" + name
		out = append(out, fi)
	}
	return out
}

func names(files []*iteminfo.FileInfo) string {
	parts := make([]string, 0, len(files))
	for _, f := range files {
		parts = append(parts, f.Name)
	}
	return strings.Join(parts, ",")
}

func TestStatFileID(t *testing.T) {
	dir := t.TempDir()
	data := writeRandom(t, dir, "a")
	link(t, dir, "a", "a-link")
	copyData(t, dir, "a-copy", data)

	idA, nA, okA := statFileID(filepath.Join(dir, "a"))
	idL, nL, okL := statFileID(filepath.Join(dir, "a-link"))
	idC, nC, okC := statFileID(filepath.Join(dir, "a-copy"))
	if !okA || !okL || !okC {
		t.Fatalf("stat failed: %v %v %v", okA, okL, okC)
	}
	if idA != idL || nA != 2 || nL != 2 {
		t.Fatalf("link: id %v vs %v, nlink %d/%d, want same id and 2", idA, idL, nA, nL)
	}
	if idA == idC || nC != 1 {
		t.Fatalf("copy: id %v vs %v, nlink %d, want another id and 1", idA, idC, nC)
	}
	if _, _, ok := statFileID(filepath.Join(dir, "missing")); ok {
		t.Fatal("missing file: want ok=false")
	}
}

func TestCollapseHardlinks(t *testing.T) {
	dir := t.TempDir()
	a := writeRandom(t, dir, "a")
	link(t, dir, "a", "a-link")
	copyData(t, dir, "a-copy", a)
	writeRandom(t, dir, "b")
	link(t, dir, "b", "b-link")
	d := writeRandom(t, dir, "d01")
	for _, n := range []string{"d02", "d03", "d04", "d05", "d06", "d07", "d08", "d09", "d10", "d11", "d12"} {
		link(t, dir, "d01", n)
	}
	copyData(t, dir, "d13", d)

	t.Run("mixed", func(t *testing.T) {
		memo := linkMemo{}
		files := infos("a", "a-copy", "a-link")
		reps, members := collapseHardlinks(files, dir, memo)
		if got := names(reps); got != "a,a-copy" {
			t.Fatalf("reps = %s, want a,a-copy", got)
		}
		if got := names(members[reps[0]]); got != "a,a-link" {
			t.Fatalf("members of a = %s, want a,a-link", got)
		}
		if got := names(members[reps[1]]); got != "a-copy" {
			t.Fatalf("members of a-copy = %s", got)
		}
		if len(memo) != 3 {
			t.Fatalf("memo has %d entries, want 3", len(memo))
		}
	})

	t.Run("links only", func(t *testing.T) {
		reps, _ := collapseHardlinks(infos("b", "b-link"), dir, linkMemo{})
		if len(reps) != 1 {
			t.Fatalf("reps = %s, want one", names(reps))
		}
	})

	t.Run("missing path dropped", func(t *testing.T) {
		reps, members := collapseHardlinks(infos("a", "gone", "a-copy"), dir, linkMemo{})
		if got := names(reps); got != "a,a-copy" {
			t.Fatalf("reps = %s, want a,a-copy", got)
		}
		if len(members) != 2 {
			t.Fatalf("members has %d entries, want 2", len(members))
		}
	})

	t.Run("12 links and a copy", func(t *testing.T) {
		files := infos("d01", "d02", "d03", "d04", "d05", "d06", "d07", "d08", "d09", "d10", "d11", "d12", "d13")
		reps, members := collapseHardlinks(files, dir, linkMemo{})
		if got := names(reps); got != "d01,d13" {
			t.Fatalf("reps = %s, want d01,d13", got)
		}
		if len(members[reps[0]]) != 12 {
			t.Fatalf("d01 has %d paths, want 12", len(members[reps[0]]))
		}
	})

	t.Run("memo reused", func(t *testing.T) {
		memo := linkMemo{filepath.Join(dir, "a-copy"): {id: fileID{dev: 1, ino: 1}, nlink: 1, ok: true}}
		reps, _ := collapseHardlinks(infos("a", "a-copy"), dir, memo)
		if len(reps) != 2 || memo[filepath.Join(dir, "a-copy")].id != (fileID{dev: 1, ino: 1}) {
			t.Fatal("memo entry was not used as is")
		}
	})
}

func TestFinalizeGroup(t *testing.T) {
	x := linkInfo{id: fileID{dev: 1, ino: 10}, nlink: 2, ok: true}
	y := linkInfo{id: fileID{dev: 1, ino: 20}, nlink: 1, ok: true}
	z := linkInfo{id: fileID{dev: 1, ino: 30}, nlink: 3, ok: true}
	w := linkInfo{id: fileID{dev: 1, ino: 40}, nlink: 5, ok: true} // its other links aren't in the group
	unknown := linkInfo{}

	results := func(paths ...string) []*indexing.SearchResult {
		out := make([]*indexing.SearchResult, 0, len(paths))
		for _, p := range paths {
			out = append(out, &indexing.SearchResult{Path: p})
		}
		return out
	}
	type want struct {
		path      string
		links     uint64
		linkGroup int
	}
	check := func(t *testing.T, out []*indexing.SearchResult, wants []want) {
		t.Helper()
		if len(out) != len(wants) {
			t.Fatalf("got %d results, want %d", len(out), len(wants))
		}
		for i, w := range wants {
			r := out[i]
			if r.Path != w.path || r.Links != w.links || r.LinkGroup != w.linkGroup {
				t.Fatalf("result %d = {%s %d %d}, want {%s %d %d}", i, r.Path, r.Links, r.LinkGroup, w.path, w.links, w.linkGroup)
			}
		}
	}

	t.Run("mixed group, links made adjacent", func(t *testing.T) {
		out, distinct := finalizeGroup(results("/p1", "/p3", "/p2"), []linkInfo{x, y, x})
		if distinct != 2 {
			t.Fatalf("distinct = %d, want 2", distinct)
		}
		check(t, out, []want{{"/p1", 2, 1}, {"/p2", 2, 1}, {"/p3", 0, 0}})
	})

	t.Run("two linked files numbered in order", func(t *testing.T) {
		out, distinct := finalizeGroup(results("/z1", "/x1", "/z2", "/x2", "/z3", "/w"), []linkInfo{z, x, z, x, z, w})
		if distinct != 3 {
			t.Fatalf("distinct = %d, want 3", distinct)
		}
		check(t, out, []want{{"/z1", 3, 1}, {"/z2", 3, 1}, {"/z3", 3, 1}, {"/x1", 2, 2}, {"/x2", 2, 2}, {"/w", 5, 0}})
	})

	t.Run("links only", func(t *testing.T) {
		_, distinct := finalizeGroup(results("/p1", "/p2"), []linkInfo{x, x})
		if distinct != 1 {
			t.Fatalf("distinct = %d, want 1", distinct)
		}
	})

	t.Run("unknown identity counts each path", func(t *testing.T) {
		out, distinct := finalizeGroup(results("/u1", "/u2"), []linkInfo{unknown, unknown})
		if distinct != 2 {
			t.Fatalf("distinct = %d, want 2", distinct)
		}
		check(t, out, []want{{"/u1", 0, 0}, {"/u2", 0, 0}})
	})
}

func TestLinkGaps(t *testing.T) {
	x := linkInfo{id: fileID{dev: 1, ino: 10}, nlink: 2, ok: true} // both links in the group
	w := linkInfo{id: fileID{dev: 1, ino: 40}, nlink: 5, ok: true} // 2 of 5 links in the group
	y := linkInfo{id: fileID{dev: 1, ino: 20}, nlink: 1, ok: true}
	unknown := linkInfo{}
	files := []*indexing.SearchResult{{Path: "/x1", Size: 7}, {Path: "/w1", Size: 7}, {Path: "/y", Size: 7}, {Path: "/x2", Size: 7}, {Path: "/w2", Size: 7}, {Path: "/u", Size: 7}}
	gaps := linkGaps(files, []linkInfo{x, w, y, x, w, unknown})
	if len(gaps) != 1 || gaps[0].first.Path != "/w1" || gaps[0].id != w.id || gaps[0].missing != 3 || gaps[0].size != 7 {
		t.Fatalf("gaps = %+v, want one for /w1 with 3 missing", gaps)
	}
}

func TestAttachOtherLinks(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scope", "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	gapFor := func(t *testing.T, name string, inGroup int) linkGap {
		t.Helper()
		id, nlink, ok := statFileID(filepath.Join(root, name))
		if !ok {
			t.Fatal("statFileID failed")
		}
		return linkGap{first: &indexing.SearchResult{Path: "/" + name}, id: id, size: 1, missing: int(nlink) - inGroup}
	}

	t.Run("other links listed, in-group path and other files skipped", func(t *testing.T) {
		data := writeRandom(t, root, "scope/a.bin")
		link(t, root, "scope/a.bin", "scope/out/a_out1.bin")
		link(t, root, "scope/a.bin", "scope/out/a_out2.bin")
		copyData(t, root, "scope/out/a_copy.bin", data)
		gap := gapFor(t, "scope/a.bin", 1)
		candidates := map[int64][]*iteminfo.FileInfo{1: infos("scope/a.bin", "scope/out/a_copy.bin", "scope/out/a_out1.bin", "scope/out/a_out2.bin")}
		attachOtherLinks([]linkGap{gap}, candidates, map[string]bool{"/scope/a.bin": true}, root, "/scope/", linkMemo{})
		got := strings.Join(gap.first.OtherLinks, ",")
		if got != "/out/a_out1.bin,/out/a_out2.bin" || gap.first.OtherLinksMore != 0 || gap.first.OtherLinksNotFound != 0 {
			t.Fatalf("got %q more %d notFound %d", got, gap.first.OtherLinksMore, gap.first.OtherLinksNotFound)
		}
	})

	t.Run("links not among the candidates are not found", func(t *testing.T) {
		writeRandom(t, root, "scope/b.bin")
		link(t, root, "scope/b.bin", "scope/out/b_out1.bin")
		link(t, root, "scope/b.bin", "scope/out/b_out2.bin")
		gap := gapFor(t, "scope/b.bin", 1)
		candidates := map[int64][]*iteminfo.FileInfo{1: infos("scope/out/b_out2.bin")}
		attachOtherLinks([]linkGap{gap}, candidates, nil, root, "/scope/", linkMemo{})
		got := strings.Join(gap.first.OtherLinks, ",")
		if got != "/out/b_out2.bin" || gap.first.OtherLinksNotFound != 1 {
			t.Fatalf("got %q notFound %d, want /out/b_out2.bin and 1", got, gap.first.OtherLinksNotFound)
		}
	})

	t.Run("more than the listed limit are counted", func(t *testing.T) {
		writeRandom(t, root, "scope/c.bin")
		var names []string
		for i := 0; i < otherLinksListed+3; i++ {
			name := "scope/out/c_" + string(rune('a'+i)) + ".bin"
			link(t, root, "scope/c.bin", name)
			names = append(names, name)
		}
		gap := gapFor(t, "scope/c.bin", 1)
		attachOtherLinks([]linkGap{gap}, map[int64][]*iteminfo.FileInfo{1: infos(names...)}, nil, root, "/scope/", linkMemo{})
		if len(gap.first.OtherLinks) != otherLinksListed || gap.first.OtherLinksMore != 3 || gap.first.OtherLinksNotFound != 0 {
			t.Fatalf("listed %d more %d notFound %d, want %d, 3, 0", len(gap.first.OtherLinks), gap.first.OtherLinksMore, gap.first.OtherLinksNotFound, otherLinksListed)
		}
	})
}

func TestFreeableCopies(t *testing.T) {
	for _, c := range []struct{ distinct, external, want int }{
		{2, 0, 1}, // two copies: one can go
		{3, 0, 2},
		{3, 1, 2}, // the external file is the one kept
		{3, 2, 1}, // only the one file without outside links frees space
		{2, 2, 0}, // nothing to free: hidden
		{3, 3, 0},
	} {
		if got := freeableCopies(c.distinct, c.external); got != c.want {
			t.Errorf("freeableCopies(%d, %d) = %d, want %d", c.distinct, c.external, got, c.want)
		}
	}
}

func TestLinksOnlyHidden(t *testing.T) {
	a := fileID{dev: 1, ino: 1}
	b := fileID{dev: 1, ino: 2}
	c := fileID{dev: 1, ino: 3}
	groups := []duplicateGroupWithChecksums{{Links: []linkInfo{{id: b, ok: true}, {id: c, ok: true}}}}
	if got := linksOnlyHidden(map[fileID]bool{a: true, b: true}, groups); got != 1 {
		t.Fatalf("linksOnlyHidden = %d, want 1 (b is in a reported group)", got)
	}
	if got := linksOnlyHidden(nil, groups); got != 0 {
		t.Fatalf("linksOnlyHidden(nil) = %d, want 0", got)
	}
}

func TestCheckStopLogsOnce(t *testing.T) {
	s := &duplicateProcessingStats{startTime: time.Now().Add(-2 * maxProcessingTime)}
	if !s.checkStop() || !s.stopped || s.stopReason == "" {
		t.Fatal("past the time limit, checkStop must stop and record the reason")
	}
	reason := s.stopReason
	s.startTime = time.Now()
	if !s.checkStop() || s.stopReason != reason {
		t.Fatal("once stopped, checkStop stays stopped and keeps the first reason")
	}
	if (&duplicateProcessingStats{startTime: time.Now()}).checkStop() {
		t.Fatal("within limits, checkStop must not stop")
	}
}
