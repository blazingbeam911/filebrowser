/**
 * Going up to a parent folder (at any distance) selects the item you came out of, so a long
 * listing doesn't start again from the top. Files.vue calls this when a folder listing loads,
 * with the listing that was on screen before it.
 *
 * Returns the full path of the child of `newPath` that leads down to `previousPath`, or null
 * when `newPath` is not a strict ancestor of `previousPath` (going down, sideways, reloading
 * the same folder, or a different source).
 *
 *   upFolderHighlightPath("src", "/a/", "src", "/a/b/c/")   -> "/a/b"
 *   upFolderHighlightPath("src", "/a",  "src", "/a/b.jpg")  -> "/a/b.jpg"
 *   upFolderHighlightPath("src", "/a/b", "src", "/a")       -> null
 */
export function upFolderHighlightPath(newSource, newPath, previousSource, previousPath) {
  if (!newSource || newSource !== previousSource) return null;
  if (typeof newPath !== "string" || typeof previousPath !== "string") return null;
  const up = splitPath(newPath);
  const from = splitPath(previousPath);
  if (from.length <= up.length) return null;
  for (let i = 0; i < up.length; i++) {
    if (up[i] !== from[i]) return null;
  }
  return `/${from.slice(0, up.length + 1).join("/")}`;
}

function splitPath(path) {
  return path.split("/").filter(Boolean);
}
