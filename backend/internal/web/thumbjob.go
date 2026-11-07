package web

// fufu: API for the thumbnail job (internal/thumbjob). Admins queue a job per folder; each job
// fills the cache with the small and large thumbnails of every previewable file under it, the
// same thumbnails (under the same keys) a browse request would make.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
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

// registerThumbJobRoutes adds the thumbnail-job API under /api/thumbnails/, admins only.
func registerThumbJobRoutes(api *http.ServeMux) {
	api.HandleFunc("POST /thumbnails/jobs", withAdmin(thumbJobsPostHandler))
	api.HandleFunc("GET /thumbnails/jobs", withAdmin(thumbJobsGetHandler))
	api.HandleFunc("GET /thumbnails/jobs/count", withAdmin(thumbJobsCountHandler))
	api.HandleFunc("DELETE /thumbnails/jobs/{id}", withAdmin(thumbJobDeleteHandler))
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
	})
	return thumbJobsMgr
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
	return db.CountPreviewableFiles(spec.Source, spec.Prefix)
}

func (thumbJobBackend) Page(spec thumbjob.Spec, after string, limit int) ([]string, error) {
	db := indexing.GetIndexDB()
	if db == nil {
		return nil, errors.New("index database not available")
	}
	return db.PreviewableFilesAfter(spec.Source, spec.Prefix, after, limit)
}

func (thumbJobBackend) Process(ctx context.Context, spec thumbjob.Spec, indexPath string) (thumbjob.Outcome, error) {
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

// thumbJobFolder resolves a folder for a job: checks it is an indexed folder the admin can
// access and returns its spec. The folder is looked up in the index database, not listed,
// since a job's folder can be huge (and a folder the index skips has nothing to list anyway).
func thumbJobFolder(d *requestContext, source, path string) (thumbjob.Spec, int, error) {
	idx := indexing.GetIndex(source)
	db := indexing.GetIndexDB()
	if idx == nil || db == nil {
		return thumbjob.Spec{}, http.StatusBadRequest, fmt.Errorf("index not found for source %s", source)
	}
	if idx.IsBuilding() {
		return thumbjob.Spec{}, http.StatusServiceUnavailable, fmt.Errorf("the index of %s is still being built, try again when indexing completes", source)
	}
	clean, err := utils.SanitizePath(path)
	if err != nil {
		return thumbjob.Spec{}, http.StatusBadRequest, fmt.Errorf("invalid path %q", path)
	}
	scope, err := d.User.GetScopeForSourceName(idx.Name)
	if err != nil {
		return thumbjob.Spec{}, http.StatusForbidden, err
	}
	scope = strings.TrimRight(scope, "/")
	scoped := utils.JoinPathAsUnix(scope, clean)
	prefix := idx.MakeIndexPath(scoped, true)
	if !state.AccessPermitted(idx.Path, prefix, d.User.Username) {
		return thumbjob.Spec{}, http.StatusForbidden, fmt.Errorf("user is not allowed to access this location")
	}
	if prefix.String() != "/" {
		if _, err := db.GetItem(idx.Name, prefix.String()); err != nil {
			if _, fileErr := db.GetItem(idx.Name, idx.MakeIndexPath(scoped, false).String()); fileErr == nil {
				return thumbjob.Spec{}, http.StatusBadRequest, fmt.Errorf("%s is not a folder", path)
			}
			return thumbjob.Spec{}, http.StatusNotFound, fmt.Errorf("folder %s is not in the index", path)
		}
	}
	return thumbjob.Spec{
		Source: idx.Name,
		Path:   clean,
		User:   d.User.Username,
		Prefix: prefix.String(),
		Data:   thumbJobData{user: d.User, scope: scope},
	}, 0, nil
}

type thumbJobsResponse struct {
	Jobs []thumbjob.Status `json:"jobs"`
	// SecondsPerFile is the latest measured time per file that needed thumbnails, 0 = unknown.
	SecondsPerFile float64 `json:"secondsPerFile"`
}

// thumbJobsPostHandler queues a job for each folder.
// @Summary Queue thumbnail jobs
// @Description Admin only. Queues one background job per folder that makes the small and large thumbnails of every previewable file under it. Already-cached files are skipped.
// @Tags Thumbnails
// @Accept json
// @Produce json
// @Param body body object true "{source, paths[]}"
// @Success 200 {object} thumbJobsResponse
// @Router /api/thumbnails/jobs [post]
func thumbJobsPostHandler(w http.ResponseWriter, r *http.Request, d *requestContext) (int, error) {
	if settings.Config.Server.DisablePreviews {
		return http.StatusNotImplemented, fmt.Errorf("previews are disabled")
	}
	var req struct {
		Source string   `json:"source"`
		Paths  []string `json:"paths"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		return http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err)
	}
	if req.Source == "" || len(req.Paths) == 0 {
		return http.StatusBadRequest, fmt.Errorf("source and paths are required")
	}
	// Check every folder before queueing any, so a bad one doesn't leave half the jobs queued.
	specs := make([]thumbjob.Spec, 0, len(req.Paths))
	for _, p := range req.Paths {
		spec, status, err := thumbJobFolder(d, req.Source, p)
		if err != nil {
			return status, err
		}
		specs = append(specs, spec)
	}
	mgr := thumbJobs()
	resp := thumbJobsResponse{SecondsPerFile: mgr.SecondsPerGenerated()}
	for _, spec := range specs {
		resp.Jobs = append(resp.Jobs, mgr.Add(spec))
	}
	return RenderJSON(w, r, resp)
}

// thumbJobsGetHandler lists running, queued and finished jobs.
// @Summary List thumbnail jobs
// @Tags Thumbnails
// @Produce json
// @Success 200 {object} thumbJobsResponse
// @Router /api/thumbnails/jobs [get]
func thumbJobsGetHandler(w http.ResponseWriter, r *http.Request, d *requestContext) (int, error) {
	mgr := thumbJobs()
	return RenderJSON(w, r, thumbJobsResponse{Jobs: mgr.List(), SecondsPerFile: mgr.SecondsPerGenerated()})
}

// thumbJobsCountHandler counts a folder's previewable files, for the start dialog.
// @Summary Count previewable files for a thumbnail job
// @Tags Thumbnails
// @Produce json
// @Param source query string true "Source name"
// @Param path query string true "Folder path"
// @Router /api/thumbnails/jobs/count [get]
func thumbJobsCountHandler(w http.ResponseWriter, r *http.Request, d *requestContext) (int, error) {
	spec, status, err := thumbJobFolder(d, r.URL.Query().Get("source"), r.URL.Query().Get("path"))
	if err != nil {
		return status, err
	}
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
func thumbJobDeleteHandler(w http.ResponseWriter, r *http.Request, d *requestContext) (int, error) {
	st, ok := thumbJobs().Cancel(r.PathValue("id"))
	if !ok {
		return http.StatusNotFound, fmt.Errorf("no such job")
	}
	return RenderJSON(w, r, st)
}
