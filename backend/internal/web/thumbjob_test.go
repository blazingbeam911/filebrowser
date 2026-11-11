package web

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gtsteffaniak/filebrowser/backend/internal/adapters/fs/fileutils"
	"github.com/gtsteffaniak/filebrowser/backend/internal/app"
	dbsql "github.com/gtsteffaniak/filebrowser/backend/internal/database/sql"
	"github.com/gtsteffaniak/filebrowser/backend/internal/database/users"
	"github.com/gtsteffaniak/filebrowser/backend/internal/preview"
	"github.com/gtsteffaniak/filebrowser/backend/internal/state"
	"github.com/gtsteffaniak/filebrowser/backend/internal/thumbjob"
	"github.com/gtsteffaniak/filebrowser/backend/internal/utils"
	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing"
	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing/iteminfo"
	"github.com/gtsteffaniak/filebrowser/backend/pkg/settings"
	"golang.org/x/crypto/bcrypt"
)

var thumbJobPreviewCache struct {
	once sync.Once
	dir  string
}

// startThumbJobPreviews starts the (process-wide) preview service once for these tests.
func startThumbJobPreviews(t *testing.T) string {
	t.Helper()
	fileutils.SetFsPermissions(0o644, 0o755) // unset in this test binary otherwise
	thumbJobPreviewCache.once.Do(func() {
		dir, err := os.MkdirTemp("", "thumbjob-cache-")
		if err != nil {
			t.Fatal(err)
		}
		thumbJobPreviewCache.dir = dir
		if preview.GetService() == nil {
			_ = preview.StartPreviewGenerator(2, dir)
		}
	})
	return thumbJobPreviewCache.dir
}

func noisyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	seed := uint32(7)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			seed = seed*1664525 + 1013904223
			img.SetNRGBA(x, y, color.NRGBA{uint8(seed >> 24), uint8(x), uint8(y), 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// setupThumbJobSource makes a source with an admin and a regular user who can both see it.
func setupThumbJobSource(t *testing.T) (root, name string, admin, regular *users.User) {
	t.Helper()
	utils.BcryptCost = bcrypt.MinCost
	if _, err := state.Initialize(filepath.Join(t.TempDir(), "thumbjob.sqlite")); err != nil {
		t.Fatal(err)
	}
	app.MustWireServices(state.Default())
	t.Cleanup(func() { state.Close() })

	root = t.TempDir()
	name = "thumbs"
	src := &settings.Source{Path: root, Name: name}
	settings.Config.Server.SourceMap = map[string]*settings.Source{root: src}
	settings.Config.Server.NameToSource = map[string]*settings.Source{name: src}
	settings.InitializeUserResolvers()
	indexing.SetTestIndex(name, root)
	t.Cleanup(func() { indexing.ClearTestIndices() })

	prevCache := settings.Config.Server.CacheDir
	settings.Config.Server.CacheDir = t.TempDir()
	t.Cleanup(func() { settings.Config.Server.CacheDir = prevCache })
	db, _, err := dbsql.NewIndexDB("thumbjob_web", "OFF", 1000, 32, false)
	if err != nil {
		t.Fatal(err)
	}
	prevDB := indexing.GetIndexDB()
	indexing.SetIndexDBForTesting(db)
	t.Cleanup(func() {
		indexing.SetIndexDBForTesting(prevDB)
		db.Close()
	})

	mk := func(id uint64, username string, isAdmin bool) *users.User {
		u := &users.User{
			ID:           id,
			FrontendUser: users.FrontendUser{Username: username},
			BackendScopes: []users.BackendScope{{
				Path:        root,
				Scope:       "/",
				Permissions: users.SourceFilePermissions{View: true, Download: true},
			}},
		}
		if err := state.CreateUser(u, ""); err != nil {
			t.Fatal(err)
		}
		u.Permissions = users.Permissions{Admin: isAdmin}
		if err := state.UpdateUser(u, "", "permissions"); err != nil {
			t.Fatal(err)
		}
		users.SyncBackendSourcePermissionsMap(u)
		return u
	}
	return root, name, mk(1, "admin", true), mk(2, "regular", false)
}

func countCacheFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	_ = filepath.Walk(filepath.Join(dir, "diskcache"), func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n++
		}
		return nil
	})
	return n
}

// A thumbnail the job makes is what a later preview request is served, from the cache, with
// nothing made again; files browsing sends as they are, and files that are gone, need nothing.
func TestThumbJobThumbnailsAreCacheHitsForPreviews(t *testing.T) {
	cacheDir := startThumbJobPreviews(t)
	root, name, admin, _ := setupThumbJobSource(t)
	if err := os.MkdirAll(filepath.Join(root, "pics"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pics", "big.png"), noisyPNG(t, 1600, 1000), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pics", "tiny.png"), noisyPNG(t, 100, 80), 0o644); err != nil {
		t.Fatal(err)
	}

	backend := thumbJobBackend{}
	spec := thumbjob.Spec{Source: name, Path: "/pics", User: "admin", Prefix: "/pics/", Data: thumbJobData{user: admin, scope: ""}}
	ctx := context.Background()
	if out, err := backend.Process(ctx, spec, "/pics/big.png"); err != nil || out != thumbjob.Generated {
		t.Fatalf("big.png: %v, %v; want generated", out, err)
	}
	if out, err := backend.Process(ctx, spec, "/pics/big.png"); err != nil || out != thumbjob.Cached {
		t.Fatalf("big.png again: %v, %v; want cached", out, err)
	}
	if out, err := backend.Process(ctx, spec, "/pics/tiny.png"); err != nil || out != thumbjob.Cached {
		t.Fatalf("tiny.png (sent as it is): %v, %v; want cached", out, err)
	}
	if out, err := backend.Process(ctx, spec, "/pics/gone.png"); err != nil || out != thumbjob.Skipped {
		t.Fatalf("gone.png: %v, %v; want skipped", out, err)
	}

	before := countCacheFiles(t, cacheDir)
	for _, size := range []string{"small", "large"} {
		req := httptest.NewRequest(http.MethodGet, "/api/resources/preview?source="+name+"&path=/pics/big.png&size="+size, nil)
		rec := httptest.NewRecorder()
		status, err := previewHandler(rec, req, &Context{User: admin})
		if err != nil || (status != 0 && status != http.StatusOK) {
			t.Fatalf("preview %s: %d %v", size, status, err)
		}
		body, _ := io.ReadAll(rec.Body)
		if ct := rec.Header().Get("Content-Type"); ct != "image/webp" || len(body) < 100 {
			t.Fatalf("preview %s: %s, %d bytes", size, ct, len(body))
		}
	}
	if after := countCacheFiles(t, cacheDir); after != before {
		t.Fatalf("preview requests after the job added %d cache files", after-before)
	}
}

// The API: non-admins are refused on every route; an admin can count, queue (a job per
// folder), list and cancel; a file or a missing folder is refused before anything is queued.
func TestThumbJobAPI(t *testing.T) {
	startThumbJobPreviews(t)
	root, name, admin, regular := setupThumbJobSource(t)
	for _, dir := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "a", "x.png"), noisyPNG(t, 10, 10), 0o644); err != nil {
		t.Fatal(err)
	}
	db := indexing.GetIndexDB()
	for _, row := range []struct {
		path    string
		dir     bool
		preview bool
	}{{"/a/", true, false}, {"/b/", true, false}, {"/a/x.png", false, true}, {"/a/notes.txt", false, false}} {
		info := &iteminfo.FileInfo{Path: row.path, IsDir: row.dir, ItemInfo: iteminfo.ItemInfo{Name: filepath.Base(row.path), ModTime: time.Unix(1, 0), Type: "image/png", HasPreview: row.preview}}
		if row.dir {
			info.Type = "directory"
		}
		if err := db.InsertItem(name, row.path, info); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	registerThumbJobRoutes(mux)
	call := func(u *users.User, method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		var rd io.Reader = http.NoBody
		if body != "" {
			rd = bytes.NewBufferString(body)
		}
		req := httptest.NewRequest(method, target, rd)
		req.AddCookie(&http.Cookie{Name: "filebrowser_quantum_jwt", Value: testSessionToken(t, u, time.Hour)})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	for _, r := range []struct{ method, target, body string }{
		{"GET", "/thumbnails/jobs", ""},
		{"GET", "/thumbnails/jobs/count?source=" + name + "&path=/a", ""},
		{"POST", "/thumbnails/jobs", `{"source":"` + name + `","paths":["/a"]}`},
		{"DELETE", "/thumbnails/jobs/1", ""},
	} {
		if rec := call(regular, r.method, r.target, r.body); rec.Code != http.StatusForbidden {
			t.Errorf("non-admin %s %s: %d, want 403", r.method, r.target, rec.Code)
		}
	}

	if rec := call(admin, "GET", "/thumbnails/jobs/count?source="+name+"&path=/a", ""); rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"count":1,`)) {
		t.Fatalf("count: %d %s", rec.Code, rec.Body)
	}
	if rec := call(admin, "POST", "/thumbnails/jobs", `{"source":"`+name+`","paths":["/a","/a/x.png"]}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("queue with a file: %d %s, want 400", rec.Code, rec.Body)
	}
	if rec := call(admin, "POST", "/thumbnails/jobs", `{"source":"`+name+`","paths":["/a","/nope"]}`); rec.Code != http.StatusNotFound {
		t.Fatalf("queue with a missing folder: %d %s, want 404", rec.Code, rec.Body)
	}
	if n := len(thumbJobs().List()); n != 0 {
		t.Fatalf("%d jobs queued by refused requests", n)
	}
	rec := call(admin, "POST", "/thumbnails/jobs", `{"source":"`+name+`","paths":["/a","/b"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("queue: %d %s", rec.Code, rec.Body)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("Prefix")) || bytes.Contains(rec.Body.Bytes(), []byte("Data")) {
		t.Fatalf("internal fields in the response: %s", rec.Body)
	}
	list := thumbJobs().List()
	if len(list) != 2 || list[0].Path == list[1].Path {
		t.Fatalf("jobs %+v, want one per folder", list)
	}
	if rec := call(admin, "GET", "/thumbnails/jobs", ""); rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"jobs"`)) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if rec := call(admin, "DELETE", "/thumbnails/jobs/"+list[1].ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", rec.Code, rec.Body)
	}
	if rec := call(admin, "DELETE", "/thumbnails/jobs/999", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("cancel unknown: %d, want 404", rec.Code)
	}
}

// The quick check agrees with the full path: it calls a file done exactly when Process (and so
// browsing) finds both sizes cached, with the size the index reports (disk usage or logical);
// it leaves everything it can't be sure of to Process.
func TestThumbJobQuickCheck(t *testing.T) {
	startThumbJobPreviews(t)
	root, name, admin, _ := setupThumbJobSource(t)
	pics := filepath.Join(root, "pics")
	if err := os.MkdirAll(pics, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(n string, data []byte) {
		if err := os.WriteFile(filepath.Join(pics, n), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("big.png", noisyPNG(t, 1600, 1000))
	write("logical.png", noisyPNG(t, 1500, 900))
	write("half.png", noisyPNG(t, 1400, 1000))
	write("tiny.png", noisyPNG(t, 100, 80))
	if err := os.Symlink(filepath.Join(pics, "big.png"), filepath.Join(pics, "link.png")); err != nil {
		t.Fatal(err)
	}

	backend := thumbJobBackend{}
	spec := thumbjob.Spec{Source: name, Path: "/pics", User: "admin", Prefix: "/pics/", Data: thumbJobData{user: admin, scope: ""}}
	ctx := context.Background()
	check := func(p string) (thumbjob.Outcome, bool) { return backend.Check(ctx, spec, p) }

	if out, ok := check("/pics/tiny.png"); !ok || out != thumbjob.Cached {
		t.Fatalf("tiny.png (sent as it is): %v %v, want cached", out, ok)
	}
	if _, ok := check("/pics/big.png"); ok {
		t.Fatal("big.png decided before anything was made")
	}
	if out, err := backend.Process(ctx, spec, "/pics/big.png"); err != nil || out != thumbjob.Generated {
		t.Fatalf("big.png: %v %v", out, err)
	}
	if out, ok := check("/pics/big.png"); !ok || out != thumbjob.Cached {
		t.Fatalf("big.png after the job made it: %v %v, want cached", out, ok)
	}
	if _, ok := check("/pics/link.png"); ok {
		t.Fatal("a symlink was decided by the quick check")
	}
	if _, ok := check("/pics/gone.png"); ok {
		t.Fatal("a missing file was decided by the quick check")
	}

	// Logical size: the key changes with it, and the quick check follows.
	idx := indexing.GetIndex(name)
	prev := idx.Config.UseLogicalSize
	idx.Config.UseLogicalSize = !prev
	t.Cleanup(func() { idx.Config.UseLogicalSize = prev })
	if out, err := backend.Process(ctx, spec, "/pics/logical.png"); err != nil || out != thumbjob.Generated {
		t.Fatalf("logical.png: %v %v", out, err)
	}
	if out, ok := check("/pics/logical.png"); !ok || out != thumbjob.Cached {
		t.Fatalf("logical.png with UseLogicalSize=%v: %v %v, want cached", !prev, out, ok)
	}
	idx.Config.UseLogicalSize = prev

	// Only one size made (browsing the list view): not done.
	req := httptest.NewRequest(http.MethodGet, "/api/resources/preview?source="+name+"&path=/pics/half.png&size=small", nil)
	if status, err := previewHandler(httptest.NewRecorder(), req, &Context{User: admin}); err != nil || (status != 0 && status != http.StatusOK) {
		t.Fatalf("preview small: %d %v", status, err)
	}
	if _, ok := check("/pics/half.png"); ok {
		t.Fatal("half.png decided with only the small size cached")
	}
}
