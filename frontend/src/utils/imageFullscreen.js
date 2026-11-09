import { reactive } from 'vue';

/**
 * Fullscreen for the image viewer. The whole page is the fullscreen element, so the previous/next
 * buttons, prompts and notifications, which live outside the viewer, stay visible. Moving between
 * images keeps it because the page itself doesn't change.
 */

const FULLSCREEN_CLASS = 'fb-image-fullscreen';
const ESCAPE_GRACE_MS = 200;

export const imageFullscreen = reactive({ available: false, active: false });

let leftAt = -Infinity;

function pageElement() {
  return document.documentElement;
}

function onFullscreenChange() {
  const active = document.fullscreenElement === pageElement();
  if (imageFullscreen.active && !active) leftAt = performance.now();
  imageFullscreen.active = active;
  pageElement().classList.toggle(FULLSCREEN_CLASS, active);
}

document.addEventListener('fullscreenchange', onFullscreenChange);

export function exitImageFullscreen() {
  if (document.fullscreenElement === pageElement()) document.exitFullscreen().catch(() => {});
}

/** Must run from a key press or click: browsers refuse fullscreen otherwise. */
export function toggleImageFullscreen() {
  if (imageFullscreen.active) {
    exitImageFullscreen();
  } else if (imageFullscreen.available) {
    pageElement().requestFullscreen?.().catch(() => {});
  }
}

export function setImageFullscreenAvailable(available) {
  imageFullscreen.available = available;
  if (!available) exitImageFullscreen();
}

/** True when Escape belongs to fullscreen; browsers may report the key just after leaving it. */
export function escapeLeavesImageFullscreen() {
  return imageFullscreen.active || performance.now() - leftAt < ESCAPE_GRACE_MS;
}
