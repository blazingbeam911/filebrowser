# gen2brain/webp v0.6.4, trimmed for this fork

A copy of [github.com/gen2brain/webp](https://github.com/gen2brain/webp) v0.6.4
(MIT; libwebp v1.6.0, BSD-3-Clause, see `lib/LICENSE.libwebp`), used through a
`replace` in `backend/go.mod` to encode thumbnails as WebP. Changes from upstream:

- `webp.go`: the `init()` that called `image.RegisterFormat("webp", ...)` is removed.
  Upstream registers itself as the process-wide WebP decoder, which takes precedence
  over `golang.org/x/image/webp` (registered by `kovidgoyal/imaging`). For lossless
  WebP sources that decoder returns 4:2:0 YCbCr instead of exact RGBA. Only the
  encoder is used here; decoding stays with `x/image/webp`.
- The system-libwebp loader is removed (`webp_dynamic.go`, `purego_darwin.go`,
  `purego_unix.go`, `purego_windows.go`); `purego_other.go`, upstream's "dynamic
  disabled" stub, is built unconditionally, and `go.mod` drops `ebitengine/purego`.
  The package therefore always uses its built-in, wasm2go-generated libwebp.
- Not copied: tests, `testdata/`, `.github/`, `lib/webp.wasm`.

`verify.sh` downloads the original module through the Go module proxy (checked
against the public checksum database) and confirms that the differences are
exactly `fufu.diff`. To update: copy the new version's files, redo the changes
above, regenerate `fufu.diff` (`./verify.sh --write`), review it, and run
`./verify.sh`.
