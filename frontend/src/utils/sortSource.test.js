import { describe, expect, it } from "vitest";
import {
  countAllSorting,
  countSortingUnder,
  findInheritedSorting,
  folderNameOfKey,
  nextListingSortAsc,
  resolveSorting,
  sortDirectionLabel,
  sortSourceLabel,
  withoutAllSorting,
  withoutSortingUnder,
} from "@/utils/sortSource.js";

const by = (b, asc = true) => ({ by: b, asc });

describe("resolveSorting", () => {
  const all = {
    own: by("modified", false),
    inherited: { sorting: by("size"), key: "/files/s/a/" },
    global: by("duration"),
  };
  it("prefers the folder's own sort, as a rule when recursive", () => {
    expect(resolveSorting(all)).toEqual({ sorting: all.own, source: "folder", from: null });
    expect(resolveSorting({ ...all, ownRecursive: true }).source).toBe("tree");
  });
  it("then a parent's rule, global, default", () => {
    expect(resolveSorting({ ...all, own: undefined })).toEqual({ sorting: by("size"), source: "inherited", from: "/files/s/a/" });
    expect(resolveSorting({ ...all, own: null, inherited: null })).toEqual({ sorting: by("duration"), source: "global", from: null });
    expect(resolveSorting({})).toEqual({ sorting: by("name"), source: "default", from: null });
    expect(resolveSorting()).toEqual({ sorting: by("name"), source: "default", from: null });
  });
  it("ignores empty sort objects", () => {
    expect(resolveSorting({ own: {}, global: { by: "" } }).source).toBe("default");
  });
});

describe("nextListingSortAsc", () => {
  it("sorts another column ascending", () => {
    expect(nextListingSortAsc(by("name", false), "folder", "size")).toBe(true);
    expect(nextListingSortAsc(by("name", false), "global", "size")).toBe(true);
  });
  it("flips the sorted column only when the folder has its own sort", () => {
    expect(nextListingSortAsc(by("name", true), "folder", "name")).toBe(false);
    expect(nextListingSortAsc(by("name", false), "tree", "name")).toBe(true);
    for (const source of ["inherited", "global", "default"]) {
      expect(nextListingSortAsc(by("name", true), source, "name")).toBe(true);
      expect(nextListingSortAsc(by("size", false), source, "size")).toBe(false);
    }
  });
});

describe("findInheritedSorting", () => {
  const level = {
    "/files/s/": { sorting: by("size"), sortingRecursive: true },
    "/files/s/a/": { sorting: by("modified", false), sortingRecursive: true },
    "/files/s/a/b/": { sorting: by("kind") }, // own sort, not a rule
    "/files/s/x%20y": { sorting: by("duration"), sortingRecursive: true }, // old spelling
  };
  it("finds the nearest parent rule", () => {
    expect(findInheritedSorting(level, "/files/s/a/b/c/")).toEqual({ sorting: by("modified", false), key: "/files/s/a/" });
    expect(findInheritedSorting(level, "/files/s/a/b")).toEqual({ sorting: by("modified", false), key: "/files/s/a/" });
    expect(findInheritedSorting(level, "/files/s/other/")).toEqual({ sorting: by("size"), key: "/files/s/" });
  });
  it("doesn't count the folder's own rule", () => {
    expect(findInheritedSorting(level, "/files/s/a/")).toEqual({ sorting: by("size"), key: "/files/s/" });
    expect(findInheritedSorting(level, "/files/s/")).toBe(null);
  });
  it("matches a parent saved under another spelling", () => {
    expect(findInheritedSorting(level, "/files/s/x y/z/")).toEqual({ sorting: by("duration"), key: "/files/s/x%20y/" });
  });
  it("returns null without rules", () => {
    expect(findInheritedSorting({ "/a/": { sorting: by("size") } }, "/a/b/")).toBe(null);
    expect(findInheritedSorting(undefined, "/a/b/")).toBe(null);
  });
});

describe("withoutSortingUnder / countSortingUnder", () => {
  const level = {
    "/files/s/a/": { sorting: by("size"), sortingRecursive: true, viewMode: "gallery" },
    "/files/s/a": { sorting: by("name", false) }, // old spelling of the same folder
    "/files/s/a/b/": { sorting: by("size", false) },
    "/files/s/a/b/c": { sorting: by("kind"), viewMode: "list" },
    "/files/s/ab/": { sorting: by("modified") }, // a sibling, not under a/
    "/files/s/a/d/": { viewMode: "list" },
  };
  it("counts folders, each spelling once", () => {
    expect(countSortingUnder(level, "/files/s/a/")).toBe(3);
    expect(countSortingUnder(level, "/files/s/a", { includeSelf: false })).toBe(2);
    expect(countSortingUnder(level, "/files/s/a/", { includeDescendants: false })).toBe(1);
    expect(countSortingUnder(undefined, "/files/s/a/")).toBe(0);
  });
  it("clears a folder only, every spelling, keeping its view mode", () => {
    const { sourceLevel, removed } = withoutSortingUnder(level, "/files/s/a/", { includeDescendants: false });
    expect(removed).toBe(1);
    expect(sourceLevel["/files/s/a/"]).toEqual({ viewMode: "gallery" });
    expect(sourceLevel["/files/s/a"]).toBeUndefined();
    expect(sourceLevel["/files/s/a/b/"]).toEqual({ sorting: by("size", false) });
    expect(level["/files/s/a"]).toEqual({ sorting: by("name", false) }); // input untouched
  });
  it("clears a folder and everything under it, not its siblings", () => {
    const { sourceLevel, removed } = withoutSortingUnder(level, "/files/s/a/");
    expect(removed).toBe(3);
    expect(sourceLevel).toEqual({
      "/files/s/a/": { viewMode: "gallery" },
      "/files/s/a/b/c": { viewMode: "list" },
      "/files/s/ab/": { sorting: by("modified") },
      "/files/s/a/d/": { viewMode: "list" },
    });
  });
  it("clears only what's under it with includeSelf false", () => {
    const { sourceLevel, removed } = withoutSortingUnder(level, "/files/s/a/", { includeSelf: false });
    expect(removed).toBe(2);
    expect(sourceLevel["/files/s/a/"].sorting).toEqual(by("size"));
    expect(sourceLevel["/files/s/a/b/"]).toBeUndefined();
  });
});

describe("countAllSorting / withoutAllSorting", () => {
  const prefs = {
    src1: { "/files/src1/a/": { sorting: by("size"), viewMode: "list" }, "/files/src1/b/": { viewMode: "gallery" } },
    src2: { "/files/src2/": { sorting: by("kind"), sortingRecursive: true } },
  };
  it("counts and clears every source, keeping view modes", () => {
    expect(countAllSorting(prefs)).toBe(2);
    const { prefs: cleaned, removed } = withoutAllSorting(prefs);
    expect(removed).toBe(2);
    expect(cleaned).toEqual({
      src1: { "/files/src1/a/": { viewMode: "list" }, "/files/src1/b/": { viewMode: "gallery" } },
      src2: {},
    });
    expect(countAllSorting(undefined)).toBe(0);
  });
});

describe("labels", () => {
  const t = (key, params) => (params ? `${key}:${JSON.stringify(params)}` : key);
  it("names directions per column", () => {
    expect(sortDirectionLabel(t, "modified", false)).toBe("files.sortNewestFirst");
    expect(sortDirectionLabel(t, "created", true)).toBe("files.sortOldestFirst");
    expect(sortDirectionLabel(t, "size", false)).toBe("files.sortLargestFirst");
    expect(sortDirectionLabel(t, "duration", true)).toBe("files.sortShortestFirst");
    expect(sortDirectionLabel(t, "name", true)).toBe("files.sortAToZ");
    expect(sortDirectionLabel(t, "kind", false)).toBe("files.sortZToA");
  });
  it("names sources, inherited with the parent's name", () => {
    expect(sortSourceLabel(t, "folder")).toBe("files.sortSourceFolder");
    expect(sortSourceLabel(t, "tree")).toBe("files.sortSourceTree");
    expect(sortSourceLabel(t, "global")).toBe("files.sortSourceGlobal");
    expect(sortSourceLabel(t, "default")).toBe("files.sortSourceDefault");
    expect(sortSourceLabel(t, "inherited", "/files/s/%E5%86%99%E7%9C%9F%20a/")).toBe('files.sortSourceInherited:{"folder":"写真 a"}');
  });
  it("names a folder from its key", () => {
    expect(folderNameOfKey("/files/s/Photos/")).toBe("Photos");
    expect(folderNameOfKey("/")).toBe("/");
    expect(folderNameOfKey("/files/s/bad%E0/")).toBe("bad%E0");
  });
});
