package sql

import (
	"database/sql"

	"github.com/gtsteffaniak/filebrowser/backend/internal/database/sqlitebusy"
)

// fufu: file lists for the thumbnail job (internal/thumbjob). Both queries are range scans of
// the primary key (source, path), limited to one folder.

// CountPreviewableFiles counts the files under dirPrefix (an index folder path ending in "/",
// or "/" for the whole source) that have a preview.
func (db *IndexDB) CountPreviewableFiles(source, dirPrefix string) (int, error) {
	var n sql.NullInt64
	err := db.QueryRow(`
	SELECT COUNT(*) FROM index_items
	WHERE source = ? AND path > ? AND path < ? AND is_dir = 0 AND has_preview = 1
	`, source, dirPrefix, getNextPathPrefix(dirPrefix)).Scan(&n)
	if err != nil {
		return 0, sqlitebusy.Wrap(err)
	}
	return int(n.Int64), nil
}

// PreviewableFilesAfter returns, in path order, up to limit paths of files under dirPrefix
// that have a preview and sort after `after` (pass dirPrefix itself for the first page).
// Paging by the last path keeps each read short, so a long job never holds the database open.
func (db *IndexDB) PreviewableFilesAfter(source, dirPrefix, after string, limit int) ([]string, error) {
	if after < dirPrefix {
		after = dirPrefix
	}
	rows, err := db.Query(`
	SELECT path FROM index_items
	WHERE source = ? AND path > ? AND path < ? AND is_dir = 0 AND has_preview = 1
	ORDER BY path LIMIT ?
	`, source, after, getNextPathPrefix(dirPrefix), limit)
	if err != nil {
		return nil, sqlitebusy.Wrap(err)
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}
