package sql

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing/iteminfo"
)

// The duplicate finder's queries limited to a folder: results as before, and each query uses
// the index that keeps it small (size index for files of given sizes, path key for size groups).
func TestDuplicateQueriesInFolder(t *testing.T) {
	dir := t.TempDir()
	pop := pushTestIndexConfig(t, dir, testIndexSQLConfig(""))
	defer pop()
	db, _, err := NewIndexDB("dup_queries", "OFF", 1000, 32, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	add := func(path string, size int64) {
		t.Helper()
		name := path[strings.LastIndex(path, "/")+1:]
		info := &iteminfo.FileInfo{Path: path, ItemInfo: iteminfo.ItemInfo{Name: name, Size: size, ModTime: time.Unix(1, 0), Type: "video/mp4"}}
		if err := db.InsertItem("src", path, info); err != nil {
			t.Fatal(err)
		}
	}
	// In /in/: sizes 100 (x2), 200 (x2), 300 (x1). Outside: more of size 100 and 300.
	add("/in/a.mp4", 100)
	add("/in/b.mp4", 100)
	add("/in/sub/c.mp4", 200)
	add("/in/d.mp4", 200)
	add("/in/e.mp4", 300)
	add("/out/f.mp4", 100)
	add("/out/g.mp4", 300)
	add("/inner/h.mp4", 100) // shares the prefix "/in" but not the folder "/in/"

	sizes, counts, err := db.GetSizeGroupsForDuplicates("src", 50, "/in/")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(sizes) != "[200 100]" || counts[100] != 2 || counts[200] != 2 {
		t.Fatalf("size groups in /in/ = %v %v, want [200 100] with 2 each", sizes, counts)
	}
	files, err := db.GetFilesForMultipleSizes("src", []int64{100, 200, 300}, "/in/")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, size := range []int64{100, 200, 300} {
		for _, f := range files[size] {
			got = append(got, f.Path)
		}
	}
	if strings.Join(got, ",") != "/in/a.mp4,/in/b.mp4,/in/sub/c.mp4,/in/d.mp4,/in/e.mp4" { // by size, type, name
		t.Fatalf("files in /in/ = %v", got)
	}

	plan := func(query string, args ...any) string {
		t.Helper()
		rows, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var details []string
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatal(err)
			}
			details = append(details, detail)
		}
		return strings.Join(details, "; ")
	}
	if p := plan(filesForSizesQuery(2, true), "src", 100, 200, "/in/", "/in0"); !strings.Contains(p, "idx_source_size") {
		t.Errorf("files-for-sizes plan = %q, want the size index", p)
	}
	if p := plan(sizeGroupsQuery(true), "src", 50, "/in/", "/in0"); strings.Contains(p, "idx_source_size") || !strings.Contains(p, "path>?") {
		t.Errorf("size-groups plan = %q, want the path key", p)
	}
}
