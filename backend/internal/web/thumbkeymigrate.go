package web

// fufu: the one-time walk that moves the thumbnail cache from the old path keys to file keys
// (preview/filekey_unix.go, preview/rekey.go), so thumbnails made before the update are found
// under the new keys instead of being made again. It runs in the background at start until it
// has finished once, which it records with a marker file in the cache directory. It goes
// through every file with a preview in each source's index, in path order, and saves its place
// after each page, so a restart carries on where it stopped (files already moved have no old
// entries left, so going over some of them again is harmless).

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	dbsql "github.com/gtsteffaniak/filebrowser/backend/internal/database/sql"
	"github.com/gtsteffaniak/filebrowser/backend/internal/preview"
	"github.com/gtsteffaniak/filebrowser/backend/internal/utils"
	"github.com/gtsteffaniak/filebrowser/backend/pkg/indexing"
	"github.com/gtsteffaniak/filebrowser/backend/pkg/settings"
	"github.com/gtsteffaniak/go-logger/logger"
)

var (
	thumbKeyPageSize      = 5000             // files per index page; the place is saved after each
	thumbKeyWorkers       = 8                // files looked at in parallel
	thumbKeyProgressEvery = int64(50000)     // files between progress lines in the log
	thumbKeyReadyWait     = 10 * time.Second // between looks at whether the indexes are ready
	thumbKeyRetryWait     = 5 * time.Second  // after a failed index read
	thumbKeyOnce          sync.Once
)

const (
	thumbKeyCursorName = preview.RekeyMarkerName + "-cursor" // the saved place, next to the marker
	thumbKeyMaxRetries = 10                                  // index reads per page before giving up
	thumbKeyMaxLogged  = 20                                  // file errors logged one by one
)

// thumbKeyCursor is the saved place: the source being walked and the last path done in it.
// Sources are walked in name order.
type thumbKeyCursor struct {
	Source string `json:"source"`
	After  string `json:"after"`
}

type thumbKeyStats struct {
	files, moved, dropped, freed, skipped, errors atomic.Int64
}

// startThumbKeyMigration starts the walk in the background unless the cache was re-keyed
// already.
func startThumbKeyMigration() {
	thumbKeyOnce.Do(func() {
		dir := preview.CacheDir()
		if dir == "" {
			return
		}
		if _, err := os.Stat(filepath.Join(dir, preview.RekeyMarkerName)); err == nil {
			return
		}
		go func() {
			if err := runThumbKeyMigration(dir); err != nil {
				logger.Errorf("[Thumbnails] re-keying the thumbnail cache stopped: %v; it carries on at the next start", err)
			}
		}()
	})
}

// thumbKeySources returns the source names in walk order.
func thumbKeySources() []string {
	names := make([]string, 0, len(settings.Config.Server.NameToSource))
	for name := range settings.Config.Server.NameToSource {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// thumbKeyReady reports whether the index database and every source's index are there and no
// index is being built.
func thumbKeyReady(names []string) bool {
	if indexing.GetIndexDB() == nil {
		return false
	}
	for _, name := range names {
		idx := indexing.GetIndex(name)
		if idx == nil || idx.IsBuilding() {
			return false
		}
	}
	return true
}

// runThumbKeyMigration walks every source and re-keys the cache entries of each file, then
// writes the marker. dir is the cache directory holding the entries.
func runThumbKeyMigration(dir string) error {
	names := thumbKeySources()
	if len(names) == 0 {
		return errors.New("no sources")
	}
	for !thumbKeyReady(names) {
		time.Sleep(thumbKeyReadyWait)
	}
	db := indexing.GetIndexDB()
	total := 0
	for _, name := range names {
		n, err := db.CountPreviewableFiles(name, "/")
		if err != nil {
			return fmt.Errorf("counting the files of source %q: %w", name, err)
		}
		total += n
	}
	if total == 0 {
		// An empty index would make the walk finish at once with nothing moved.
		return errors.New("the index lists no files with previews")
	}

	cursorPath := filepath.Join(dir, thumbKeyCursorName)
	cur := loadThumbKeyCursor(cursorPath)
	if cur.Source != "" {
		logger.Infof("[Thumbnails] re-keying the thumbnail cache: carrying on in source %q after %q (%d files with previews in all)", cur.Source, cur.After, total)
	} else {
		logger.Infof("[Thumbnails] re-keying the thumbnail cache: %d files with previews in %d sources", total, len(names))
	}

	var st thumbKeyStats
	start := time.Now()
	nextProgress := thumbKeyProgressEvery
	for _, name := range names {
		if cur.Source != "" && name < cur.Source {
			continue // done before the restart
		}
		after := "/"
		if name == cur.Source {
			after = cur.After
		}
		idx := indexing.GetIndex(name)
		if idx == nil {
			return fmt.Errorf("source %q has no index", name)
		}
		for {
			page, err := thumbKeyPage(db, name, after)
			if err != nil {
				return err
			}
			if len(page) == 0 {
				break
			}
			rekeyThumbKeyPage(idx, page, &st)
			after = page[len(page)-1].Path
			if err := saveThumbKeyCursor(cursorPath, thumbKeyCursor{Source: name, After: after}); err != nil {
				logger.Warningf("[Thumbnails] can't save the re-keying place: %v", err)
			}
			if n := st.files.Load(); n >= nextProgress {
				logger.Infof("[Thumbnails] re-keying: %d of %d files, %d entries moved, %d duplicates deleted (%s)",
					n, total, st.moved.Load(), st.dropped.Load(), time.Since(start).Round(time.Second))
				nextProgress = (n/thumbKeyProgressEvery + 1) * thumbKeyProgressEvery
			}
		}
	}

	summary := fmt.Sprintf("re-keyed %s: %d files, %d entries moved, %d duplicates deleted (%d bytes freed), %d files skipped, %d errors, in %s",
		time.Now().UTC().Format(time.RFC3339), st.files.Load(), st.moved.Load(), st.dropped.Load(), st.freed.Load(),
		st.skipped.Load(), st.errors.Load(), time.Since(start).Round(time.Second))
	if err := os.WriteFile(filepath.Join(dir, preview.RekeyMarkerName), []byte(summary+"\n"), 0o644); err != nil {
		return fmt.Errorf("writing the marker: %w", err)
	}
	_ = os.Remove(cursorPath)
	logger.Infof("[Thumbnails] thumbnail cache %s", summary)
	return nil
}

// thumbKeyPage reads the next page of a source, trying again for a while if the database is
// busy.
func thumbKeyPage(db *dbsql.IndexDB, source, after string) ([]dbsql.PreviewFile, error) {
	var err error
	for try := 0; try < thumbKeyMaxRetries; try++ {
		var page []dbsql.PreviewFile
		page, err = db.PreviewableFilesWithTypeAfter(source, after, thumbKeyPageSize)
		if err == nil {
			return page, nil
		}
		time.Sleep(thumbKeyRetryWait)
	}
	return nil, fmt.Errorf("reading the index of source %q after %q: %w", source, after, err)
}

// rekeyThumbKeyPage re-keys one page of files with thumbKeyWorkers workers and returns when
// all are done.
func rekeyThumbKeyPage(idx *indexing.Index, page []dbsql.PreviewFile, st *thumbKeyStats) {
	work := make(chan dbsql.PreviewFile)
	var wg sync.WaitGroup
	for w := 0; w < thumbKeyWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range work {
				rekeyThumbKeyFile(idx, f, st)
			}
		}()
	}
	for _, f := range page {
		work <- f
	}
	close(work)
	wg.Wait()
}

// rekeyThumbKeyFile re-keys one file's entries. Symlinks are skipped: their thumbnails were
// stored under the target's path, and the target is walked itself.
func rekeyThumbKeyFile(idx *indexing.Index, f dbsql.PreviewFile, st *thumbKeyStats) {
	st.files.Add(1)
	realPath := utils.JoinUnderSourceRoot(idx.Path, f.Path)
	fi, err := os.Lstat(realPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			st.skipped.Add(1) // gone since the index saw it
			return
		}
		thumbKeyFileError(st, f.Path, err)
		return
	}
	if !fi.Mode().IsRegular() {
		st.skipped.Add(1)
		return
	}
	res, err := preview.RekeyFile(realPath, fi, idx.DisplaySize(fi), strings.HasPrefix(f.Type, "video"))
	if err != nil {
		thumbKeyFileError(st, f.Path, err)
		return
	}
	st.moved.Add(int64(res.Moved))
	st.dropped.Add(int64(res.Dropped))
	st.freed.Add(res.Freed)
}

func thumbKeyFileError(st *thumbKeyStats, path string, err error) {
	if n := st.errors.Add(1); n <= thumbKeyMaxLogged {
		logger.Warningf("[Thumbnails] re-keying %s: %v", path, err)
		if n == thumbKeyMaxLogged {
			logger.Warningf("[Thumbnails] re-keying: further file errors are only counted")
		}
	}
}

func loadThumbKeyCursor(path string) thumbKeyCursor {
	var cur thumbKeyCursor
	data, err := os.ReadFile(path)
	if err != nil {
		return cur
	}
	if err := json.Unmarshal(data, &cur); err != nil {
		logger.Warningf("[Thumbnails] ignoring the unreadable re-keying place %s: %v", path, err)
		return thumbKeyCursor{}
	}
	return cur
}

// saveThumbKeyCursor writes the place to a temporary file and renames it over the old one, so
// a crash never leaves half a file.
func saveThumbKeyCursor(path string, cur thumbKeyCursor) error {
	data, err := json.Marshal(cur)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
