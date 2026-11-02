import { reactive } from "vue";
import { setPreloadHold } from "@/utils/imagePreloader.js";

/**
 * Holding ← / → in the image viewer moves through the images one at a time, each as soon as
 * the current one is on screen:
 *
 * - plain hold: once the full image is shown (or failed to load);
 * - fast hold (Shift held too): once its thumbnail is shown. The viewer then shows thumbnails
 *   instead of downloading every full image, and the preloader pauses, until the hold ends.
 *
 * nextPrevious.vue drives the hold from the keyboard; ExtendedImage.vue reports what it shows
 * for which URL (the URL ties the report to an image, so a report about the previous image
 * can never let the hold move on).
 */
export const imageHold = reactive({
  /** An arrow key is being held (auto-repeat seen) while moving through images. */
  active: false,
  /** Shift is held during the hold: thumbnails are enough. */
  fast: false,
  /** URL of the image the viewer reported on, and what of it is on screen. */
  shownSrc: null,
  /** "none" | "thumbnail" | "full" */
  shown: "none",
});

/** Called on each auto-repeated arrow keydown on an image. */
export function startHold(fast) {
  imageHold.active = true;
  setHoldFast(fast);
}

export function setHoldFast(fast) {
  const value = imageHold.active && Boolean(fast);
  if (imageHold.fast === value) return;
  imageHold.fast = value;
  setPreloadHold(value);
}

/** The arrow was released, or the window lost focus. */
export function endHold() {
  setHoldFast(false);
  imageHold.active = false;
}

/** Viewer report: for `src`, `shown` ("none" | "thumbnail" | "full") is on screen. */
export function reportShown(src, shown) {
  if (shown !== "full" && imageHold.shownSrc === src && imageHold.shown === "full") return;
  imageHold.shownSrc = src;
  imageHold.shown = shown;
}

/** True when a held arrow may move on from the image at `src`. */
export function holdMayMove(src) {
  if (!src || imageHold.shownSrc !== src) return false;
  if (imageHold.shown === "full") return true;
  return imageHold.fast && imageHold.shown === "thumbnail";
}
