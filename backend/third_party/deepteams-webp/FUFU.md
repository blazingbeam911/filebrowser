# deepteams/webp v1.2.8, trimmed for this fork

A copy of [github.com/deepteams/webp](https://github.com/deepteams/webp) v1.2.8 (MIT; derived
from libwebp, BSD-3-Clause, see `LICENSE`), used through a `replace` in `backend/go.mod` as
the process-wide WebP decoder. Importing it registers its decoder with `image.RegisterFormat`,
ahead of `golang.org/x/image/webp` (registered by `kovidgoyal/imaging`), so `image.Decode`,
and with it `imaging.Decode` for still WebP, uses it.

Why: on 4K lossless WebP it decodes about 2.5x faster than `x/image/webp` (93 vs 229 ms
per image), bit-identically. On lossy WebP it matches libwebp's own output, where
`x/image/webp` returns 4:2:0 YCbCr that Go's generic conversion upsamples visibly
differently. It includes hand-written SSE2/AVX2 (amd64) and NEON (arm64) assembly.

Changes from upstream:
- `webp.go` `init()` registers `safeDecode`/`safeDecodeConfig` (new `fufu_safe.go`) instead of
  `Decode`/`DecodeConfig`: the same functions, with any panic turned into an error.
- Not copied: tests, `testdata/`, `testc/` (cgo comparison harness), `cmd/`, and the author's
  development files (`.claude/`, `CLAUDE.md`, `.github/`, `.gitignore`).

`verify.sh` downloads the original module through the Go module proxy (checked against the
public checksum database) and confirms the differences are exactly `fufu.diff`. To update:
copy the new version's files, redo the change above, regenerate `fufu.diff`
(`./verify.sh --write`), review it, and run `./verify.sh`.
