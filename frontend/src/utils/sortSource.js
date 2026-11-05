/**
 * Where a folder listing's sort comes from (fufu).
 *
 * Sources, first match wins:
 *   "folder"    the folder's own saved sort (per-folder display preference);
 *   "tree"      the folder's own sort, saved as a rule for its subfolders too
 *               (`sortingRecursive: true`, "Apply recursively");
 *   "inherited" the nearest parent folder's rule, for a folder without its own sort;
 *   "global"    the global sort of this browser ("Apply globally");
 *   "default"   name ascending.
 */
import { displayPreferenceKey, displayPreferenceLookupKeys } from "@/utils/displayPreferenceKey.js";

export const SORT_SOURCES = ["folder", "tree", "inherited", "global", "default"];

/** Sources that are the folder's own saved sort: a click on the sorted column flips them. */
export const OWN_SORT_SOURCES = ["folder", "tree"];

const NAME_ASCENDING = { by: "name", asc: true };

export function isSorting(value) {
  return Boolean(value && typeof value.by === "string" && value.by !== "");
}

/**
 * The sort to use, its source and, for "inherited", the key of the folder it comes from:
 * `{ sorting, source, from }`.
 */
export function resolveSorting({ own, ownRecursive, inherited, global } = {}) {
  if (isSorting(own)) {
    return { sorting: own, source: ownRecursive ? "tree" : "folder", from: null };
  }
  if (inherited && isSorting(inherited.sorting)) {
    return { sorting: inherited.sorting, source: "inherited", from: inherited.key };
  }
  if (isSorting(global)) {
    return { sorting: global, source: "global", from: null };
  }
  return { sorting: NAME_ASCENDING, source: "default", from: null };
}

/**
 * The direction a click on `field` gives in the main listing: another column sorts
 * ascending; the sorted column flips when the folder has its own sort, and otherwise keeps
 * the shown direction (the click only saves it for the folder).
 */
export function nextListingSortAsc(current, source, field) {
  if (!current || current.by !== field) return true;
  return OWN_SORT_SOURCES.includes(source) ? !current.asc : current.asc;
}

function isEntry(value) {
  return Boolean(value && typeof value === "object");
}

/** The folder's entry, looked up under every spelling of its path (best first). */
export function folderEntry(sourceLevel, path) {
  if (!isEntry(sourceLevel)) return null;
  for (const key of displayPreferenceLookupKeys(path)) {
    if (isEntry(sourceLevel[key])) return sourceLevel[key];
  }
  return null;
}

/** Canonical keys of the folder's parents, nearest first, up to "/". */
function parentKeys(path) {
  const key = displayPreferenceKey(path);
  if (key === null) return [];
  const segments = key.split("/").filter(Boolean);
  const keys = [];
  for (let i = segments.length - 1; i >= 0; i--) {
    keys.push(i === 0 ? "/" : `/${segments.slice(0, i).join("/")}/`);
  }
  return keys;
}

/**
 * The nearest parent folder's rule (`sortingRecursive` with a sort) for `path`:
 * `{ sorting, key }` (canonical key of that parent), or null. Any spelling of a parent's
 * path counts.
 */
export function findInheritedSorting(sourceLevel, path) {
  if (!isEntry(sourceLevel)) return null;
  const rules = new Map();
  for (const [rawKey, entry] of Object.entries(sourceLevel)) {
    if (!isEntry(entry) || entry.sortingRecursive !== true || !isSorting(entry.sorting)) continue;
    const key = displayPreferenceKey(rawKey);
    // The canonical spelling wins over an old one.
    if (key !== null && (!rules.has(key) || rawKey === key)) rules.set(key, entry.sorting);
  }
  if (rules.size === 0) return null;
  for (const key of parentKeys(path)) {
    if (rules.has(key)) return { sorting: rules.get(key), key };
  }
  return null;
}

function matchesScope(rawKey, base, includeSelf, includeDescendants) {
  const key = displayPreferenceKey(rawKey);
  if (key === null) return false;
  if (key === base) return includeSelf;
  return includeDescendants && key.startsWith(base);
}

/**
 * A source's preferences without the sorts (and rules) saved for `path` (`includeSelf`)
 * and/or every folder under it (`includeDescendants`), under any spelling. Other
 * preferences (view mode) stay; an entry left empty is dropped. Returns
 * `{ sourceLevel, removed }`, `removed` = folders whose sort was removed. The input isn't
 * modified.
 */
export function withoutSortingUnder(sourceLevel, path, { includeSelf = true, includeDescendants = true } = {}) {
  const result = { ...(isEntry(sourceLevel) ? sourceLevel : {}) };
  const base = displayPreferenceKey(path);
  const removedFolders = new Set();
  if (base === null) return { sourceLevel: result, removed: 0 };
  for (const [rawKey, entry] of Object.entries(result)) {
    if (!isEntry(entry) || !matchesScope(rawKey, base, includeSelf, includeDescendants)) continue;
    if (!Object.hasOwn(entry, "sorting") && !Object.hasOwn(entry, "sortingRecursive")) continue;
    const { sorting, sortingRecursive: _rule, ...rest } = entry;
    if (isSorting(sorting)) removedFolders.add(displayPreferenceKey(rawKey));
    if (Object.keys(rest).length === 0) {
      delete result[rawKey];
    } else {
      result[rawKey] = rest;
    }
  }
  return { sourceLevel: result, removed: removedFolders.size };
}

/** Folders with a saved sort at `path` (`includeSelf`) and/or under it, any spelling counted once. */
export function countSortingUnder(sourceLevel, path, { includeSelf = true, includeDescendants = true } = {}) {
  if (!isEntry(sourceLevel)) return 0;
  const base = displayPreferenceKey(path);
  if (base === null) return 0;
  const folders = new Set();
  for (const [rawKey, entry] of Object.entries(sourceLevel)) {
    if (isEntry(entry) && isSorting(entry.sorting) && matchesScope(rawKey, base, includeSelf, includeDescendants)) {
      folders.add(displayPreferenceKey(rawKey));
    }
  }
  return folders.size;
}

/** Folders with a saved sort in every source of `prefs` (`{ [source]: { [path]: entry } }`). */
export function countAllSorting(prefs) {
  if (!isEntry(prefs)) return 0;
  return Object.values(prefs).reduce((sum, level) => sum + countSortingUnder(level, "/"), 0);
}

/** `prefs` without any saved sort or rule, in every source: `{ prefs, removed }`. */
export function withoutAllSorting(prefs) {
  const result = {};
  let removed = 0;
  for (const [source, level] of Object.entries(isEntry(prefs) ? prefs : {})) {
    const cleaned = withoutSortingUnder(level, "/");
    removed += cleaned.removed;
    result[source] = cleaned.sourceLevel;
  }
  return { prefs: result, removed };
}

/** A sort's direction in words, e.g. "Newest first". `t` is the i18n translate function. */
export function sortDirectionLabel(t, by, asc) {
  switch (by) {
    case "size":
      return asc ? t("files.sortSmallestFirst") : t("files.sortLargestFirst");
    case "modified":
    case "created":
      return asc ? t("files.sortOldestFirst") : t("files.sortNewestFirst");
    case "duration":
      return asc ? t("files.sortShortestFirst") : t("files.sortLongestFirst");
    default: // name, kind
      return asc ? t("files.sortAToZ") : t("files.sortZToA");
  }
}

/** The last segment of a folder key, decoded: the folder's name. */
export function folderNameOfKey(key) {
  const segments = String(key ?? "").split("/").filter(Boolean);
  const last = segments[segments.length - 1] ?? "/";
  try {
    return decodeURIComponent(last);
  } catch {
    return last;
  }
}

/** A sort source in words, e.g. "this folder", "inherited from Photos". `t` is the i18n translate function. */
export function sortSourceLabel(t, source, from = null) {
  switch (source) {
    case "folder":
      return t("files.sortSourceFolder");
    case "tree":
      return t("files.sortSourceTree");
    case "inherited":
      return t("files.sortSourceInherited", { folder: folderNameOfKey(from) });
    case "global":
      return t("files.sortSourceGlobal");
    default:
      return t("files.sortSourceDefault");
  }
}
