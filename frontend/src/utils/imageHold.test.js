import { beforeEach, describe, expect, it, vi } from 'vitest';

const setPreloadHold = vi.fn();
vi.mock('@/utils/imagePreloader.js', () => ({ setPreloadHold: (v) => setPreloadHold(v) }));

const { imageHold, startHold, setHoldFast, endHold, reportShown, holdMayMove } = await import('./imageHold.js');

describe('imageHold', () => {
  beforeEach(() => {
    endHold();
    reportShown(null, 'none');
    setPreloadHold.mockClear();
  });

  describe('hold state', () => {
    it('starts plain or fast and ends', () => {
      startHold(false);
      expect(imageHold).toMatchObject({ active: true, fast: false });
      startHold(true);
      expect(imageHold).toMatchObject({ active: true, fast: true });
      endHold();
      expect(imageHold).toMatchObject({ active: false, fast: false });
    });

    it('releasing Shift keeps the hold going as a plain hold', () => {
      startHold(true);
      setHoldFast(false);
      expect(imageHold).toMatchObject({ active: true, fast: false });
    });

    it('cannot be fast without a hold', () => {
      setHoldFast(true);
      expect(imageHold.fast).toBe(false);
      expect(setPreloadHold).not.toHaveBeenCalled();
    });

    it('pauses the preloader only while fast, telling it once per change', () => {
      startHold(false);
      expect(setPreloadHold).not.toHaveBeenCalled();
      startHold(true);
      startHold(true);
      expect(setPreloadHold.mock.calls).toEqual([[true]]);
      endHold();
      expect(setPreloadHold.mock.calls).toEqual([[true], [false]]);
    });
  });

  describe('holdMayMove', () => {
    it('moves on once the full image of the current URL is shown', () => {
      startHold(false);
      reportShown('a', 'none');
      expect(holdMayMove('a')).toBe(false);
      reportShown('a', 'thumbnail');
      expect(holdMayMove('a')).toBe(false);
      reportShown('a', 'full');
      expect(holdMayMove('a')).toBe(true);
    });

    it('accepts a thumbnail only in a fast hold', () => {
      startHold(true);
      reportShown('a', 'thumbnail');
      expect(holdMayMove('a')).toBe(true);
      setHoldFast(false);
      expect(holdMayMove('a')).toBe(false);
    });

    it('never moves on a report about another image', () => {
      startHold(true);
      reportShown('previous', 'full');
      expect(holdMayMove('next')).toBe(false);
      expect(holdMayMove(null)).toBe(false);
    });

    it('a late placeholder report does not downgrade a full image', () => {
      startHold(false);
      reportShown('a', 'full');
      reportShown('a', 'thumbnail');
      reportShown('a', 'none');
      expect(holdMayMove('a')).toBe(true);
      reportShown('b', 'none');
      expect(holdMayMove('a')).toBe(false);
    });
  });
});
