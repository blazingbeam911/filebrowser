/**
 * Per-folder display preferences (sort order, view mode) are stored under the folder's route
 * path. The same folder can be reached under different spellings of that path: with or
 * without a trailing slash (folder links and breadcrumbs add one, going up to the parent
 * folder doesn't), or with differently escaped characters (a typed URL may leave `&` or `+`
 * as they are). Each spelling used to get its own, separate preferences.
 *
 * displayPreferenceKey() gives every spelling of a path one key: each segment decoded and
 * re-encoded with encodeURIComponent (as the app builds its own links), empty segments
 * dropped, and exactly one trailing slash. Paths built by the app's links come out
 * unchanged, so preferences saved under them before keep working.
 *
 *   displayPreferenceKey("/files/src/a b")      -> "/files/src/a%20b/"
 *   displayPreferenceKey("/files/src/a%20b/")   -> "/files/src/a%20b/"
 *   displayPreferenceKey("/files/src/x&y+z")    -> "/files/src/x%26y%2Bz/"
 */
export function displayPreferenceKey(path) {
  if (typeof path !== "string" || path === "") {
    return null;
  }
  const segments = path.split("/").filter(Boolean).map(canonicalSegment);
  return segments.length ? `/${segments.join("/")}/` : "/";
}

/**
 * Keys to look a folder's preferences up under, best first: the canonical key, then the
 * path exactly as given and without its trailing slash (preferences saved under another
 * spelling before this normalization existed). Duplicates removed.
 */
export function displayPreferenceLookupKeys(path) {
  const key = displayPreferenceKey(path);
  if (key === null) {
    return [];
  }
  const keys = [key, path, path.replace(/\/+$/, "")];
  return keys.filter((k, i) => k !== "" && keys.indexOf(k) === i);
}

function canonicalSegment(segment) {
  try {
    return encodeURIComponent(decodeURIComponent(segment));
  } catch {
    return segment; // malformed escape: keep it as it is
  }
}
