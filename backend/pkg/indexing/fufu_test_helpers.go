package indexing

// SetTestIndexWithDB is SetTestIndex with the index writing to the shared index database (set
// it first with SetIndexDBForTesting), for tests that refresh folders into the index. fufu:
// the thumbnail job's refresh tests (internal/web/thumbjob_test.go).
func SetTestIndexWithDB(name string, path string) {
	SetTestIndex(name, path)
	indexesMutex.Lock()
	defer indexesMutex.Unlock()
	indexes[name].db = indexDB
}
