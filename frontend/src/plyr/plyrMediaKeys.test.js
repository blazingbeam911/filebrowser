import { describe, expect, it } from 'vitest';
import {
  MAX_SPEED,
  MIN_SPEED,
  mediaKeyAction,
  parseStoredSpeed,
  seekIconPath,
  steppedSpeed,
  wheelVolumeStep,
} from '@/plyr/plyrMediaKeys.js';

const key = (k, mods = {}) => ({ key: k, shiftKey: false, ctrlKey: false, altKey: false, metaKey: false, ...mods });

describe('mediaKeyAction', () => {
  it('seeks 1 s on plain left and right', () => {
    expect(mediaKeyAction(key('ArrowLeft'))).toEqual({ type: 'seek', seconds: -1 });
    expect(mediaKeyAction(key('ArrowRight'))).toEqual({ type: 'seek', seconds: 1 });
  });

  it('seeks 10 s with Shift on left and right', () => {
    expect(mediaKeyAction(key('ArrowLeft', { shiftKey: true }))).toEqual({ type: 'seek', seconds: -10 });
    expect(mediaKeyAction(key('ArrowRight', { shiftKey: true }))).toEqual({ type: 'seek', seconds: 10 });
  });

  it('seeks 60 s with Shift on up and down', () => {
    expect(mediaKeyAction(key('ArrowUp', { shiftKey: true }))).toEqual({ type: 'seek', seconds: 60 });
    expect(mediaKeyAction(key('ArrowDown', { shiftKey: true }))).toEqual({ type: 'seek', seconds: -60 });
  });

  it('changes volume by 2% on plain up and down', () => {
    expect(mediaKeyAction(key('ArrowUp'))).toEqual({ type: 'volume', delta: 0.02 });
    expect(mediaKeyAction(key('ArrowDown'))).toEqual({ type: 'volume', delta: -0.02 });
  });

  it('maps the speed keys', () => {
    expect(mediaKeyAction(key('['))).toEqual({ type: 'speed', delta: -0.1 });
    expect(mediaKeyAction(key(']'))).toEqual({ type: 'speed', delta: 0.1 });
    expect(mediaKeyAction(key('\\'))).toEqual({ type: 'speedReset' });
  });

  it('moves speed by 0.5 with Shift on the bracket keys', () => {
    expect(mediaKeyAction(key('{', { shiftKey: true }))).toEqual({ type: 'speed', delta: -0.5 });
    expect(mediaKeyAction(key('}', { shiftKey: true }))).toEqual({ type: 'speed', delta: 0.5 });
  });

  it('maps frame step and the A-B loop', () => {
    expect(mediaKeyAction(key(','))).toEqual({ type: 'frameStep', direction: -1 });
    expect(mediaKeyAction(key('.'))).toEqual({ type: 'frameStep', direction: 1 });
    expect(mediaKeyAction(key('L', { shiftKey: true }))).toEqual({ type: 'abLoop' });
  });

  it('leaves plain l, and an L typed with Caps Lock, to the single-file loop', () => {
    expect(mediaKeyAction(key('l'))).toBeNull();
    expect(mediaKeyAction(key('L'))).toBeNull();
  });

  it('maps the stats keys', () => {
    expect(mediaKeyAction(key('i'))).toEqual({ type: 'stats' });
    expect(mediaKeyAction(key('I', { shiftKey: true }))).toEqual({ type: 'statsBrief' });
    expect(mediaKeyAction(key('I'))).toBeNull();
  });

  it('maps theater mode', () => {
    expect(mediaKeyAction(key('t'))).toEqual({ type: 'theater' });
  });

  it('leaves Ctrl, Alt and Meta combinations alone, such as Ctrl+arrow between files', () => {
    expect(mediaKeyAction(key('ArrowRight', { ctrlKey: true }))).toBeNull();
    expect(mediaKeyAction(key('ArrowUp', { altKey: true }))).toBeNull();
    expect(mediaKeyAction(key('[', { metaKey: true }))).toBeNull();
  });

  it('leaves other keys to Plyr and the viewer', () => {
    for (const k of ['f', 'm', 'k', ' ', '5', 'l', 'PageDown']) {
      expect(mediaKeyAction(key(k))).toBeNull();
    }
  });
});

describe('steppedSpeed', () => {
  it('avoids floating-point noise', () => {
    expect(steppedSpeed(1.1, 0.1)).toBe(1.2);
    expect(steppedSpeed(1.2, 0.1)).toBe(1.3);
    expect(steppedSpeed(0.9, 0.1)).toBe(1);
  });

  it('stays within the limits', () => {
    expect(steppedSpeed(MAX_SPEED, 0.1)).toBe(MAX_SPEED);
    expect(steppedSpeed(MIN_SPEED, -0.1)).toBe(MIN_SPEED);
  });
});

describe('parseStoredSpeed', () => {
  it('accepts a speed within the limits', () => {
    expect(parseStoredSpeed('1.3')).toBe(1.3);
    expect(parseStoredSpeed('0.25')).toBe(0.25);
    expect(parseStoredSpeed('4')).toBe(4);
  });

  it('rejects missing, invalid and out-of-range values', () => {
    for (const raw of [null, undefined, '', 'fast', '0', '0.1', '4.5', 'NaN', 'Infinity']) {
      expect(parseStoredSpeed(raw)).toBeNull();
    }
  });
});

describe('seekIconPath', () => {
  it('has an icon for every seek size the keys and buttons use', () => {
    for (const seconds of [1, 10, 60]) {
      expect(seekIconPath(seconds)).toMatch(/^M.*Z$/);
      expect(seekIconPath(-seconds)).toMatch(/^M.*Z$/);
    }
  });

  it('points the arrow by direction and numbers it by size', () => {
    expect(seekIconPath(10)).not.toBe(seekIconPath(-10));
    expect(seekIconPath(1)).not.toBe(seekIconPath(60));
  });
});

describe('wheelVolumeStep', () => {
  it('takes one step per mouse wheel notch, in pixels or lines', () => {
    expect(wheelVolumeStep(0, -100, 0)).toEqual({ direction: 1, accumulated: 0 });
    expect(wheelVolumeStep(0, 3, 1)).toEqual({ direction: -1, accumulated: 0 });
  });

  it('adds up small touchpad movements', () => {
    const first = wheelVolumeStep(0, -20, 0);
    expect(first).toEqual({ direction: 0, accumulated: -20 });
    const second = wheelVolumeStep(first.accumulated, -20, 0);
    expect(second.direction).toBe(0);
    expect(wheelVolumeStep(second.accumulated, -20, 0)).toEqual({ direction: 1, accumulated: 0 });
  });

  it('adds up fractions of a line', () => {
    expect(wheelVolumeStep(0, -0.5, 1).direction).toBe(0);
    expect(wheelVolumeStep(-40, -0.5, 1).direction).toBe(1);
  });
});
