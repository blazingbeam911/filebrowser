import { effectScope, watch } from 'vue';
import { videoMayFollow } from '@/utils/theaterMode.js';

/**
 * Moving between videos replaces the whole player, so the page would show through until the next
 * video has a frame. This keeps the last frame on screen in the meantime, as the image viewer does.
 */

const LONGEST_HOLD_MS = 6000;

let canvas = null;
let watching = null;
let longestTimer = null;
let settleTimer = null;

export function releaseVideoFrame() {
  clearTimeout(longestTimer);
  clearTimeout(settleTimer);
  longestTimer = null;
  settleTimer = null;
  watching?.stop();
  watching = null;
  canvas?.remove();
  canvas = null;
}

function drawFrame(video) {
  const rect = video.getBoundingClientRect();
  const main = document.querySelector('#main');
  if (!main || !rect.width || !rect.height || !video.videoWidth) return null;
  const ratio = window.devicePixelRatio || 1;
  const hold = document.createElement('canvas');
  hold.className = 'video-switch-hold';
  hold.width = Math.round(rect.width * ratio);
  hold.height = Math.round(rect.height * ratio);
  Object.assign(hold.style, {
    position: 'fixed',
    top: `${rect.top}px`,
    left: `${rect.left}px`,
    width: `${rect.width}px`,
    height: `${rect.height}px`,
    background: '#000',
    pointerEvents: 'none',
    zIndex: '10000',
  });
  const scale = Math.min(hold.width / video.videoWidth, hold.height / video.videoHeight);
  const width = video.videoWidth * scale;
  const height = video.videoHeight * scale;
  hold.getContext('2d').drawImage(video, (hold.width - width) / 2, (hold.height - height) / 2, width, height);
  main.append(hold);
  return hold;
}

/** Puts the frame `video` is showing on screen, until `releaseVideoFrame` or the page moves on. */
export function holdVideoFrame(video) {
  if (!canvas && video.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA) {
    try {
      canvas = drawFrame(video);
    } catch {
      canvas = null;
    }
  }
  if (!canvas) return;
  clearTimeout(longestTimer);
  clearTimeout(settleTimer);
  settleTimer = null;
  longestTimer = setTimeout(releaseVideoFrame, LONGEST_HOLD_MS);
  if (watching) return;
  // Detached: this runs while a component unmounts, and a watcher created there would stop with it.
  watching = effectScope(true);
  watching.run(() => {
    watch(videoMayFollow, (mayFollow) => {
      if (!mayFollow) releaseVideoFrame();
    }, { flush: 'sync' });
  });
}

/** The next player exists: if it hasn't shown a frame after `ms`, as when it stays paused, let go. */
export function settleVideoFrame(ms) {
  if (!canvas) return;
  clearTimeout(settleTimer);
  settleTimer = setTimeout(releaseVideoFrame, ms);
}
