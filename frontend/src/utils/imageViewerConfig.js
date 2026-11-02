import { reactive } from 'vue';
import { getObjectProperty } from './object.js';

/**
 * Per-device image viewer settings, kept in localStorage (like editorConfig and
 * visualizerConfig), so each browser can pick values that suit its own memory and network.
 *
 * - preloadAhead:  images to preload in the direction the user is moving through the folder
 * - preloadBehind: images to preload in the opposite direction
 * - preloadMaxMB:  files larger than this (in MB) are never preloaded
 *
 * preloadAhead = preloadBehind = 0 turns preloading off.
 */

const STORAGE_KEY = 'imageViewerConfig';

export const IMAGE_VIEWER_DEFAULTS = Object.freeze({
  preloadAhead: 2,
  preloadBehind: 1,
  preloadMaxMB: 30,
});

export const IMAGE_VIEWER_LIMITS = Object.freeze({
  preloadAhead: Object.freeze({ min: 0, max: 10 }),
  preloadBehind: Object.freeze({ min: 0, max: 5 }),
  preloadMaxMB: Object.freeze({ min: 1, max: 500 }),
});

/** Rounds and clamps a finite number into [min, max]; anything else gives the fallback. */
function clampInt(value, { min, max }, fallback) {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    return fallback;
  }
  return Math.min(max, Math.max(min, Math.round(value)));
}

/**
 * Returns a complete, valid config from any input: missing or invalid fields fall back to
 * their defaults, out-of-range numbers are clamped, unknown fields are dropped.
 */
export function normalizeImageViewerConfig(raw) {
  const source = raw && typeof raw === 'object' && !Array.isArray(raw) ? raw : {};
  return {
    preloadAhead: clampInt(
      getObjectProperty(source, 'preloadAhead'),
      IMAGE_VIEWER_LIMITS.preloadAhead,
      IMAGE_VIEWER_DEFAULTS.preloadAhead,
    ),
    preloadBehind: clampInt(
      getObjectProperty(source, 'preloadBehind'),
      IMAGE_VIEWER_LIMITS.preloadBehind,
      IMAGE_VIEWER_DEFAULTS.preloadBehind,
    ),
    preloadMaxMB: clampInt(
      getObjectProperty(source, 'preloadMaxMB'),
      IMAGE_VIEWER_LIMITS.preloadMaxMB,
      IMAGE_VIEWER_DEFAULTS.preloadMaxMB,
    ),
  };
}

function load() {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (!stored) return normalizeImageViewerConfig(null);
    return normalizeImageViewerConfig(JSON.parse(stored));
  } catch (_) {
    return normalizeImageViewerConfig(null);
  }
}

function persist() {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(imageViewerConfig));
  } catch (_) { /* ignore — e.g. private browsing quota */ }
}

export const imageViewerConfig = reactive(load());

/** Merges `partial` into the current config, normalizes the result and saves it. */
export function saveImageViewerConfig(partial) {
  Object.assign(imageViewerConfig, normalizeImageViewerConfig({ ...imageViewerConfig, ...partial }));
  persist();
}

export function resetImageViewerConfig() {
  Object.assign(imageViewerConfig, IMAGE_VIEWER_DEFAULTS);
  persist();
}

/** True when at least one direction preloads. */
export function isPreloadEnabled(config = imageViewerConfig) {
  return config.preloadAhead > 0 || config.preloadBehind > 0;
}
