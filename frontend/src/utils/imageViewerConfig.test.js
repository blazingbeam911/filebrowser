import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const STORAGE_KEY = 'imageViewerConfig';
const DEFAULTS = { preloadAhead: 2, preloadBehind: 1, preloadMaxMB: 30 };

// Each test imports a fresh module; on a busy machine that can exceed the default timeout.
vi.setConfig({ testTimeout: 60_000, hookTimeout: 60_000 });

/** The module reads localStorage once at import, so each test imports a fresh copy. */
async function loadModule() {
  vi.resetModules();
  return import('./imageViewerConfig.js');
}

describe('imageViewerConfig', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
  });

  describe('loading', () => {
    it('uses the defaults when nothing is stored', async () => {
      const { imageViewerConfig } = await loadModule();
      expect({ ...imageViewerConfig }).toEqual(DEFAULTS);
    });

    it('reads valid stored values', async () => {
      localStorage.setItem(
        STORAGE_KEY,
        JSON.stringify({ preloadAhead: 4, preloadBehind: 0, preloadMaxMB: 100 }),
      );
      const { imageViewerConfig } = await loadModule();
      expect({ ...imageViewerConfig }).toEqual({ preloadAhead: 4, preloadBehind: 0, preloadMaxMB: 100 });
    });

    it('falls back to the defaults on invalid JSON', async () => {
      localStorage.setItem(STORAGE_KEY, '{not json');
      const { imageViewerConfig } = await loadModule();
      expect({ ...imageViewerConfig }).toEqual(DEFAULTS);
    });

    it.each([['5'], ['null'], ['[3, 1, 30]'], ['"text"']])(
      'falls back to the defaults when the stored JSON %s is not an object',
      async (stored) => {
        localStorage.setItem(STORAGE_KEY, stored);
        const { imageViewerConfig } = await loadModule();
        expect({ ...imageViewerConfig }).toEqual(DEFAULTS);
      },
    );

    it('falls back to the defaults when reading localStorage throws', async () => {
      vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
        throw new Error('denied');
      });
      const { imageViewerConfig } = await loadModule();
      expect({ ...imageViewerConfig }).toEqual(DEFAULTS);
    });

    it('keeps valid fields and replaces only the invalid ones', async () => {
      localStorage.setItem(
        STORAGE_KEY,
        JSON.stringify({ preloadAhead: '4', preloadBehind: 3, preloadMaxMB: null, extra: true }),
      );
      const { imageViewerConfig } = await loadModule();
      expect({ ...imageViewerConfig }).toEqual({ preloadAhead: 2, preloadBehind: 3, preloadMaxMB: 30 });
    });
  });

  describe('normalizeImageViewerConfig', () => {
    it('clamps values to their limits', async () => {
      const { normalizeImageViewerConfig } = await loadModule();
      expect(normalizeImageViewerConfig({ preloadAhead: 99, preloadBehind: -2, preloadMaxMB: 0 }))
        .toEqual({ preloadAhead: 10, preloadBehind: 0, preloadMaxMB: 1 });
      expect(normalizeImageViewerConfig({ preloadAhead: -1, preloadBehind: 9, preloadMaxMB: 10000 }))
        .toEqual({ preloadAhead: 0, preloadBehind: 5, preloadMaxMB: 500 });
    });

    it('rounds fractional values', async () => {
      const { normalizeImageViewerConfig } = await loadModule();
      expect(normalizeImageViewerConfig({ preloadAhead: 2.6, preloadBehind: 1.4, preloadMaxMB: 12.5 }))
        .toEqual({ preloadAhead: 3, preloadBehind: 1, preloadMaxMB: 13 });
    });

    it('treats NaN and Infinity as invalid', async () => {
      const { normalizeImageViewerConfig } = await loadModule();
      expect(normalizeImageViewerConfig({ preloadAhead: Number.NaN, preloadBehind: Infinity, preloadMaxMB: -Infinity }))
        .toEqual(DEFAULTS);
    });

    it('ignores inherited properties', async () => {
      const { normalizeImageViewerConfig } = await loadModule();
      const inherited = Object.create({ preloadAhead: 7 });
      expect(normalizeImageViewerConfig(inherited)).toEqual(DEFAULTS);
    });
  });

  describe('saving', () => {
    it('merges, normalizes and persists a partial update', async () => {
      const { imageViewerConfig, saveImageViewerConfig } = await loadModule();
      saveImageViewerConfig({ preloadAhead: 50, preloadMaxMB: 64 });
      const expected = { preloadAhead: 10, preloadBehind: 1, preloadMaxMB: 64 };
      expect({ ...imageViewerConfig }).toEqual(expected);
      expect(JSON.parse(localStorage.getItem(STORAGE_KEY))).toEqual(expected);
    });

    it('ignores an invalid field in an update instead of resetting it', async () => {
      localStorage.setItem(STORAGE_KEY, JSON.stringify({ preloadAhead: 4, preloadBehind: 2, preloadMaxMB: 80 }));
      const { imageViewerConfig, saveImageViewerConfig } = await loadModule();
      saveImageViewerConfig({ preloadAhead: '' });
      expect(imageViewerConfig.preloadAhead).toBe(2);
      expect(imageViewerConfig.preloadMaxMB).toBe(80);
    });

    it('survives a value saved by one import being read by the next', async () => {
      const first = await loadModule();
      first.saveImageViewerConfig({ preloadAhead: 0, preloadBehind: 0 });
      const second = await loadModule();
      expect({ ...second.imageViewerConfig }).toEqual({ preloadAhead: 0, preloadBehind: 0, preloadMaxMB: 30 });
    });

    it('does not throw when localStorage refuses to save', async () => {
      const { imageViewerConfig, saveImageViewerConfig } = await loadModule();
      vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
        throw new Error('QuotaExceededError');
      });
      expect(() => saveImageViewerConfig({ preloadAhead: 5 })).not.toThrow();
      expect(imageViewerConfig.preloadAhead).toBe(5);
    });

    it('resets to the defaults and persists them', async () => {
      localStorage.setItem(STORAGE_KEY, JSON.stringify({ preloadAhead: 7, preloadBehind: 4, preloadMaxMB: 200 }));
      const { imageViewerConfig, resetImageViewerConfig } = await loadModule();
      resetImageViewerConfig();
      expect({ ...imageViewerConfig }).toEqual(DEFAULTS);
      expect(JSON.parse(localStorage.getItem(STORAGE_KEY))).toEqual(DEFAULTS);
    });
  });

  describe('isPreloadEnabled', () => {
    it('is off only when both directions are 0', async () => {
      const { isPreloadEnabled } = await loadModule();
      expect(isPreloadEnabled({ preloadAhead: 0, preloadBehind: 0, preloadMaxMB: 30 })).toBe(false);
      expect(isPreloadEnabled({ preloadAhead: 1, preloadBehind: 0, preloadMaxMB: 30 })).toBe(true);
      expect(isPreloadEnabled({ preloadAhead: 0, preloadBehind: 1, preloadMaxMB: 30 })).toBe(true);
    });

    it('defaults to the live config', async () => {
      const { isPreloadEnabled, saveImageViewerConfig } = await loadModule();
      expect(isPreloadEnabled()).toBe(true);
      saveImageViewerConfig({ preloadAhead: 0, preloadBehind: 0 });
      expect(isPreloadEnabled()).toBe(false);
    });
  });
});
