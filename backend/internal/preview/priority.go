package preview

// fufu: lets the thumbnail job (internal/thumbjob) give way to browsing. Every preview that a
// browse request has to make goes through GeneratePreviewWithMD5, which marks it as foreground
// work for as long as it waits for and holds an image slot. The job's own work carries a
// Background context, so it doesn't count. BrowsingBusy is true while any foreground preview
// is being made and for ForegroundGrace after the last one ends; the job starts no new file
// meanwhile. Cache hits never reach GeneratePreviewWithMD5, so browsing a folder whose
// thumbnails are all made doesn't pause the job.

import (
	"context"
	"sync/atomic"
	"time"
)

// ForegroundGrace is how long the job keeps waiting after the last foreground preview ended.
// Scrolling sends preview requests in bursts; this keeps the job from slipping in between them.
var ForegroundGrace = 3 * time.Second

type backgroundKey struct{}

var (
	foregroundActive  atomic.Int64
	foregroundLastEnd atomic.Int64 // UnixNano
)

// Background marks ctx as background (thumbnail job) work.
func Background(ctx context.Context) context.Context {
	return context.WithValue(ctx, backgroundKey{}, true)
}

func isBackground(ctx context.Context) bool {
	b, _ := ctx.Value(backgroundKey{}).(bool)
	return b
}

// foreground counts a preview being made for a browse request until the returned func runs.
func foreground(ctx context.Context) func() {
	if isBackground(ctx) {
		return func() {}
	}
	foregroundActive.Add(1)
	return func() {
		// The end time first, so a reader that sees no active preview also sees when it ended.
		foregroundLastEnd.Store(time.Now().UnixNano())
		foregroundActive.Add(-1)
	}
}

// BrowsingBusy reports whether browse requests are making previews now or did within
// ForegroundGrace.
func BrowsingBusy() bool {
	if foregroundActive.Load() > 0 {
		return true
	}
	last := foregroundLastEnd.Load()
	return last != 0 && time.Since(time.Unix(0, last)) < ForegroundGrace
}
