package web

// fufu: API for the thumbnail job (internal/thumbjob). Admins and users with API access queue
// jobs for folders they can see; each job fills the cache with the small and large thumbnails
// of every previewable file under its folders, the same thumbnails (under the same keys) a
// browse request would make. A job can be asked to refresh its folders in the index first, so
// a downloader can send folders it has just written (before any scan has seen them).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/gtsteffaniak/filebrowser/backend/internal/adapters/fs/files"
	"github.com/gtsteffaniak/filebrowser/backend/internal/database/users"
	"github.com/gtsteffaniak/filebrowser/backend/internal/events"
	"github.com/gtsteffaniak/filebrowser/backend/internal/preview"
	"github.com/gtsteffaniak/filebrowser/backend/internal/state"
	"github.com/gtsteffaniak/filebrowser/backend/internal/thumbjob"
	"github.com/gtsteffaniak/filebrowser/backend/internal/utils"
	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing"
	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing/iteminfo"
	"github.com/gtsteffaniak/filebrowser/backend/pkg/settings"
	"github.com/gtsteffaniak/go-logger/logger"
)

// registerThumbJobRoutes adds the thumbnail-job API under /api/thumbnails/, for admins and
// users with API access, and starts the job queue (which queues saved jobs again).
func registerThumbJobRoutes(api *http.ServeMux) {
	api.HandleFunc("POST /thumbnails/jobs", withThumbJobAccess(thumbJobsPostHandler))
	api.HandleFunc("GET /thumbnails/jobs", withThumbJobAccess(thumbJobsGetHandler))
	api.HandleFunc("GET /thumbnails/jobs/count", withThumbJobAccess(thumbJobsCountHandler))
	api.HandleFunc("DELETE /thumbnails/jobs/{id}", withThumbJobAccess(thumbJobDeleteHandler))
	thumbJobs()
}

// canUseThumbJobs: admins, and users with API access (the permission tokens and WebDAV need).
func canUseThumbJobs(u *users.User) bool {
	return u != nil && (u.Permissions.Admin || u.Permissions.Api)
}

func withThumbJobAccess(fn handleFunc) http.HandlerFunc {
	return withUser(func(w http.ResponseWriter, r *http.Request, d *requestContext) (int, error) {
		if !canUseThumbJobs(d.User) {
			return http.StatusForbidden, nil
		}
		return fn(w, r, d)
	})
}

// thumbJobSizes are the sizes a job fills: list views (small) and the gallery view (large).
var thumbJobSizes = []string{"small", "large"}

// previewFileOptions are the file-info options of a preview request. The job uses the same
// ones, so a file resolves to the same real path, size and time, and so the same cache key.
func previewFileOptions(path, source string) utils.FileOptions {
	return utils.FileOptions{
		Path:           path,
		Source:         source,
		AlbumArt:       true, // Extract album art for audio previews
		Expand:         true,
		FollowSymlinks: true,
	}
}

// servedAsOriginal reports whether a preview request for file at size sends the file itself
// (PreviewHelperFunc's rule), so there is no thumbnail to make.
func servedAsOriginal(file *iteminfo.ExtendedFileInfo, size string) bool {
	ext := strings.ToLower(filepath.Ext(file.Name))
	return strings.HasPrefix(file.Type, "image") &&
		shouldServeOriginalPreview(ext, iteminfo.ResizableImageTypes[ext], file.RealPath, size, file.Size)
}

var (
	thumbJobsOnce sync.Once
	thumbJobsMgr  *thumbjob.Manager
)

func thumbJobs() *thumbjob.Manager {
	thumbJobsOnce.Do(func() {
		thumbJobsMgr = thumbjob.New(thumbJobBackend{}, func(user string, st thumbjob.Status) {
			msg, err := json.Marshal(st)
			if err != nil {
				return
			}
			events.SendToUsers("thumbnailJob", string(msg), []string{user})
		})
		if path := thumbJobSavePath(); path != "" {
			thumbJobsMgr.Persist(path, restoreThumbJob)
		}
	})
	return thumbJobsMgr
}

// thumbJobSavePath is where the job queue is saved: thumbjobs.json next to the database.
func thumbJobSavePath() string {
	db := settings.Config.Server.DatabaseV2.Path
	if db == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(db), "thumbjobs.json")
}

// restoreThumbJob rebuilds a saved job at start, with the same checks as a new request, as the
// user who queued it (looked up again: their access may have changed).
func restoreThumbJob(spec thumbjob.Spec) (thumbjob.Spec, error) {
	idx := indexing.GetIndex(spec.Source)
	if idx == nil || indexing.GetIndexDB() == nil || idx.IsBuilding() {
		return thumbjob.Spec{}, thumbjob.ErrNotReady
	}
	u, err := state.GetUserByUsername(spec.User)
	if err != nil {
		return thumbjob.Spec{}, fmt.Errorf("user %s: %w", spec.User, err)
	}
	if !canUseThumbJobs(&u) {
		return thumbjob.Spec{}, fmt.Errorf("user %s may no longer start thumbnail jobs", spec.User)
	}
	paths := spec.Paths
	if len(paths) == 0 {
		paths = []string{spec.Path}
	}
	specs, missing, _, err := thumbJobSpecs(&u, spec.Source, paths, spec.Refresh, true)
	if err != nil {
		if errors.Is(err, errThumbJobNotReady) {
			return thumbjob.Spec{}, thumbjob.ErrNotReady
		}
		return thumbjob.Spec{}, err
	}
	if len(specs) == 0 {
		return thumbjob.Spec{}, fmt.Errorf("its folders are gone: %s", strings.Join(missing, ", "))
	}
	return specs[0], nil
}

// thumbJobData is what the backend needs to act as the user who started the job.
type thumbJobData struct {
	user  *users.User
	scope string // the user's scope in the source, without a trailing "/" ("" for the root)
}

type thumbJobBackend struct{}

func (thumbJobBackend) Count(spec thumbjob.Spec) (int, error) {
	db := indexing.GetIndexDB()
	if db == nil {
		return 0, errors.New("index database not available")
	}
	total := 0
	for _, prefix := range spec.Prefixes {
		n, err := db.CountPreviewableFiles(spec.Source, prefix)
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

func (thumbJobBackend) Page(spec thumbjob.Spec, prefix, after string, limit int) ([]string, error) {
	db := indexing.GetIndexDB()
	if db == nil {
		return nil, errors.New("index database not available")
	}
	return db.PreviewableFilesAfter(spec.Source, prefix, after, limit)
}

// Prepare, for a job that asks for it, brings its folders into the index, so files written
// since the last scan are in the job. A folder already in the index is refreshed with
// everything under it. A new folder can't be refreshed on its own (the index adds folders
// under a parent it knows), so the nearest folder above it that is in the index is refreshed
// (just that level, which adds the new folder), then the highest new folder with everything
// under it.
func (thumbJobBackend) Prepare(ctx context.Context, spec thumbjob.Spec) error {
	if !spec.Refresh {
		return nil
	}
	db := indexing.GetIndexDB()
	if db == nil {
		return errors.New("index database not available")
	}
	var done []string // folders refreshed with everything under them
	for _, prefix := range spec.Prefixes {
		if ctx.Err() != nil {
			return nil
		}
		if under(prefix, done) {
			continue
		}
		top, isNew := prefix, false
		for p := prefix; p != "/"; p = parentIndexDir(p) {
			if _, err := db.GetItem(spec.Source, p); err == nil {
				break
			}
			top, isNew = p, true
		}
		if under(top, done) {
			continue
		}
		if isNew {
			if err := files.RefreshIndex(spec.Source, parentIndexDir(top), true, false); err != nil {
				return fmt.Errorf("%s: %w", parentIndexDir(top), err)
			}
		}
		if err := files.RefreshIndex(spec.Source, top, true, true); err != nil {
			return fmt.Errorf("%s: %w", top, err)
		}
		done = append(done, top)
	}
	return nil
}

// under reports whether index path p is one of dirs or inside one of them (dirs end in "/").
func under(p string, dirs []string) bool {
	for _, d := range dirs {
		if strings.HasPrefix(p, d) {
			return true
		}
	}
	return false
}

// parentIndexDir is the folder an index folder path ("/a/b/") is in ("/a/"); "/" for "/x/".
func parentIndexDir(p string) string {
	p = strings.TrimSuffix(p, "/")
	i := strings.LastIndex(p, "/")
	if i <= 0 {
		return "/"
	}
	return p[:i+1]
}

// Busy: someone is browsing and a preview is being made for them (preview/priority.go).
func (thumbJobBackend) Busy() bool {
	return preview.BrowsingBusy()
}

// Check is the quick look at a file that may already be done: one stat of the file and one of
// each cache entry, no xattrs, no image header, no reading cache entries. It decides only
//   - a plain regular file (not a symlink; Process follows it to its target) whose small and
//     large thumbnails are all in the cache under the key browsing computes, and
//   - an image browsing sends as it is because it is under 256 KB (or resizing is off).
//
// Everything else (missing sizes, the empty marker, audio with album art, symlinks, images
// that might already fit the bounds) goes to Process, which does the full lookup.
func (thumbJobBackend) Check(_ context.Context, spec thumbjob.Spec, indexPath string) (thumbjob.Outcome, bool) {
	idx := indexing.GetIndex(spec.Source)
	if idx == nil {
		return thumbjob.Failed, false
	}
	realPath := utils.JoinUnderSourceRoot(idx.Path, indexPath)
	fi, err := os.Lstat(realPath)
	if err != nil || !fi.Mode().IsRegular() {
		return thumbjob.Failed, false
	}
	size := idx.DisplaySize(fi)
	ext := strings.ToLower(filepath.Ext(fi.Name()))
	if quickServedAsOriginal(ext, size) {
		return thumbjob.Cached, true
	}
	if preview.CachedFromStat(realPath, fi, thumbJobSizes) {
		return thumbjob.Cached, true
	}
	return thumbjob.Failed, false
}

// quickServedAsOriginal is shouldServeOriginalPreview without the dimension read: true only
// when that rule says so from the extension and size alone.
func quickServedAsOriginal(ext string, size int64) bool {
	const maxSizeForOriginal = 256 * 1024
	if !(settings.Config.Server.DisableResize || size < maxSizeForOriginal) {
		return false
	}
	return shouldServeOriginalPreview(ext, iteminfo.ResizableImageTypes[ext], "", "small", size)
}

func (thumbJobBackend) Process(ctx context.Context, spec thumbjob.Spec, indexPath string) (thumbjob.Outcome, error) {
	ctx = preview.Background(ctx) // the job's own previews don't count as browsing
	data, ok := spec.Data.(thumbJobData)
	if !ok || data.user == nil {
		return thumbjob.Failed, errors.New("job has no user")
	}
	userPath := strings.TrimPrefix(indexPath, data.scope)
	file, err := files.FileInfoFaster(previewFileOptions(userPath, spec.Source), data.user)
	if err != nil {
		if ErrToStatus(err) == http.StatusNotFound {
			return thumbjob.Skipped, nil // gone since the index saw it
		}
		return thumbjob.Failed, err
	}
	// Office previews are rendered by OnlyOffice from a download link with the requester's
	// token, which a background job doesn't have.
	if file.Type == "directory" || !file.HasPreview || file.OnlyOfficeId != "" {
		return thumbjob.Skipped, nil
	}
	var sizes []string
	for _, size := range thumbJobSizes {
		if !servedAsOriginal(file, size) {
			sizes = append(sizes, size)
		}
	}
	if len(sizes) == 0 {
		return thumbjob.Cached, nil
	}
	res, err := preview.Pregenerate(ctx, *file, sizes)
	if err != nil {
		if errors.Is(err, preview.ErrUnsupportedFormat) || errors.Is(err, preview.ErrUnsupportedMedia) {
			return thumbjob.Skipped, nil
		}
		if errors.Is(err, preview.ErrPreviewTooSmall) {
			err = fmt.Errorf("no usable image (from this or an earlier attempt): %w", err)
		}
		return thumbjob.Failed, err
	}
	if res.Generated > 0 {
		return thumbjob.Generated, nil
	}
	return thumbjob.Cached, nil
}

var (
	errThumbJobFolderMissing = errors.New("folder not found")
	errThumbJobNotReady      = errors.New("index not ready")
)

// thumbJobFolderInfo is one folder of a request, resolved.
type thumbJobFolderInfo struct {
	source string // the index's name
	clean  string // the folder as the user sees it
	scope  string // the user's scope in the source, without a trailing "/"
	prefix string // index path ("/a/b/")
}

// thumbJobFolder resolves a folder for a job: checks the user may see files in the source and
// may access the folder, and that it is a folder: in the index database (not listed, since a
// job's folder can be huge, and a folder the index skips has nothing to list anyway), or, for
// a job that refreshes its folders first (onDisk), on disk.
func thumbJobFolder(u *users.User, source, path string, onDisk bool) (thumbJobFolderInfo, int, error) {
	idx := indexing.GetIndex(source)
	db := indexing.GetIndexDB()
	if idx == nil || db == nil {
		return thumbJobFolderInfo{}, http.StatusBadRequest, fmt.Errorf("index not found for source %s", source)
	}
	if idx.IsBuilding() {
		return thumbJobFolderInfo{}, http.StatusServiceUnavailable, fmt.Errorf("the index of %s is still being built, try again when indexing completes: %w", source, errThumbJobNotReady)
	}
	clean, err := utils.SanitizePath(path)
	if err != nil {
		return thumbJobFolderInfo{}, http.StatusBadRequest, fmt.Errorf("invalid path %q", path)
	}
	perms, err := u.FilePermsForSourceName(idx.Name)
	if err != nil {
		return thumbJobFolderInfo{}, http.StatusForbidden, err
	}
	if !perms.View { // as for a browse preview (previewHandler)
		return thumbJobFolderInfo{}, http.StatusForbidden, fmt.Errorf("user is not allowed to view files in this source")
	}
	scope, err := u.GetScopeForSourceName(idx.Name)
	if err != nil {
		return thumbJobFolderInfo{}, http.StatusForbidden, err
	}
	scope = strings.TrimRight(scope, "/")
	scoped := utils.JoinPathAsUnix(scope, clean)
	prefix := idx.MakeIndexPath(scoped, true)
	if !state.AccessPermitted(idx.Path, prefix, u.Username) {
		return thumbJobFolderInfo{}, http.StatusForbidden, fmt.Errorf("user is not allowed to access this location")
	}
	info := thumbJobFolderInfo{source: idx.Name, clean: clean, scope: scope, prefix: prefix.String()}
	if onDisk {
		fi, err := os.Stat(utils.JoinUnderSourceRoot(idx.Path, info.prefix))
		switch {
		case errors.Is(err, os.ErrNotExist):
			return thumbJobFolderInfo{}, http.StatusNotFound, fmt.Errorf("folder %s: %w", path, errThumbJobFolderMissing)
		case err != nil:
			return thumbJobFolderInfo{}, http.StatusInternalServerError, fmt.Errorf("folder %s: %w", path, err)
		case !fi.IsDir():
			return thumbJobFolderInfo{}, http.StatusBadRequest, fmt.Errorf("%s is not a folder", path)
		}
		return info, 0, nil
	}
	if info.prefix != "/" {
		if _, err := db.GetItem(idx.Name, info.prefix); err != nil {
			if _, fileErr := db.GetItem(idx.Name, idx.MakeIndexPath(scoped, false).String()); fileErr == nil {
				return thumbJobFolderInfo{}, http.StatusBadRequest, fmt.Errorf("%s is not a folder", path)
			}
			return thumbJobFolderInfo{}, http.StatusNotFound, fmt.Errorf("folder %s is not in the index", path)
		}
	}
	return info, 0, nil
}

// thumbJobSpecs resolves a request's folders into job specs: one job per folder, or with
// oneJob a single job for all of them (folders inside another listed one left out). Every
// folder is checked before any spec is made, so a bad one refuses the whole request; with
// refresh, folders missing on disk are left out instead and returned in missing.
func thumbJobSpecs(u *users.User, source string, paths []string, refresh, oneJob bool) (specs []thumbjob.Spec, missing []string, status int, err error) {
	var folders []thumbJobFolderInfo
	for _, p := range paths {
		f, status, err := thumbJobFolder(u, source, p, refresh)
		if err != nil {
			if refresh && errors.Is(err, errThumbJobFolderMissing) {
				missing = append(missing, p)
				continue
			}
			return nil, nil, status, err
		}
		folders = append(folders, f)
	}
	if len(folders) == 0 {
		return nil, missing, 0, nil
	}
	data := thumbJobData{user: u, scope: folders[0].scope}
	if !oneJob {
		for _, f := range folders {
			specs = append(specs, thumbjob.Spec{Source: f.source, Path: f.clean, User: u.Username, Refresh: refresh,
				Prefixes: []string{f.prefix}, Data: data})
		}
		return specs, missing, 0, nil
	}
	// One job: the folders in index order, none inside another (a folder inside another is
	// in that one's range already).
	sort.Slice(folders, func(a, b int) bool { return folders[a].prefix < folders[b].prefix })
	var kept []thumbJobFolderInfo
	var prefixes []string
	for _, f := range folders {
		if under(f.prefix, prefixes) {
			continue
		}
		kept = append(kept, f)
		prefixes = append(prefixes, f.prefix)
	}
	spec := thumbjob.Spec{Source: kept[0].source, Path: kept[0].clean, User: u.Username, Refresh: refresh,
		Prefixes: prefixes, Data: data}
	if len(kept) > 1 {
		for _, f := range kept {
			spec.Paths = append(spec.Paths, f.clean)
		}
		spec.Path = commonFolder(spec.Paths)
	}
	return []thumbjob.Spec{spec}, missing, 0, nil
}

// commonFolder is the deepest folder all of paths ("/a/b", "/a/c") are in ("/a").
func commonFolder(paths []string) string {
	common := strings.Split(strings.Trim(paths[0], "/"), "/")
	for _, p := range paths[1:] {
		parts := strings.Split(strings.Trim(p, "/"), "/")
		n := 0
		for n < len(common) && n < len(parts) && common[n] == parts[n] {
			n++
		}
		common = common[:n]
	}
	if len(common) == 0 || common[0] == "" {
		return "/"
	}
	return "/" + strings.Join(common, "/")
}

type thumbJobsResponse struct {
	Jobs []thumbjob.Status `json:"jobs"`
	// Missing are requested folders left out because they don't exist (refresh requests only).
	Missing []string `json:"missing,omitempty"`
	// SecondsPerFile is the latest measured time per file that needed thumbnails, 0 = unknown.
	SecondsPerFile float64 `json:"secondsPerFile"`
}

// thumbJobsPostHandler queues a job for each folder, or one for all of them.
// @Summary Queue thumbnail jobs
// @Description Admins and users with API access. Queues background jobs that make the small and large thumbnails of every previewable file under the folders: one job per folder, or with oneJob one job for all of them. With refresh, the folders only have to exist on disk: each job first refreshes them in the index (for folders just written), and folders that don't exist are left out and listed in missing. A request already covered by one of the user's queued jobs returns that job (merged). Already-cached files are skipped.
// @Tags Thumbnails
// @Accept json
// @Produce json
// @Param body body object true "{source, paths[], refresh, oneJob}"
// @Success 200 {object} thumbJobsResponse
// @Router /api/thumbnails/jobs [post]
func thumbJobsPostHandler(w http.ResponseWriter, r *http.Request, d *requestContext) (int, error) {
	if settings.Config.Server.DisablePreviews {
		return http.StatusNotImplemented, fmt.Errorf("previews are disabled")
	}
	var req struct {
		Source  string   `json:"source"`
		Paths   []string `json:"paths"`
		Refresh bool     `json:"refresh"`
		OneJob  bool     `json:"oneJob"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		return http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err)
	}
	if req.Source == "" || len(req.Paths) == 0 {
		return http.StatusBadRequest, fmt.Errorf("source and paths are required")
	}
	specs, missing, status, err := thumbJobSpecs(d.User, req.Source, req.Paths, req.Refresh, req.OneJob)
	if err != nil {
		return status, err
	}
	mgr := thumbJobs()
	resp := thumbJobsResponse{Jobs: []thumbjob.Status{}, Missing: missing, SecondsPerFile: mgr.SecondsPerGenerated()}
	for _, spec := range specs {
		resp.Jobs = append(resp.Jobs, mgr.Add(spec))
	}
	if len(missing) > 0 {
		logger.Infof("[Thumbnails] request by %s: %d folder(s) not found, left out: %s", d.User.Username, len(missing), strings.Join(missing, ", "))
	}
	return RenderJSON(w, r, resp)
}

// thumbJobsGetHandler lists running, queued and finished jobs.
// @Summary List thumbnail jobs
// @Tags Thumbnails
// @Produce json
// @Success 200 {object} thumbJobsResponse
// @Router /api/thumbnails/jobs [get]
// Admins get everyone's jobs, other users their own.
func thumbJobsGetHandler(w http.ResponseWriter, r *http.Request, d *requestContext) (int, error) {
	mgr := thumbJobs()
	jobs := mgr.List()
	if !d.User.Permissions.Admin {
		own := []thumbjob.Status{}
		for _, st := range jobs {
			if st.User == d.User.Username {
				own = append(own, st)
			}
		}
		jobs = own
	}
	return RenderJSON(w, r, thumbJobsResponse{Jobs: jobs, SecondsPerFile: mgr.SecondsPerGenerated()})
}

// thumbJobsCountHandler counts a folder's previewable files, for the start dialog.
// @Summary Count previewable files for a thumbnail job
// @Tags Thumbnails
// @Produce json
// @Param source query string true "Source name"
// @Param path query string true "Folder path"
// @Router /api/thumbnails/jobs/count [get]
func thumbJobsCountHandler(w http.ResponseWriter, r *http.Request, d *requestContext) (int, error) {
	f, status, err := thumbJobFolder(d.User, r.URL.Query().Get("source"), r.URL.Query().Get("path"), false)
	if err != nil {
		return status, err
	}
	spec := thumbjob.Spec{Source: f.source, Path: f.clean, Prefixes: []string{f.prefix}}
	n, err := thumbJobBackend{}.Count(spec)
	if err != nil {
		logger.Errorf("[Thumbnails] counting %s %s: %v", spec.Source, spec.Path, err)
		return http.StatusInternalServerError, err
	}
	return RenderJSON(w, r, map[string]any{
		"count":          n,
		"sizes":          thumbJobSizes,
		"secondsPerFile": thumbJobs().SecondsPerGenerated(),
	})
}

// thumbJobDeleteHandler cancels a job: a queued one is removed, a running one stops after the
// file it is on.
// @Summary Cancel a thumbnail job
// @Tags Thumbnails
// @Param id path string true "Job id"
// @Router /api/thumbnails/jobs/{id} [delete]
// Users other than admins can only cancel their own jobs (others' are "no such job").
func thumbJobDeleteHandler(w http.ResponseWriter, r *http.Request, d *requestContext) (int, error) {
	id := r.PathValue("id")
	if !d.User.Permissions.Admin {
		if st, ok := thumbJobs().Get(id); !ok || st.User != d.User.Username {
			return http.StatusNotFound, fmt.Errorf("no such job")
		}
	}
	st, ok := thumbJobs().Cancel(id)
	if !ok {
		return http.StatusNotFound, fmt.Errorf("no such job")
	}
	return RenderJSON(w, r, st)
}
