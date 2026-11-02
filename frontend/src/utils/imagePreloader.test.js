import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

const fileViewingDisabled = vi.fn(() => false);

vi.mock('@/store/index.ts', () => ({
  getters: { fileViewingDisabled: (name) => fileViewingDisabled(name) },
  state: {},
}));

// URL building is covered by previewRawUrl.test.js; here a URL is just "url:<path>".
vi.mock('@/utils/previewRawUrl.js', () => ({
  isHeicOrHeif: (type) => type === 'image/heic' || type === 'image/heif',
  isServerConvertedImage: (item) => item.type === 'image/x-canon-cr2',
  previewRawUrl: (item) => `url:${item.path}`,
}));

/** Stand-in for HTMLImageElement: decode() settles when the test says so. */
class FakeImage {
  static instances = [];

  constructor() {
    this.attrSrc = null;
    this.decodePromise = new Promise((resolve, reject) => {
      this.finishDecode = resolve;
      this.failDecode = reject;
    });
    FakeImage.instances.push(this);
  }

  set src(value) {
    this.attrSrc = value;
  }

  get src() {
    return this.attrSrc;
  }

  removeAttribute(name) {
    if (name === 'src') {
      this.attrSrc = null;
      this.aborted = true;
      this.failDecode(new Error('aborted'));
    }
  }

  decode() {
    return this.decodePromise;
  }
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

// Fresh module imports can be slow on a busy machine; give hooks that do them more time.
const IMPORT_HOOK_TIMEOUT_MS = 60_000;

function file(name, extra = {}) {
  return { name, type: 'image/jpeg', size: 2 * 1024 * 1024, ...extra };
}

async function loadModules(config) {
  vi.resetModules();
  localStorage.clear();
  if (config) {
    localStorage.setItem('imageViewerConfig', JSON.stringify(config));
  }
  return import('./imagePreloader.js');
}

/** URLs currently being downloaded or held (not aborted). */
function liveSrcs() {
  return FakeImage.instances.filter((img) => !img.aborted).map((img) => img.src);
}

describe('isPreloadableImage', () => {
  let isPreloadableImage;

  beforeAll(async () => {
    ({ isPreloadableImage } = await loadModules());
  }, IMPORT_HOOK_TIMEOUT_MS);

  beforeEach(() => {
    fileViewingDisabled.mockReturnValue(false);
  });

  it('accepts ordinary images within the size limit', () => {
    expect(isPreloadableImage(file('a.jpg'), 30)).toBe(true);
    expect(isPreloadableImage(file('b.png', { type: 'image/png' }), 30)).toBe(true);
    expect(isPreloadableImage(file('c.jpg', { size: undefined }), 30)).toBe(true);
  });

  it('rejects files over the size limit', () => {
    expect(isPreloadableImage(file('big.jpg', { size: 31 * 1024 * 1024 }), 30)).toBe(false);
    expect(isPreloadableImage(file('edge.jpg', { size: 30 * 1024 * 1024 }), 30)).toBe(true);
  });

  it('rejects non-images, directories and missing items', () => {
    expect(isPreloadableImage(null, 30)).toBe(false);
    expect(isPreloadableImage({ name: 'dir', type: 'directory' }, 30)).toBe(false);
    expect(isPreloadableImage(file('v.mp4', { type: 'video/mp4' }), 30)).toBe(false);
    expect(isPreloadableImage(file('t.txt', { type: 'text/plain' }), 30)).toBe(false);
  });

  it('rejects HEIC/HEIF, TIFF and server-converted images', () => {
    expect(isPreloadableImage(file('p.heic', { type: 'image/heic' }), 30)).toBe(false);
    expect(isPreloadableImage(file('p.heif', { type: 'image/heif' }), 30)).toBe(false);
    expect(isPreloadableImage(file('s.tiff', { type: 'image/tiff' }), 30)).toBe(false);
    expect(isPreloadableImage(file('s.TIF', { type: 'image/x-unknown' }), 30)).toBe(false);
    expect(isPreloadableImage(file('r.cr2', { type: 'image/x-canon-cr2' }), 30)).toBe(false);
  });

  it('rejects files whose viewing is disabled', () => {
    fileViewingDisabled.mockReturnValue(true);
    expect(isPreloadableImage(file('a.jpg'), 30)).toBe(false);
  });
});

describe('travelDirection', () => {
  let travelDirection;

  beforeAll(async () => {
    ({ travelDirection } = await loadModules());
  }, IMPORT_HOOK_TIMEOUT_MS);

  it('starts as next in a new folder', () => {
    expect(travelDirection(null, 'src|/a', 5)).toBe('next');
    expect(travelDirection({ key: 'src|/b', index: 9, direction: 'previous' }, 'src|/a', 5)).toBe('next');
  });

  it('follows the move within a folder', () => {
    expect(travelDirection({ key: 'k', index: 4, direction: 'next' }, 'k', 3)).toBe('previous');
    expect(travelDirection({ key: 'k', index: 3, direction: 'previous' }, 'k', 7)).toBe('next');
  });

  it('keeps the last direction when the index did not change', () => {
    expect(travelDirection({ key: 'k', index: 4, direction: 'previous' }, 'k', 4)).toBe('previous');
  });
});

describe('computePreloadOrder', () => {
  let computePreloadOrder;
  const all = () => true;
  const items = Array.from({ length: 10 }, (_, i) => ({ i }));

  beforeAll(async () => {
    ({ computePreloadOrder } = await loadModules());
  }, IMPORT_HOOK_TIMEOUT_MS);

  it('orders nearest ahead, nearest behind, rest ahead, rest behind', () => {
    expect(computePreloadOrder(items, 5, 'next', { ahead: 3, behind: 2 }, all)).toEqual([6, 4, 7, 8, 3]);
  });

  it('mirrors when travelling backwards', () => {
    expect(computePreloadOrder(items, 5, 'previous', { ahead: 3, behind: 2 }, all)).toEqual([4, 6, 3, 2, 7]);
  });

  it('skips items that are not accepted and keeps counting past them', () => {
    const odd = (item) => item.i % 2 === 1;
    expect(computePreloadOrder(items, 4, 'next', { ahead: 2, behind: 1 }, odd)).toEqual([5, 3, 7]);
  });

  it('stops at the ends of the listing', () => {
    expect(computePreloadOrder(items, 9, 'next', { ahead: 3, behind: 1 }, all)).toEqual([8]);
    expect(computePreloadOrder(items, 0, 'next', { ahead: 2, behind: 2 }, all)).toEqual([1, 2]);
  });

  it('returns nothing for zero counts', () => {
    expect(computePreloadOrder(items, 5, 'next', { ahead: 0, behind: 0 }, all)).toEqual([]);
  });

  it('handles behind-only', () => {
    expect(computePreloadOrder(items, 5, 'next', { ahead: 0, behind: 2 }, all)).toEqual([4, 3]);
  });
});

describe('preloading at runtime', () => {
  let preloader;
  const listing = Array.from({ length: 8 }, (_, i) => file(`img${i}.jpg`));

  function refresh(index, items = listing, directoryPath = '/album') {
    preloader.refreshImagePreloads({ items, currentIndex: index, directoryPath, source: 'media' });
  }

  beforeEach(async () => {
    FakeImage.instances = [];
    vi.stubGlobal('Image', FakeImage);
    fileViewingDisabled.mockReturnValue(false);
    preloader = await loadModules({ preloadAhead: 2, preloadBehind: 1, preloadMaxMB: 30 });
  }, IMPORT_HOOK_TIMEOUT_MS);

  afterEach(() => {
    preloader.clearImagePreloads();
    vi.unstubAllGlobals();
  });

  it('waits for the viewer before loading anything', () => {
    refresh(3);
    expect(FakeImage.instances).toHaveLength(0);
  });

  it('loads one image at a time, nearest first, and marks them ready once decoded', async () => {
    refresh(3);
    preloader.resumeImagePreloads();
    expect(liveSrcs()).toEqual(['url:/album/img4.jpg']);
    expect(preloader.isImagePreloaded('url:/album/img4.jpg')).toBe(false);

    FakeImage.instances[0].finishDecode();
    await flush();
    expect(preloader.isImagePreloaded('url:/album/img4.jpg')).toBe(true);
    expect(FakeImage.instances.map((img) => img.src)).toEqual(['url:/album/img4.jpg', 'url:/album/img2.jpg']);

    FakeImage.instances[1].finishDecode();
    await flush();
    expect(FakeImage.instances.map((img) => img.src)).toEqual([
      'url:/album/img4.jpg',
      'url:/album/img2.jpg',
      'url:/album/img5.jpg',
    ]);
  });

  it('does not start new loads while paused, and continues on resume', async () => {
    refresh(3);
    preloader.resumeImagePreloads();
    preloader.pauseImagePreloads('url:/album/img3.jpg');
    FakeImage.instances[0].finishDecode();
    await flush();
    expect(FakeImage.instances).toHaveLength(1);

    preloader.resumeImagePreloads();
    expect(FakeImage.instances).toHaveLength(2);
  });

  it('keeps images still in the window and releases the rest when moving on', async () => {
    refresh(3);
    preloader.resumeImagePreloads();
    for (let n = 0; n < 3; n++) {
      FakeImage.instances[n].finishDecode();
      await flush();
    }
    // Ready: img4, img2, img5. Move to img4: window becomes img5, img3, img6.
    preloader.pauseImagePreloads('url:/album/img4.jpg');
    refresh(4);
    expect(preloader.isImagePreloaded('url:/album/img5.jpg')).toBe(true);
    expect(preloader.isImagePreloaded('url:/album/img2.jpg')).toBe(false);
    expect(FakeImage.instances[1].aborted).toBe(true);
    // img4 is the image being opened: kept (still preloaded for the viewer), not aborted.
    expect(preloader.isImagePreloaded('url:/album/img4.jpg')).toBe(true);
    expect(FakeImage.instances[0].aborted).toBeUndefined();

    preloader.resumeImagePreloads();
    expect(FakeImage.instances.at(-1).src).toBe('url:/album/img3.jpg');
  });

  it('aborts an in-flight download that leaves the window and moves on', async () => {
    refresh(3);
    preloader.resumeImagePreloads();
    const first = FakeImage.instances[0];
    expect(first.src).toBe('url:/album/img4.jpg');

    refresh(0, listing, '/other');
    expect(first.aborted).toBe(true);
    // A new position waits for the viewer to show its image first.
    expect(liveSrcs()).toEqual([]);
    preloader.pauseImagePreloads('url:/other/img0.jpg');
    preloader.resumeImagePreloads();
    await flush();
    expect(liveSrcs()).toEqual(['url:/other/img1.jpg']);
  });

  it('holds new preloads after a move until the viewer has shown the new image', async () => {
    preloader.pauseImagePreloads('url:/album/img3.jpg');
    preloader.resumeImagePreloads();
    refresh(3);
    expect(liveSrcs()).toEqual(['url:/album/img4.jpg']);
    FakeImage.instances[0].finishDecode();
    await flush();
    const before = FakeImage.instances.length;

    // Move on: the window changes, but nothing new starts (not even the image just left)
    // until the viewer reports the new image on screen.
    refresh(4);
    expect(FakeImage.instances).toHaveLength(before);
    preloader.pauseImagePreloads('url:/album/img4.jpg');
    preloader.resumeImagePreloads();
    expect(FakeImage.instances).toHaveLength(before + 1);
  });

  it('does not hold preloads when the viewer already shows the current image', () => {
    preloader.pauseImagePreloads('url:/album/img3.jpg');
    preloader.resumeImagePreloads();
    refresh(3);
    expect(liveSrcs()).toEqual(['url:/album/img4.jpg']);
  });

  it('carries on after a while if the viewer never reports back', () => {
    vi.useFakeTimers();
    try {
      refresh(5);
      vi.advanceTimersByTime(2999);
      expect(FakeImage.instances).toHaveLength(0);
      vi.advanceTimersByTime(1);
      expect(liveSrcs()).toEqual(['url:/album/img6.jpg']);
    } finally {
      vi.useRealTimers();
    }
  });

  it('follows a reversal of direction', async () => {
    refresh(5);
    refresh(4);
    preloader.resumeImagePreloads();
    // Moving backwards: nearest ahead is img3, nearest behind img5.
    expect(liveSrcs()).toEqual(['url:/album/img3.jpg']);
    FakeImage.instances[0].finishDecode();
    await flush();
    FakeImage.instances[1].finishDecode();
    await flush();
    expect(FakeImage.instances.map((img) => img.src)).toEqual([
      'url:/album/img3.jpg',
      'url:/album/img5.jpg',
      'url:/album/img2.jpg',
    ]);
  });

  it('drops an image that fails to load and continues with the next', async () => {
    refresh(3);
    preloader.resumeImagePreloads();
    FakeImage.instances[0].failDecode(new Error('404'));
    await flush();
    expect(preloader.isImagePreloaded('url:/album/img4.jpg')).toBe(false);
    expect(FakeImage.instances[1].src).toBe('url:/album/img2.jpg');
  });

  it('skips files the filter rejects', () => {
    const mixed = [
      file('a.jpg'),
      file('clip.mp4', { type: 'video/mp4' }),
      file('huge.jpg', { size: 200 * 1024 * 1024 }),
      file('b.jpg'),
    ];
    refresh(0, mixed);
    preloader.resumeImagePreloads();
    expect(liveSrcs()).toEqual(['url:/album/b.jpg']);
  });

  it('preloads nothing when both counts are 0', async () => {
    preloader = await loadModules({ preloadAhead: 0, preloadBehind: 0, preloadMaxMB: 30 });
    refresh(3);
    preloader.resumeImagePreloads();
    expect(FakeImage.instances).toHaveLength(0);
  }, IMPORT_HOOK_TIMEOUT_MS);

  it('clearImagePreloads aborts everything and pauses', async () => {
    refresh(3);
    preloader.resumeImagePreloads();
    FakeImage.instances[0].finishDecode();
    await flush();
    preloader.clearImagePreloads();
    expect(FakeImage.instances.every((img) => img.aborted)).toBe(true);
    expect(preloader.isImagePreloaded('url:/album/img4.jpg')).toBe(false);

    refresh(3);
    expect(liveSrcs()).toEqual([]);
  });

  it('clears on an invalid position', async () => {
    refresh(3);
    preloader.resumeImagePreloads();
    refresh(-1);
    expect(FakeImage.instances[0].aborted).toBe(true);
  });
});
