import { effectScope, watch } from 'vue';
import { getters, state } from '@/store/index.ts';
import { imageFullscreen } from '@/utils/imageFullscreen.js';

export const THEATER_CLASS = 'fb-theater';
export const THEATER_BAR_HIDDEN_CLASS = 'fb-theater-bar-hidden';
const STORAGE_KEY = 'fbTheaterMode';

let watching = null;

export function readTheaterSetting() {
  try {
    return localStorage.getItem(STORAGE_KEY) === '1';
  } catch {
    return false;
  }
}

export function writeTheaterSetting(on) {
  try {
    localStorage.setItem(STORAGE_KEY, on ? '1' : '0');
  } catch {
    /* storage unavailable: it just isn't remembered */
  }
}

export function applyTheaterLayout(on) {
  const root = document.documentElement;
  root.classList.toggle(THEATER_CLASS, on);
  if (!on) root.classList.remove(THEATER_BAR_HIDDEN_CLASS);
}

/** The top bar never disappears from under the mouse. */
export function setTheaterBarHidden(hidden) {
  if (hidden && document.querySelector('header.flexbar')?.matches(':hover')) return;
  document.documentElement.classList.toggle(THEATER_BAR_HIDDEN_CLASS, hidden);
}

export function videoMayFollow() {
  if (state.navigation.isTransitioning) return true;
  return getters.currentView() === 'preview' && getters.previewType() === 'video';
}

function theaterMediaMayFollow() {
  if (state.navigation.isTransitioning) return true;
  if (getters.currentView() !== 'preview') return false;
  return ['video', 'image'].includes(getters.previewType()) || imageFullscreen.available;
}

function removeClasses() {
  document.documentElement.classList.remove(THEATER_CLASS, THEATER_BAR_HIDDEN_CLASS);
}

/** A viewer has taken over the layout. */
export function claimTheaterLayout() {
  watching?.stop();
  watching = null;
}

/**
 * Called as a viewer goes away. Moving between files replaces the viewer, more than once on the
 * way, so the layout is kept until the page has moved on to something without theater mode. That
 * stops the page and the top bar from jumping during the switch.
 */
export function releaseTheaterLayout() {
  if (watching) return;
  if (!theaterMediaMayFollow()) {
    removeClasses();
    return;
  }
  // Detached: this runs while a component unmounts, and a watcher created there would stop with it.
  watching = effectScope(true);
  watching.run(() => {
    watch(theaterMediaMayFollow, (mayFollow) => {
      if (mayFollow) return;
      claimTheaterLayout();
      removeClasses();
    }, { flush: 'sync' });
  });
}
