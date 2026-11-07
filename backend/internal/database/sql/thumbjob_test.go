package sql

import (
	"strings"
	"testing"
	"time"

	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing/iteminfo"
)

// The thumbnail job's file list: only previewable files under the folder, in path order,
// page by page, and read through the primary key.
func TestPreviewableFilesInFolder(t *testing.T) {
	dir := t.TempDir()
	pop := pushTestIndexConfig(t, dir, testIndexSQLConfig(""))
	defer pop()
	db, _, err := NewIndexDB("thumbjob_queries", "OFF", 1000, 32, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	add := func(path string, isDir, hasPreview bool) {
		t.Helper()
		name := strings.TrimSuffix(path, "/")
		name = name[strings.LastIndex(name, "/")+1:]
		typ := "image/png"
		if isDir {
			typ = "directory"
		}
		info := &iteminfo.FileInfo{Path: path, ItemInfo: iteminfo.ItemInfo{Name: name, Size: 1000, ModTime: time.Unix(1, 0), Type: typ, HasPreview: hasPreview}}
		if err := db.InsertItem("src", path, info); err != nil {
			t.Fatal(err)
		}
	}
	add("/in/", true, true)
	add("/in/a.png", false, true)
	add("/in/b.txt", false, false)
	add("/in/sub/", true, true)
	add("/in/sub/c.png", false, true)
	add("/in/d.png", false, true)
	add("/inner/e.png", false, true) // shares the prefix "/in" but not the folder "/in/"
	add("/out/f.png", false, true)

	if n, err := db.CountPreviewableFiles("src", "/in/"); err != nil || n != 3 {
		t.Fatalf("count in /in/ = %d, %v; want 3", n, err)
	}
	if n, err := db.CountPreviewableFiles("src", "/"); err != nil || n != 5 {
		t.Fatalf("count in / = %d, %v; want 5", n, err)
	}
	if n, err := db.CountPreviewableFiles("other", "/"); err != nil || n != 0 {
		t.Fatalf("count in another source = %d, %v; want 0", n, err)
	}

	var got []string
	after := "/in/"
	for {
		page, err := db.PreviewableFilesAfter("src", "/in/", after, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		if len(page) > 2 {
			t.Fatalf("page of %d, limit 2", len(page))
		}
		got = append(got, page...)
		after = page[len(page)-1]
	}
	if strings.Join(got, ",") != "/in/a.png,/in/d.png,/in/sub/c.png" {
		t.Fatalf("files in /in/ = %v", got)
	}

	rows, err := db.Query(`EXPLAIN QUERY PLAN SELECT path FROM index_items
	WHERE source = ? AND path > ? AND path < ? AND is_dir = 0 AND has_preview = 1
	ORDER BY path LIMIT ?`, "src", "/in/", "/in0", 500)
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
	plan := strings.Join(details, "; ")
	if !strings.Contains(plan, "path>? AND path<?") || strings.Contains(plan, "TEMP B-TREE") {
		t.Errorf("plan = %q, want a range scan of the path key with no sort", plan)
	}
}
