import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const STORAGE_KEY = 'viewerNavigationConfig';

// Each test imports a fresh module; on a busy machine that can exceed the default timeout.
vi.setConfig({ testTimeout: 60_000, hookTimeout: 60_000 });

/** The module reads localStorage once at import, so each test imports a fresh copy. */
async function loadModule() {
  vi.resetModules();
  return import('./navigationConfig.js');
}

describe('navigationConfig', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    localStorage.clear();
  });

  it('is off when nothing is stored', async () => {
    const { navigationConfig } = await loadModule();
    expect({ ...navigationConfig }).toEqual({ sameCategory: false });
  });

  it('reads a stored value', async () => {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ sameCategory: true }));
    const { navigationConfig } = await loadModule();
    expect(navigationConfig.sameCategory).toBe(true);
  });

  it('falls back to off on invalid JSON or values, dropping unknown fields', async () => {
    const { normalizeNavigationConfig } = await loadModule();
    expect(normalizeNavigationConfig(null)).toEqual({ sameCategory: false });
    expect(normalizeNavigationConfig([true])).toEqual({ sameCategory: false });
    expect(normalizeNavigationConfig({ sameCategory: 'yes', other: 1 })).toEqual({ sameCategory: false });

    localStorage.setItem(STORAGE_KEY, '{not json');
    const fresh = await loadModule();
    expect(fresh.navigationConfig.sameCategory).toBe(false);
  });

  it('saves the switch', async () => {
    const { navigationConfig, setSameCategoryOnly } = await loadModule();
    setSameCategoryOnly(true);
    expect(navigationConfig.sameCategory).toBe(true);
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY))).toEqual({ sameCategory: true });
    setSameCategoryOnly(false);
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY))).toEqual({ sameCategory: false });
  });

  describe('sameCategoryFilter', () => {
    const items = [
      { name: 'photo_10.jpg', type: 'image/jpeg' },
      { name: 'photo_10b_clip.mp4', type: 'video/mp4' },
      { name: 'photo_11.png', type: 'image/png' },
      { name: 'scan.tiff', type: 'image/tiff' },
      { name: 'song.mp3', type: 'audio/mpeg' },
      { name: 'notes.pdf', type: 'application/pdf' },
      { name: 'report.docx', type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document' },
      { name: 'untyped' },
    ];
    const names = (filter) => items.filter(filter).map((item) => item.name);

    it('accepts everything when off', async () => {
      const { sameCategoryFilter } = await loadModule();
      expect(names(sameCategoryFilter('image/jpeg', { sameCategory: false }))).toHaveLength(items.length);
    });

    it('keeps only the current category when on', async () => {
      const { sameCategoryFilter } = await loadModule();
      const on = { sameCategory: true };
      expect(names(sameCategoryFilter('image/jpeg', on))).toEqual(['photo_10.jpg', 'photo_11.png', 'scan.tiff']);
      expect(names(sameCategoryFilter('video/mp4', on))).toEqual(['photo_10b_clip.mp4']);
      expect(names(sameCategoryFilter('audio/mpeg', on))).toEqual(['song.mp3']);
      expect(names(sameCategoryFilter('application/pdf', on))).toEqual(['notes.pdf', 'report.docx']);
    });

    it('follows the stored setting by default', async () => {
      const { sameCategoryFilter, setSameCategoryOnly } = await loadModule();
      expect(names(sameCategoryFilter('video/mp4'))).toHaveLength(items.length);
      setSameCategoryOnly(true);
      expect(names(sameCategoryFilter('video/mp4'))).toEqual(['photo_10b_clip.mp4']);
    });
  });
});
