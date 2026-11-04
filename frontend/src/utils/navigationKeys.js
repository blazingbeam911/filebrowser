/**
 * Keys that move the previewer to the next or previous file.
 *
 * ← / → do so in the image viewer (Ctrl+← / Ctrl+→ in the video and audio player, where the
 * plain arrows seek). PgDn / PgUp do the same everywhere, without Ctrl, and holding them behaves
 * like holding an arrow.
 */
const DIRECTIONS = {
  ArrowRight: "next",
  PageDown: "next",
  ArrowLeft: "previous",
  PageUp: "previous",
};

/** "next", "previous", or null for any other key. */
export function navigationKeyDirection(key) {
  return DIRECTIONS[key] ?? null;
}

/** True for PgUp / PgDn, which change file even in the video and audio player. */
export function isPageKey(key) {
  return key === "PageDown" || key === "PageUp";
}
