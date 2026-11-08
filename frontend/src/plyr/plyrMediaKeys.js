/**
 * Keyboard controls of the video and audio players that go beyond Plyr's own shortcuts.
 *
 * Plyr's global key handler listens on `window`; the viewer's handler listens on `document`, so it
 * runs first and can keep a key away from Plyr by stopping propagation.
 */

export const MIN_SPEED = 0.25;
export const MAX_SPEED = 4;
const SPEED_STEP = 0.1;
const LARGE_SPEED_STEP = 0.5;
const VOLUME_STEP = 0.02;

/** The action for a key event, or null when the key isn't one of these controls. */
export function mediaKeyAction(event) {
  if (event.ctrlKey || event.metaKey || event.altKey) return null;
  const { key, shiftKey } = event;
  switch (key) {
    case 'ArrowLeft':
      return { type: 'seek', seconds: shiftKey ? -10 : -1 };
    case 'ArrowRight':
      return { type: 'seek', seconds: shiftKey ? 10 : 1 };
    case 'ArrowUp':
      return shiftKey ? { type: 'seek', seconds: 60 } : { type: 'volume', delta: VOLUME_STEP };
    case 'ArrowDown':
      return shiftKey ? { type: 'seek', seconds: -60 } : { type: 'volume', delta: -VOLUME_STEP };
    case '[':
      return { type: 'speed', delta: -SPEED_STEP };
    case ']':
      return { type: 'speed', delta: SPEED_STEP };
    case '{':
      return { type: 'speed', delta: -LARGE_SPEED_STEP };
    case '}':
      return { type: 'speed', delta: LARGE_SPEED_STEP };
    case '\\':
      return { type: 'speedReset' };
    default:
      return null;
  }
}

function clampSpeed(speed) {
  return Math.min(MAX_SPEED, Math.max(MIN_SPEED, speed));
}

/** The speed one step from `current`, free of floating-point noise such as 1.2000000000000002. */
export function steppedSpeed(current, delta) {
  return clampSpeed(Math.round((current + delta) * 100) / 100);
}

/** A speed read back from storage, or null when it is missing or out of range. */
export function parseStoredSpeed(raw) {
  if (raw === null || raw === undefined || raw === '') return null;
  const speed = Number(raw);
  if (!Number.isFinite(speed) || speed < MIN_SPEED || speed > MAX_SPEED) return null;
  return speed;
}

// Drawn on a 960-unit square from the icon font's own outlines, because it has no icons numbered 1
// or 60: the arrows and the 1 and 10 from replay_10 and forward_10, and the 60 from 60fps, scaled to
// the same height. Keeping 10 here too means every size is drawn the same way.
const SEEK_ARROW_BACK = 'M120 520H200Q200 637 281.5 718.5Q363 800 480 800Q597 800 678.5 718.5Q760 637 760 520Q760 403 678.5 321.5Q597 240 480 240H474L536 302L480 360L320 200L480 40L536 98L474 160H480Q555 160 620.5 188.5Q686 217 734.5 265.5Q783 314 811.5 379.5Q840 445 840 520Q840 595 811.5 660.5Q783 726 734.5 774.5Q686 823 620.5 851.5Q555 880 480 880Q405 880 339.5 851.5Q274 823 225.5 774.5Q177 726 148.5 660.5Q120 595 120 520Z';
const SEEK_ARROW_FORWARD = 'M480 160H486L424 98L480 40L640 200L480 360L424 302L486 240H480Q363 240 281.5 321.5Q200 403 200 520Q200 637 281.5 718.5Q363 800 480 800Q597 800 678.5 718.5Q760 637 760 520H840Q840 595 811.5 660.5Q783 726 734.5 774.5Q686 823 620.5 851.5Q555 880 480 880Q405 880 339.5 851.5Q274 823 225.5 774.5Q177 726 148.5 660.5Q120 595 120 520Q120 445 148.5 379.5Q177 314 225.5 265.5Q274 217 339.5 188.5Q405 160 480 160Z';
const SEEK_DIGITS = {
  1: 'M480 640V460H420V400H540V640Z',
  10: 'M360 640V460H300V400H420V640ZM500 640Q483 640 471.5 628.5Q460 617 460 600V440Q460 423 471.5 411.5Q483 400 500 400H580Q597 400 608.5 411.5Q620 423 620 440V600Q620 617 608.5 628.5Q597 640 580 640ZM520 580H560V460H520Z',
  60: 'M411.4 640H360Q338.6 640 323.6 625Q308.6 610 308.6 588.6V451.4Q308.6 430 323.6 415Q338.6 400 360 400H445.7V451.4H360V485.7H411.4Q432.9 485.7 447.9 500.7Q462.9 515.7 462.9 537.1V588.6Q462.9 610 447.9 625Q432.9 640 411.4 640ZM360 537.1V588.6H411.4V537.1ZM531.4 588.6H600V451.4H531.4ZM531.4 640Q510 640 495 625Q480 610 480 588.6V451.4Q480 430 495 415Q510 400 531.4 400H600Q621.4 400 636.4 415Q651.4 430 651.4 451.4V588.6Q651.4 610 636.4 625Q621.4 640 600 640Z',
};

/** The SVG path of the icon for a seek of `seconds`: a circular arrow with the size inside. */
export function seekIconPath(seconds) {
  return (seconds < 0 ? SEEK_ARROW_BACK : SEEK_ARROW_FORWARD) + SEEK_DIGITS[Math.abs(seconds)];
}
