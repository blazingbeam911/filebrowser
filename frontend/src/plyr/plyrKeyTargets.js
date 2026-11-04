/**
 * Where keyboard shortcuts give way to typing.
 *
 * Plyr's default `selectors.editable` is `'input, textarea, select, [contenteditable]'`, which also
 * matches its own volume slider (`<input type="range">`): once the slider had been used, focus
 * stayed on it and every shortcut (f, space, m, arrows) was ignored until a click elsewhere.
 * Sliders don't take text, so they don't count.
 */
export const PLYR_EDITABLE_SELECTOR = 'input:not([type="range"]), textarea, select, [contenteditable]';

/** True when a key event's target is somewhere the user types, so shortcuts must not fire. */
export function isTextEntryTarget(target) {
  return Boolean(target && typeof target.matches === 'function' && target.matches(PLYR_EDITABLE_SELECTOR));
}
