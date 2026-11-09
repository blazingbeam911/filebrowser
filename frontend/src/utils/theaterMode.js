import { effectScope, watch } from 'vue';
import { getters, state } from '@/store/index.ts';

export const THEATER_CLASS = 'fb-theater';
export const THEATER_BAR_HIDDEN_CLASS = 'fb-theater-bar-hidden';

let watching = null;

export function videoMayFollow() {
  if (state.navigation.isTransitioning) return true;
  return getters.currentView() === 'preview' && getters.previewType() === 'video';
}

function removeClasses() {
  document.documentElement.classList.remove(THEATER_CLASS, THEATER_BAR_HIDDEN_CLASS);
}

/** A video viewer has taken over the layout. */
export function claimTheaterLayout() {
  watching?.stop();
  watching = null;
}

/**
 * Called as a video viewer goes away. Moving between files replaces the viewer, more than once on
 * the way, so the layout is kept until the page has moved on to something that isn't a video. That
 * stops the page and the top bar from jumping during the switch.
 */
export function releaseTheaterLayout() {
  if (watching) return;
  if (!videoMayFollow()) {
    removeClasses();
    return;
  }
  // Detached: this runs while a component unmounts, and a watcher created there would stop with it.
  watching = effectScope(true);
  watching.run(() => {
    watch(videoMayFollow, (mayFollow) => {
      if (mayFollow) return;
      claimTheaterLayout();
      removeClasses();
    }, { flush: 'sync' });
  });
}
