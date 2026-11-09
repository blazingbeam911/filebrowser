import { effectScope, reactive, watch } from 'vue';
import { getters, state } from '@/store/index.ts';

/**
 * Fullscreen for images and videos, with the whole page as the fullscreen element. The page outlives
 * the viewers, so moving between files doesn't leave fullscreen, and the previous and next buttons,
 * prompts and notifications, which live outside the viewers, stay visible.
 */

const FULLSCREEN_CLASS = 'fb-page-fullscreen';
const ESCAPE_GRACE_MS = 200;

export const pageFullscreen = reactive({ imageShowing: false, active: false });

let leftAt = -Infinity;
let watching = null;

function pageElement() {
  return document.documentElement;
}

/** True while an image or video is shown, or may be about to replace the current one. */
export function mediaMayFollow() {
  if (state.navigation.isTransitioning) return true;
  if (getters.currentView() !== 'preview') return false;
  return ['video', 'image'].includes(getters.previewType()) || pageFullscreen.imageShowing;
}

export function exitPageFullscreen() {
  if (document.fullscreenElement === pageElement()) document.exitFullscreen().catch(() => {});
}

function stopWatching() {
  watching?.stop();
  watching = null;
}

function leaveWhenMediaEnds() {
  if (watching) return;
  // Detached: this can start while a component is being set up, and must outlive it.
  watching = effectScope(true);
  watching.run(() => {
    watch(mediaMayFollow, (mayFollow) => {
      if (!mayFollow) exitPageFullscreen();
    }, { flush: 'sync' });
  });
}

function onFullscreenChange() {
  const active = document.fullscreenElement === pageElement();
  if (pageFullscreen.active && !active) leftAt = performance.now();
  pageFullscreen.active = active;
  pageElement().classList.toggle(FULLSCREEN_CLASS, active);
  if (active) leaveWhenMediaEnds();
  else stopWatching();
}

document.addEventListener('fullscreenchange', onFullscreenChange);

/** Must run from a key press or click: browsers refuse fullscreen otherwise. */
export function togglePageFullscreen() {
  if (pageFullscreen.active) {
    exitPageFullscreen();
  } else if (pageFullscreen.imageShowing) {
    pageElement().requestFullscreen?.().catch(() => {});
  }
}

export function setImageShowing(showing) {
  pageFullscreen.imageShowing = showing;
}

/** True when Escape belongs to fullscreen; browsers may report the key just after leaving it. */
export function escapeLeavesPageFullscreen() {
  return pageFullscreen.active || performance.now() - leftAt < ESCAPE_GRACE_MS;
}
