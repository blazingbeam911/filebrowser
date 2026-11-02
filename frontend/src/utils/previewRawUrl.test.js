import { beforeEach, describe, expect, it, vi } from 'vitest';

// URL builders are replaced by stubs that encode their arguments, so a test can see exactly
// which builder was chosen and with what.
vi.mock('@/api/index.js', () => ({
  resourcesApi: {
    getViewURL: (...args) => `view:${JSON.stringify(args)}`,
    getPreviewURL: (...args) => `preview:${JSON.stringify(args)}`,
    getPreviewURLPublic: (...args) => `previewPublic:${JSON.stringify(args)}`,
  },
}));

vi.mock('@/store/index.ts', () => ({
  state: {
    isSafari: false,
    shareInfo: { hash: '', subPath: '' },
    user: { disableViewingExt: [] },
    req: null,
  },
  getters: {
    isShare: () => mockState().shareInfo.hash !== '',
  },
}));

vi.mock('@/utils/constants.js', () => ({
  globalVars: { muPdfAvailable: true, mediaAvailable: true, enableHeicConversion: true },
}));

import { resourcesApi } from '@/api/index.js';
import { state } from '@/store/index.ts';
import { globalVars } from '@/utils/constants.js';
import { isRawImageMimeType } from '@/utils/mimetype.js';
import { removeTrailingSlash } from '@/utils/url.js';
import { isPdfConvertable, isServerConvertedImage, previewRawUrl } from './previewRawUrl.js';

function mockState() {
  return state;
}

/**
 * Upstream v2.1.4 `Preview.vue` computed `pdfConvertable` + `raw`, copied verbatim except
 * that `state.req` is the `req` argument. previewRawUrl must return the same for every input.
 */
function legacyRaw(req) {
  const pdfConvertable = (() => {
    if (!globalVars.muPdfAvailable) {
      return false;
    }
    const ext = `.${req.name.split('.').pop().toLowerCase()}`;
    if (state.user.disableViewingExt.includes(ext)) {
      return false;
    }
    switch (ext) {
      case '.xps':
      case '.mobi':
      case '.fb2':
      case '.cbz':
      case '.svg':
      case '.docx':
      case '.pptx':
      case '.xlsx':
      case '.hwp':
      case '.hwpx':
        return true;
      default:
        return false;
    }
  })();
  const isShare = state.shareInfo.hash !== '';

  const viewToken = req.viewToken;
  const typeHint = req.type || req.name;
  const isHeicOrHeif = req.type === 'image/heic' || req.type === 'image/heif';

  if (state.isSafari && isHeicOrHeif) {
    if (isShare) {
      return resourcesApi.getViewURL(
        req.source,
        req.path,
        viewToken,
        {
          path: state.shareInfo.subPath,
          hash: state.shareInfo.hash,
        },
        false,
        typeHint,
      );
    }
    return resourcesApi.getViewURL(req.source, req.path, viewToken, null, false, typeHint);
  }

  const getRawPreview = isRawImageMimeType(req.type);
  const getHeicPreview = isHeicOrHeif && globalVars.mediaAvailable && globalVars.enableHeicConversion;
  if (pdfConvertable || getRawPreview || getHeicPreview) {
    if (isShare) {
      const previewPath = removeTrailingSlash(req.path);
      return resourcesApi.getPreviewURLPublic(previewPath, 'original', req.viewToken);
    }
    return `${resourcesApi.getPreviewURL(req.source, req.path, req.modified)}&size=original`;
  }
  if (isShare) {
    return resourcesApi.getViewURL(
      req.source,
      req.path,
      viewToken,
      {
        path: state.shareInfo.subPath,
        hash: state.shareInfo.hash,
      },
      false,
      typeHint,
    );
  }
  return resourcesApi.getViewURL(req.source, req.path, viewToken, null, false, typeHint);
}

const FILES = [
  { name: 'photo.jpg', type: 'image/jpeg' },
  { name: 'PHOTO.PNG', type: 'image/png' },
  { name: 'anim.webp', type: 'image/webp' },
  { name: 'scan.tiff', type: 'image/tiff' },
  { name: 'phone.heic', type: 'image/heic' },
  { name: 'phone.heif', type: 'image/heif' },
  { name: 'raw.cr2', type: 'image/x-canon-cr2' },
  { name: 'raw.dng', type: 'image/x-adobe-dng' },
  { name: 'drawing.svg', type: 'image/svg+xml' },
  { name: 'comic.CBZ', type: 'application/zip' },
  { name: 'doc.docx', type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document' },
  { name: 'clip.mp4', type: 'video/mp4' },
  { name: 'song.flac', type: 'audio/flac' },
  { name: 'notes.txt', type: 'text/plain' },
  { name: 'untyped.jpg', type: '' },
];

function item(file, extra = {}) {
  return {
    source: 'media',
    path: `/albums/trip/${file.name}`,
    modified: '2026-10-01T12:00:00Z',
    viewToken: 'tok123',
    ...file,
    ...extra,
  };
}

describe('previewRawUrl', () => {
  beforeEach(() => {
    state.isSafari = false;
    state.shareInfo = { hash: '', subPath: '' };
    state.user = { disableViewingExt: [] };
    globalVars.muPdfAvailable = true;
    globalVars.mediaAvailable = true;
    globalVars.enableHeicConversion = true;
  });

  it('matches upstream raw() for every combination of context and file type', () => {
    let compared = 0;
    for (const share of [false, true]) {
      for (const safari of [false, true]) {
        for (const muPdf of [false, true]) {
          for (const heicConversion of [false, true]) {
            for (const disabled of [[], ['.svg', '.cbz']]) {
              for (const file of FILES) {
                for (const extra of [{}, { viewToken: undefined }, { path: `/albums/trip/${file.name}/` }]) {
                  state.shareInfo = share ? { hash: 'sharehash', subPath: '/trip' } : { hash: '', subPath: '' };
                  state.isSafari = safari;
                  state.user = { disableViewingExt: disabled };
                  globalVars.muPdfAvailable = muPdf;
                  globalVars.enableHeicConversion = heicConversion;
                  const req = item(file, extra);
                  expect(previewRawUrl(req), JSON.stringify({ share, safari, muPdf, heicConversion, disabled, req }))
                    .toBe(legacyRaw(req));
                  compared++;
                }
              }
            }
          }
        }
      }
    }
    expect(compared).toBe(2 * 2 * 2 * 2 * 2 * FILES.length * 3);
  });

  it('uses the plain view URL for an ordinary image', () => {
    expect(previewRawUrl(item(FILES[0]))).toBe(
      `view:${JSON.stringify(['media', '/albums/trip/photo.jpg', 'tok123', null, false, 'image/jpeg'])}`,
    );
  });

  it('passes the share context for share links', () => {
    state.shareInfo = { hash: 'sharehash', subPath: '/trip' };
    expect(previewRawUrl(item(FILES[0]))).toBe(
      `view:${JSON.stringify(['media', '/albums/trip/photo.jpg', 'tok123', { path: '/trip', hash: 'sharehash' }, false, 'image/jpeg'])}`,
    );
  });

  it('asks for the original-size conversion for RAW files', () => {
    expect(previewRawUrl(item(FILES[6]))).toBe(
      `preview:${JSON.stringify(['media', '/albums/trip/raw.cr2', '2026-10-01T12:00:00Z'])}&size=original`,
    );
  });
});

describe('isPdfConvertable', () => {
  beforeEach(() => {
    state.user = { disableViewingExt: [] };
    globalVars.muPdfAvailable = true;
  });

  it('accepts the MuPDF document types, case-insensitively', () => {
    expect(isPdfConvertable('book.EPUB')).toBe(false);
    expect(isPdfConvertable('comic.CBZ')).toBe(true);
    expect(isPdfConvertable('slides.pptx')).toBe(true);
  });

  it('is false without MuPDF, for disabled extensions and for a missing name', () => {
    expect(isPdfConvertable(undefined)).toBe(false);
    state.user = { disableViewingExt: ['.cbz'] };
    expect(isPdfConvertable('comic.cbz')).toBe(false);
    globalVars.muPdfAvailable = false;
    expect(isPdfConvertable('slides.pptx')).toBe(false);
  });
});

describe('isServerConvertedImage', () => {
  beforeEach(() => {
    state.isSafari = false;
    state.user = { disableViewingExt: [] };
    globalVars.muPdfAvailable = true;
    globalVars.mediaAvailable = true;
    globalVars.enableHeicConversion = true;
  });

  it('is false for images the browser loads as-is', () => {
    expect(isServerConvertedImage(item(FILES[0]))).toBe(false);
    expect(isServerConvertedImage(item(FILES[2]))).toBe(false);
  });

  it('is true for RAW, documents and converted HEIC', () => {
    expect(isServerConvertedImage(item(FILES[6]))).toBe(true);
    expect(isServerConvertedImage(item(FILES[9]))).toBe(true);
    expect(isServerConvertedImage(item(FILES[4]))).toBe(true);
  });

  it('is false for HEIC on Safari, which shows it natively', () => {
    state.isSafari = true;
    expect(isServerConvertedImage(item(FILES[4]))).toBe(false);
  });

  it('is false for HEIC when the server has no conversion', () => {
    globalVars.enableHeicConversion = false;
    expect(isServerConvertedImage(item(FILES[4]))).toBe(false);
  });

  it('is false for no item', () => {
    expect(isServerConvertedImage(null)).toBe(false);
  });
});
