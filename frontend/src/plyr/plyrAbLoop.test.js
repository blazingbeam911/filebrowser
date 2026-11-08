import { describe, expect, it } from 'vitest';
import { EMPTY_AB_LOOP, abLoopActive, abLoopMarkers, nextAbLoop } from '@/plyr/plyrAbLoop.js';

describe('nextAbLoop', () => {
  it('sets A, then B, then clears', () => {
    const a = nextAbLoop(EMPTY_AB_LOOP, 10);
    expect(a).toEqual({ a: 10, b: null });
    const ab = nextAbLoop(a, 12);
    expect(ab).toEqual({ a: 10, b: 12 });
    expect(nextAbLoop(ab, 20)).toEqual(EMPTY_AB_LOOP);
  });

  it('swaps a B earlier than A', () => {
    expect(nextAbLoop({ a: 10, b: null }, 4)).toEqual({ a: 4, b: 10 });
  });

  it('keeps waiting for B at the same spot as A', () => {
    const a = { a: 10, b: null };
    expect(nextAbLoop(a, 10.01)).toBe(a);
  });
});

describe('abLoopActive', () => {
  it('needs both points', () => {
    expect(abLoopActive(EMPTY_AB_LOOP)).toBe(false);
    expect(abLoopActive({ a: 1, b: null })).toBe(false);
    expect(abLoopActive({ a: 1, b: 2 })).toBe(true);
  });

  it('accepts a loop starting at zero', () => {
    expect(abLoopActive({ a: 0, b: 2 })).toBe(true);
  });
});

describe('abLoopMarkers', () => {
  it('places the points that are set', () => {
    expect(abLoopMarkers({ a: 10, b: null }, 100)).toEqual([10]);
    expect(abLoopMarkers({ a: 10, b: 50 }, 100)).toEqual([10, 50]);
  });

  it('has no markers without a duration', () => {
    expect(abLoopMarkers({ a: 10, b: 50 }, 0)).toEqual([]);
  });
});
