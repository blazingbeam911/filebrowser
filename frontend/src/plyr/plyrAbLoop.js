/**
 * A-B loop state: a temporary loop between two points of one file. It is separate from the player's
 * loop setting and playback mode, which it never reads or changes.
 */

export const EMPTY_AB_LOOP = Object.freeze({ a: null, b: null });
const MIN_LOOP_SECONDS = 0.05;

export function abLoopActive(loop) {
  return loop.a !== null && loop.b !== null;
}

/** The next state for a press of the key: set A, set B (swapped if earlier than A), clear. */
export function nextAbLoop(loop, time) {
  if (loop.a === null) return { a: time, b: null };
  if (loop.b !== null) return EMPTY_AB_LOOP;
  if (Math.abs(time - loop.a) < MIN_LOOP_SECONDS) return loop;
  return time > loop.a ? { a: loop.a, b: time } : { a: time, b: loop.a };
}

/** Where the loop's points sit along a file of `duration`, as percentages. */
export function abLoopMarkers(loop, duration) {
  if (!(duration > 0)) return [];
  return [loop.a, loop.b].filter((t) => t !== null).map((t) => (t / duration) * 100);
}
