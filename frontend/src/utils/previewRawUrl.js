import { resourcesApi } from "@/api/index.js";
import { getters, state } from "@/store/index.ts";
import { globalVars } from "@/utils/constants.js";
import { isRawImageMimeType } from "@/utils/mimetype.js";
import { removeTrailingSlash } from "@/utils/url.js";

/**
 * URL the file previewer loads for a file: images, server-converted documents (MuPDF, RAW,
 * HEIC) and, through getViewURL, the media stream for audio and video.
 *
 * Shared by the previewer (for the open file) and the image preloader (for its neighbours),
 * so a preloaded image has exactly the URL the previewer will later ask for and the
 * browser can reuse it instead of downloading it again.
 */

const PDF_CONVERTABLE_EXTENSIONS = new Set([
  ".xps", ".mobi", ".fb2", ".cbz", ".svg", ".docx", ".pptx", ".xlsx", ".hwp", ".hwpx",
]);

/** True for documents the server renders to an image with MuPDF. */
export function isPdfConvertable(name) {
  if (!globalVars.muPdfAvailable || typeof name !== "string") {
    return false;
  }
  const ext = `.${name.split(".").pop().toLowerCase()}`;
  if (state.user?.disableViewingExt?.includes(ext)) {
    return false;
  }
  return PDF_CONVERTABLE_EXTENSIONS.has(ext);
}

/** Type FileBrowser gives Photoshop files (.psd, .psb). */
export const PSD_MIME_TYPE = "image/vnd.adobe.photoshop";
const PSD_NAME = /\.ps[db]$/i;

/**
 * True for Photoshop files (fufu). No browser can show them; the server makes a WebP of the
 * file's flattened image (backend internal/preview/psd.go).
 */
export function isPsd(item) {
  if (!item) return false;
  return item.type === PSD_MIME_TYPE || PSD_NAME.test(item.name || "");
}

export function isHeicOrHeif(type) {
  return type === "image/heic" || type === "image/heif";
}

/**
 * True when the image comes from a server-side conversion (RAW, HEIC/HEIF converted for
 * non-Safari browsers, MuPDF documents, PSD) rather than the file's own bytes.
 */
export function isServerConvertedImage(item) {
  if (!item) return false;
  if (isPsd(item)) return true;
  if (state.isSafari && isHeicOrHeif(item.type)) {
    return false;
  }
  const heicConverted = isHeicOrHeif(item.type) && globalVars.mediaAvailable && globalVars.enableHeicConversion;
  return isPdfConvertable(item.name) || isRawImageMimeType(item.type) || Boolean(heicConverted);
}

/**
 * @param {{ source?: string, path: string, name?: string, type?: string,
 *           modified?: string, viewToken?: string }} item
 * @returns {string|null}
 */
export function previewRawUrl(item) {
  const viewToken = item.viewToken;
  const typeHint = item.type || item.name;
  const share = getters.isShare()
    ? { path: state.shareInfo.subPath, hash: state.shareInfo.hash }
    : null;

  if (isServerConvertedImage(item)) {
    if (share) {
      return resourcesApi.getPreviewURLPublic(removeTrailingSlash(item.path), "original", viewToken);
    }
    return `${resourcesApi.getPreviewURL(item.source, item.path, item.modified)}&size=original`;
  }
  return resourcesApi.getViewURL(item.source, item.path, viewToken, share, false, typeHint);
}

/**
 * URL of a PSD's full-size image (`size=full`, PSD only): the viewer first shows `original`,
 * which the server caps at 4096 px, and swaps this in on request.
 * @param {{ source?: string, path: string, modified?: string, viewToken?: string }} item
 * @returns {string}
 */
export function psdFullUrl(item) {
  if (getters.isShare()) {
    return resourcesApi.getPreviewURLPublic(removeTrailingSlash(item.path), "full", item.viewToken);
  }
  return `${resourcesApi.getPreviewURL(item.source, item.path, item.modified)}&size=full`;
}

/**
 * URL of the server's large (640 px) thumbnail of `item`, built exactly as the listing builds
 * it (ListingItem.vue thumbnailUrl + Icon.vue "&size=large"), so a thumbnail the listing has
 * already loaded is reused. Null when thumbnails are off or the file has none.
 * @param {{ source?: string, path: string, modified?: string, hasPreview?: boolean }} item
 * @returns {string|null}
 */
export function largeThumbnailUrl(item) {
  if (!item?.path || !item.hasPreview || !globalVars.enableThumbs || !getters.previewPerms()?.image) {
    return null;
  }
  if (getters.isShare()) {
    if (state.shareInfo?.disableThumbnails) {
      return null;
    }
    return `${resourcesApi.getPreviewURLPublic(item.path)}&size=large`;
  }
  if (!item.source) {
    return null;
  }
  return `${resourcesApi.getPreviewURL(item.source, item.path, item.modified)}&size=large`;
}
