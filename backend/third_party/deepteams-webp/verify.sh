#!/usr/bin/env bash
# Check this trimmed copy against the published deepteams/webp module.
# Usage: ./verify.sh            compare with fufu.diff (exit 1 on any difference)
#        ./verify.sh --write    regenerate fufu.diff (review it before committing)
set -euo pipefail
VERSION=v1.2.8
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TMP="$(mktemp -d)"
trap 'chmod -R u+w "$TMP" 2>/dev/null; rm -rf "$TMP"' EXIT

# go mod download (outside any module) verifies the zip against sum.golang.org.
SRC="$(cd "$TMP" && GOPATH="$TMP/gopath" GOFLAGS=-modcacherw GONOSUMDB= GOPRIVATE= \
  go mod download -json "github.com/deepteams/webp@$VERSION" | python3 -c 'import json,sys; print(json.load(sys.stdin)["Dir"])')"

# Upstream files that are deliberately not copied.
SKIP='^(\.claude/|\.github/|\.gitignore$|testdata/|testc/|cmd/|CLAUDE\.md$|.*_test\.go$)'

mkdir -p "$TMP/a" "$TMP/b"
(cd "$SRC" && find . -type f | sed 's|^\./||' | grep -Ev "$SKIP" | while read -r f; do
  mkdir -p "$TMP/a/$(dirname "$f")"; cp "$f" "$TMP/a/$f"; done)
(cd "$HERE" && find . -type f | sed 's|^\./||' | grep -Ev '^(FUFU\.md|verify\.sh|fufu\.diff)$' | while read -r f; do
  mkdir -p "$TMP/b/$(dirname "$f")"; cp "$f" "$TMP/b/$f"; done)

ACTUAL="$(cd "$TMP" && { diff -ruN a b || true; } | sed -E 's/^(---|\+\+\+) ([^\t]+)\t.*/\1 \2/')"
if [ "${1:-}" = "--write" ]; then
  printf '%s\n' "$ACTUAL" > "$HERE/fufu.diff"
  echo "wrote fufu.diff ($(printf '%s\n' "$ACTUAL" | wc -l) lines); review it"
  exit 0
fi
if [ "$ACTUAL" = "$(cat "$HERE/fufu.diff")" ]; then
  echo "OK: matches deepteams/webp $VERSION except for fufu.diff"
else
  echo "MISMATCH: differences from deepteams/webp $VERSION are not exactly fufu.diff" >&2
  diff <(printf '%s\n' "$ACTUAL") "$HERE/fufu.diff" | head -40 >&2
  exit 1
fi
