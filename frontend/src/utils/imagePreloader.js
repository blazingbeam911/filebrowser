import { getters } from "@/store/index.ts";
import { imageViewerConfig, isPreloadEnabled } from "@/utils/imageViewerConfig.js";
import { getTypeInfo } from "@/utils/mimetype.js";
import { isHeicOrHeif, isServerConvertedImage, previewRawUrl } from "@/utils/previewRawUrl.js";
import { joinPath } from "@/utils/url.js";

/**
 * Preloads (downloads and decodes) the images next to the one open in the image viewer, so
 * moving to them can swap instantly.
 *
 * - Which images: up to `preloadAhead` in the direction the user is moving through the folder
 *   and `preloadBehind` the other way (utils/imageViewerConfig.js, per device). Only images
 *   the browser shows from the file itself count; server-converted ones (RAW, HEIC, MuPDF
 *   documents), TIFF (decoded to a canvas by the viewer) and files over `preloadMaxMB` are
 *   skipped.
 * - When: one at a time, nearest first, and only while the viewer isn't loading its own
 *   image (the viewer pauses and resumes the preloader), and not during a fast hold
 *   (utils/imageHold.js), which wants the bandwidth for thumbnails.
 * - Memory: images that leave the window are released (and their download aborted).
 */

const MB = 1024 * 1024;
/** Longest the preloader waits for the viewer to show a new image before carrying on anyway. */
const VIEWER_WAIT_MS = 3000;
const TIFF_NAME = /\.tiff?$/i;

/** True if `item` is an image the viewer loads straight from the file and may be preloaded. */
export function isPreloadableImage(item, maxMB) {
  if (!item || item.type === "directory") return false;
  if (getTypeInfo(item.type).simpleType !== "image") return false;
  if (isHeicOrHeif(item.type) || item.type === "image/tiff" || TIFF_NAME.test(item.name || "")) return false;
  if (isServerConvertedImage(item)) return false;
  if (typeof item.size === "number" && item.size > maxMB * MB) return false;
  if (getters.fileViewingDisabled(item.name)) return false;
  return true;
}

/**
 * Direction of travel after moving to `index` in the folder identified by `key`.
 * A new folder starts as "next"; staying at the same index keeps the last direction.
 * @param {{ key: string, index: number, direction: string } | null} previous
 */
export function travelDirection(previous, key, index) {
  if (!previous || previous.key !== key) return "next";
  if (index > previous.index) return "next";
  if (index < previous.index) return "previous";
  return previous.direction;
}

/**
 * Listing indexes to preload, in load order: the nearest image ahead, the nearest behind,
 * then the rest ahead, then the rest behind.
 * @param {unknown[]} items listing in navigation order
 * @param {number} currentIndex
 * @param {"next"|"previous"} direction
 * @param {{ ahead: number, behind: number }} counts
 * @param {(item: unknown) => boolean} accept
 * @returns {number[]}
 */
export function computePreloadOrder(items, currentIndex, direction, { ahead, behind }, accept) {
  const step = direction === "previous" ? -1 : 1;
  const collect = (delta, count) => {
    const found = [];
    for (let i = currentIndex + delta; found.length < count && i >= 0 && i < items.length; i += delta) {
      if (accept(items.at(i))) found.push(i);
    }
    return found;
  };
  const forward = collect(step, ahead);
  const backward = collect(-step, behind);
  return [...forward.slice(0, 1), ...backward.slice(0, 1), ...forward.slice(1), ...backward.slice(1)];
}

function decodeImage(img) {
  if (typeof img.decode === "function") {
    return img.decode();
  }
  return new Promise((resolve, reject) => {
    img.onload = resolve;
    img.onerror = reject;
  });
}

/** url -> { img: HTMLImageElement, ready: boolean } */
const entries = new Map();
/** URLs in the window that haven't started loading, in load order. */
let queue = [];
let loadingUrl = null;
let paused = true;
/** A fast hold is in progress (utils/imageHold.js): no new preloads until it ends. */
let held = false;
/** The viewer's own image URL; never aborted, since the viewer may share its download. */
let viewerUrl = null;
/** The last image the viewer finished showing. */
let viewerShownUrl = null;
let viewerWaitTimer = null;
let position = null;

function clearViewerWait() {
  if (viewerWaitTimer !== null) {
    clearTimeout(viewerWaitTimer);
    viewerWaitTimer = null;
  }
}

/**
 * Holds preloading until the viewer has shown the image at the new position, so a preload
 * (for instance of the image just left) can't hold up the decode of the one being opened.
 * The timer is a safety net in case the viewer never reports back.
 */
function waitForViewer() {
  paused = true;
  clearViewerWait();
  viewerWaitTimer = setTimeout(() => {
    viewerWaitTimer = null;
    paused = false;
    pump();
  }, VIEWER_WAIT_MS);
}

function release(url) {
  const entry = entries.get(url);
  if (!entry) return;
  entries.delete(url);
  if (url !== viewerUrl) {
    entry.img.removeAttribute("src");
  }
  if (loadingUrl === url) {
    loadingUrl = null;
  }
}

function pump() {
  if (paused || held || loadingUrl !== null) return;
  const url = queue.shift();
  if (url === undefined) return;
  const img = new Image();
  img.decoding = "async";
  const entry = { img, ready: false };
  entries.set(url, entry);
  loadingUrl = url;
  img.src = url;
  decodeImage(img)
    .then(() => {
      if (entries.get(url) === entry) entry.ready = true;
    })
    .catch(() => {
      if (entries.get(url) === entry) entries.delete(url);
    })
    .finally(() => {
      if (loadingUrl === url) loadingUrl = null;
      pump();
    });
}

/**
 * Replaces the preload window with `urls` (load order). Keeps what's still wanted, plus
 * `keepUrl` (the image being opened, so the viewer can still find it preloaded).
 */
export function setPreloadWindow(urls, keepUrl = null) {
  const wanted = new Set(urls);
  if (keepUrl) wanted.add(keepUrl);
  for (const url of [...entries.keys()]) {
    if (!wanted.has(url)) release(url);
  }
  queue = urls.filter((url) => !entries.has(url));
  pump();
}

/**
 * Recomputes the window after the viewer moved to `currentIndex` of `items` (the listing in
 * navigation order, as built by mutations.setupNavigation).
 */
export function refreshImagePreloads({ items, currentIndex, directoryPath, source }) {
  if (!isPreloadEnabled() || !Array.isArray(items) || currentIndex < 0) {
    clearImagePreloads();
    return;
  }
  const key = `${source ?? ""}|${directoryPath}`;
  const direction = travelDirection(position, key, currentIndex);
  position = { key, index: currentIndex, direction };

  const maxMB = imageViewerConfig.preloadMaxMB;
  const order = computePreloadOrder(
    items,
    currentIndex,
    direction,
    { ahead: imageViewerConfig.preloadAhead, behind: imageViewerConfig.preloadBehind },
    (item) => isPreloadableImage(item, maxMB),
  );
  const urlAt = (index) => {
    const item = items.at(index);
    return previewRawUrl({ ...item, source: item.source ?? source, path: joinPath(directoryPath, item.name) });
  };
  const currentUrl = urlAt(currentIndex);
  if (currentUrl && currentUrl !== viewerShownUrl) {
    waitForViewer();
  }
  const urls = [];
  for (const index of order) {
    const url = urlAt(index);
    if (url) urls.push(url);
  }
  setPreloadWindow(urls, currentUrl);
}

/** Called by the viewer when it starts loading its own image: hold off new preloads. */
export function pauseImagePreloads(url = null) {
  clearViewerWait();
  paused = true;
  viewerUrl = url;
}

/** Called by the viewer once its own image has loaded (or failed): preloading may continue. */
export function resumeImagePreloads() {
  clearViewerWait();
  viewerShownUrl = viewerUrl;
  paused = false;
  pump();
}

/**
 * Fast hold on/off (utils/imageHold.js). Independent of the viewer's pause: preloading
 * continues only once both allow it. A preload already downloading is left to finish.
 */
export function setPreloadHold(value) {
  held = Boolean(value);
  pump();
}

/** True if `url` has been downloaded and decoded by the preloader. */
export function isImagePreloaded(url) {
  return entries.get(url)?.ready === true;
}

/** Drops every preloaded image and aborts any download in progress. */
export function clearImagePreloads() {
  for (const url of [...entries.keys()]) release(url);
  queue = [];
  loadingUrl = null;
  clearViewerWait();
  paused = true;
  viewerUrl = null;
  viewerShownUrl = null;
  position = null;
}
