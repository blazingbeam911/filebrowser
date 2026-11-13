package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

	"github.com/gtsteffaniak/filebrowser/backend/internal/adapters/fs/files"
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
	spec := thumbjob.Spec{Source: name, Path: "/pics", User: "admin", Prefixes: []string{"/pics/"}, Data: thumbJobData{user: admin, scope: ""}}
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

// The API: users without API access are refused on every route; an admin can count, queue (a
// job per folder), list and cancel; a file or a missing folder is refused before anything is
// queued.
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
	insertThumbJobRows(t, name, []thumbJobRow{{"/a/", true, false}, {"/b/", true, false}, {"/a/x.png", false, true}, {"/a/notes.txt", false, false}})
	call := thumbJobCaller(t)

	for _, r := range []struct{ method, target, body string }{
		{"GET", "/thumbnails/jobs", ""},
		{"GET", "/thumbnails/jobs/count?source=" + name + "&path=/a", ""},
		{"POST", "/thumbnails/jobs", `{"source":"` + name + `","paths":["/a"]}`},
		{"DELETE", "/thumbnails/jobs/1", ""},
	} {
		if rec := call(regular, r.method, r.target, r.body); rec.Code != http.StatusForbidden {
			t.Errorf("user without API access %s %s: %d, want 403", r.method, r.target, rec.Code)
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
	waitThumbJobsIdle(t)
}

type thumbJobRow struct {
	path    string
	dir     bool
	preview bool
}

func insertThumbJobRows(t *testing.T, source string, rows []thumbJobRow) {
	t.Helper()
	db := indexing.GetIndexDB()
	for _, row := range rows {
		info := &iteminfo.FileInfo{Path: row.path, IsDir: row.dir, ItemInfo: iteminfo.ItemInfo{Name: filepath.Base(row.path), ModTime: time.Unix(1, 0), Type: "image/png", HasPreview: row.preview}}
		if row.dir {
			info.Type = "directory"
		}
		if err := db.InsertItem(source, row.path, info); err != nil {
			t.Fatal(err)
		}
	}
}

// thumbJobCaller calls the thumbnail-job routes as a user (session cookie).
func thumbJobCaller(t *testing.T) func(u *users.User, method, target, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	registerThumbJobRoutes(mux)
	return func(u *users.User, method, target, body string) *httptest.ResponseRecorder {
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
}

// waitThumbJobsIdle waits until the shared job queue has nothing queued or running, so one
// test's jobs don't run into the next test's source.
func waitThumbJobsIdle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		busy := false
		for _, st := range thumbJobs().List() {
			if st.State == thumbjob.StateQueued || st.State == thumbjob.StateRunning {
				busy = true
			}
		}
		if !busy {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("thumbnail jobs still running")
}

func waitThumbJob(t *testing.T, id string) thumbjob.Status {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if st, ok := thumbJobs().Get(id); ok && st.State != thumbjob.StateQueued && st.State != thumbjob.StateRunning {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s didn't finish", id)
	return thumbjob.Status{}
}

func setThumbJobUserPerms(t *testing.T, u *users.User, api bool, view bool) {
	t.Helper()
	u.Permissions.Api = api
	if err := state.UpdateUser(u, "", "permissions"); err != nil {
		t.Fatal(err)
	}
	for i := range u.BackendScopes {
		u.BackendScopes[i].Permissions.View = view
	}
	users.SyncBackendSourcePermissionsMap(u)
	if err := state.UpdateUser(u, "", "backendScopes"); err != nil {
		t.Fatal(err)
	}
}

// A user with API access (not an admin) can queue jobs, sees and cancels only their own, and
// needs permission to view files in the source; admins see everyone's.
func TestThumbJobAPIUsers(t *testing.T) {
	startThumbJobPreviews(t)
	root, name, admin, regular := setupThumbJobSource(t)
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	insertThumbJobRows(t, name, []thumbJobRow{{"/a/", true, false}})
	call := thumbJobCaller(t)
	setThumbJobUserPerms(t, regular, true, true)

	rec := call(regular, "POST", "/thumbnails/jobs", `{"source":"`+name+`","paths":["/a"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("API user queue: %d %s", rec.Code, rec.Body)
	}
	if rec := call(regular, "GET", "/thumbnails/jobs/count?source="+name+"&path=/a", ""); rec.Code != http.StatusOK {
		t.Fatalf("API user count: %d %s", rec.Code, rec.Body)
	}
	adminJob := thumbJobs().Add(thumbjob.Spec{Source: name, Path: "/a", User: admin.Username, Prefixes: []string{"/a/"}, Data: thumbJobData{user: admin}})
	waitThumbJobsIdle(t)

	rec = call(regular, "GET", "/thumbnails/jobs", "")
	if rec.Code != http.StatusOK || bytes.Contains(rec.Body.Bytes(), []byte(`"user":"admin"`)) || !bytes.Contains(rec.Body.Bytes(), []byte(`"user":"regular"`)) {
		t.Fatalf("API user list: %d %s, want only their own jobs", rec.Code, rec.Body)
	}
	if rec := call(admin, "GET", "/thumbnails/jobs", ""); !bytes.Contains(rec.Body.Bytes(), []byte(`"user":"regular"`)) || !bytes.Contains(rec.Body.Bytes(), []byte(`"user":"admin"`)) {
		t.Fatalf("admin list: %s, want everyone's jobs", rec.Body)
	}
	if rec := call(regular, "DELETE", "/thumbnails/jobs/"+adminJob.ID, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("API user cancels the admin's job: %d, want 404", rec.Code)
	}

	setThumbJobUserPerms(t, regular, true, false)
	if rec := call(regular, "POST", "/thumbnails/jobs", `{"source":"`+name+`","paths":["/a"]}`); rec.Code != http.StatusForbidden {
		t.Fatalf("queue without View: %d %s, want 403", rec.Code, rec.Body)
	}
}

// A refresh job takes folders that exist on disk but not yet in the index (a new folder with
// new folders in it, and a new file in a known folder): the job brings them into the index and
// makes their thumbnails. Folders gone from disk are left out; nested ones are one range.
func TestThumbJobRefreshNewFolders(t *testing.T) {
	startThumbJobPreviews(t)
	root, name, admin, _ := setupThumbJobSource(t)
	indexing.SetTestIndexWithDB(name, root)
	write := func(rel string) {
		t.Helper()
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, noisyPNG(t, 700, 500), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("site/old/a.png")
	if err := files.RefreshIndex(name, "/", true, true); err != nil {
		t.Fatal(err)
	}
	db := indexing.GetIndexDB()
	if _, err := db.GetItem(name, "/site/old/"); err != nil {
		t.Fatalf("the starting index lacks /site/old/: %v", err)
	}
	// Written after the index saw the tree: a new creator with two new posts, a new file in a
	// known folder.
	write("site/newcreator/post1/1.png")
	write("site/newcreator/post1/2.png")
	write("site/newcreator/post2/3.png")
	write("site/old/b.png")
	if _, err := db.GetItem(name, "/site/newcreator/"); err == nil {
		t.Fatal("the new folder is in the index before the job")
	}
	call := thumbJobCaller(t)

	// Without refresh, a folder not in the index is refused, as before.
	if rec := call(admin, "POST", "/thumbnails/jobs", `{"source":"`+name+`","paths":["/site/newcreator/post1"]}`); rec.Code != http.StatusNotFound {
		t.Fatalf("new folder without refresh: %d %s, want 404", rec.Code, rec.Body)
	}
	body := `{"source":"` + name + `","refresh":true,"oneJob":true,"paths":["/site/newcreator/post1","/site/newcreator/post2","/site/old","/site/old/sub/../","/site/gone"]}`
	rec := call(admin, "POST", "/thumbnails/jobs", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh job: %d %s", rec.Code, rec.Body)
	}
	var resp thumbJobsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Jobs) != 1 || len(resp.Missing) != 1 || resp.Missing[0] != "/site/gone" {
		t.Fatalf("response %+v, want one job and /site/gone missing", resp)
	}
	queued := resp.Jobs[0]
	if queued.Path != "/site" || len(queued.Paths) != 3 || !queued.Refresh {
		t.Fatalf("job %+v, want path /site with 3 folders, refresh", queued)
	}
	st := waitThumbJob(t, queued.ID)
	if st.State != thumbjob.StateDone || st.Total != 5 || st.Generated != 5 {
		t.Fatalf("job %+v, want done with all 5 files made", st)
	}
	for _, p := range []string{"/site/newcreator/", "/site/newcreator/post1/", "/site/newcreator/post2/3.png", "/site/old/b.png"} {
		if _, err := db.GetItem(name, p); err != nil {
			t.Errorf("%s not in the index after the job: %v", p, err)
		}
	}

	// Only missing folders: nothing queued, not an error.
	rec = call(admin, "POST", "/thumbnails/jobs", `{"source":"`+name+`","refresh":true,"oneJob":true,"paths":["/site/gone"]}`)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"jobs":[]`)) {
		t.Fatalf("only missing folders: %d %s", rec.Code, rec.Body)
	}
	// A file, with refresh: refused.
	if rec := call(admin, "POST", "/thumbnails/jobs", `{"source":"`+name+`","refresh":true,"paths":["/site/old/a.png"]}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("a file with refresh: %d, want 400", rec.Code)
	}
}

// A saved job comes back as the user who queued it, with the same checks as a new request.
func TestThumbJobRestore(t *testing.T) {
	startThumbJobPreviews(t)
	root, name, _, regular := setupThumbJobSource(t)
	for _, dir := range []string{"x/one", "x/two"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	saved := thumbjob.Spec{Source: name, Path: "/x", Paths: []string{"/x/one", "/x/two"}, User: regular.Username, Refresh: true}
	if _, err := restoreThumbJob(saved); err == nil {
		t.Fatal("restored for a user without API access")
	}
	setThumbJobUserPerms(t, regular, true, true)
	spec, err := restoreThumbJob(saved)
	if err != nil {
		t.Fatal(err)
	}
	data, ok := spec.Data.(thumbJobData)
	if !ok || data.user == nil || data.user.Username != regular.Username || len(spec.Prefixes) != 2 || spec.Prefixes[0] != "/x/one/" {
		t.Fatalf("restored %+v", spec)
	}
	if _, err := restoreThumbJob(thumbjob.Spec{Source: name, Path: "/x", User: "nobody", Refresh: true}); err == nil || errors.Is(err, thumbjob.ErrNotReady) {
		t.Fatalf("unknown user: %v", err)
	}
	if _, err := restoreThumbJob(thumbjob.Spec{Source: "nosource", Path: "/x", User: regular.Username}); !errors.Is(err, thumbjob.ErrNotReady) {
		t.Fatalf("unknown source: %v, want not ready", err)
	}
	if err := os.RemoveAll(filepath.Join(root, "x")); err != nil {
		t.Fatal(err)
	}
	if _, err := restoreThumbJob(saved); err == nil || errors.Is(err, thumbjob.ErrNotReady) {
		t.Fatalf("folders gone: %v", err)
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
	spec := thumbjob.Spec{Source: name, Path: "/pics", User: "admin", Prefixes: []string{"/pics/"}, Data: thumbJobData{user: admin, scope: ""}}
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
