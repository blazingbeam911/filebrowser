import { reactive } from "vue";

/**
 * Full-size viewing of PSDs (fufu). The viewer shows a PSD as the server's "original" preview,
 * fitted in PSD_VIEW_CAP px. When that image came back exactly PSD_VIEW_CAP px on its long side,
 * it was (almost always) shrunk, and the top bar offers a button that swaps in the full-size
 * image (`size=full`) in place, keeping zoom and position. It applies to one file: the next file
 * opens capped again.
 *
 * Shared between ExtendedImage (which loads and swaps the image) and the top bar (the button).
 */

/** Long side of the server's "original" PSD preview (backend psdOriginalSide). */
export const PSD_VIEW_CAP = 4096;

export const psdFullSize = reactive({
  /** The capped image's URL this state belongs to; null when no PSD is shown. */
  src: null,
  /** The capped image was shrunk: the button can be offered. */
  available: false,
  /** The full-size image is being made / downloaded. */
  loading: false,
  /** The full-size image is on screen. */
  on: false,
  /** Bumped by each press of the button; ExtendedImage watches it. */
  requests: 0,
});

/** Forget any full-size state (a new file is being shown). */
export function resetPsdFullSize(src = null) {
  psdFullSize.src = src;
  psdFullSize.available = false;
  psdFullSize.loading = false;
  psdFullSize.on = false;
}

/** Called when a PSD's capped image has loaded with its natural size. */
export function notePsdCappedImage(src, naturalWidth, naturalHeight) {
  if (psdFullSize.src !== src || psdFullSize.on) return;
  psdFullSize.available = Math.max(naturalWidth, naturalHeight) === PSD_VIEW_CAP;
}

/** True when the top bar should show the full-size button. */
export function psdFullSizeOffered() {
  return psdFullSize.available && !psdFullSize.on;
}

/** The button was pressed. */
export function requestPsdFullSize() {
  if (!psdFullSizeOffered() || psdFullSize.loading) return;
  psdFullSize.loading = true;
  psdFullSize.requests++;
}

/** The full-size image for `src` is on screen (ok) or failed. */
export function finishPsdFullSize(src, ok) {
  if (psdFullSize.src !== src) return;
  psdFullSize.loading = false;
  if (ok) psdFullSize.on = true;
}
