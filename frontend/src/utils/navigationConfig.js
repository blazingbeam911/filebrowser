import { reactive } from 'vue';
import { getTypeInfo } from './mimetype.js';

/**
 * Per-device previewer navigation setting, kept in localStorage (like imageViewerConfig).
 *
 * - sameCategory: previous / next (keys, side arrows, swipes) only reach files of the same
 *   category as the open one (getTypeInfo(type).simpleType: image, video, audio, document,
 *   text, ...). Off by default.
 */

const STORAGE_KEY = 'viewerNavigationConfig';

export const NAVIGATION_DEFAULTS = Object.freeze({
  sameCategory: false,
});

/** Returns a complete, valid config from any input; unknown fields are dropped. */
export function normalizeNavigationConfig(raw) {
  const source = raw && typeof raw === 'object' && !Array.isArray(raw) ? raw : {};
  return {
    sameCategory: typeof source.sameCategory === 'boolean'
      ? source.sameCategory
      : NAVIGATION_DEFAULTS.sameCategory,
  };
}

function load() {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (!stored) return normalizeNavigationConfig(null);
    return normalizeNavigationConfig(JSON.parse(stored));
  } catch (_) {
    return normalizeNavigationConfig(null);
  }
}

function persist() {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(navigationConfig));
  } catch (_) { /* ignore — e.g. private browsing quota */ }
}

export const navigationConfig = reactive(load());

export function setSameCategoryOnly(value) {
  navigationConfig.sameCategory = Boolean(value);
  persist();
}

/** The file category navigation compares, or null for items without a type. */
export function navigationCategory(type) {
  if (!type) return null;
  return getTypeInfo(type).simpleType ?? null;
}

/**
 * Filter for previous / next candidates: with sameCategory on, only items of the current
 * item's category; otherwise every item.
 */
export function sameCategoryFilter(currentType, config = navigationConfig) {
  if (!config.sameCategory) {
    return () => true;
  }
  const category = navigationCategory(currentType);
  return (item) => navigationCategory(item?.type) === category;
}
