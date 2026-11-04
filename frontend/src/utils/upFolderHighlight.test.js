import { describe, expect, it } from 'vitest';
import { upFolderHighlightPath } from './upFolderHighlight.js';

describe('upFolderHighlightPath', () => {
  it('one level up selects the folder you came out of', () => {
    expect(upFolderHighlightPath('s', '/a/', 's', '/a/b/')).toBe('/a/b');
    expect(upFolderHighlightPath('s', '/a', 's', '/a/b')).toBe('/a/b');
  });

  it('several levels up selects the folder on the way down', () => {
    expect(upFolderHighlightPath('s', '/a/', 's', '/a/b/c/d/')).toBe('/a/b');
  });

  it('up to the source root', () => {
    expect(upFolderHighlightPath('s', '/', 's', '/a/b/')).toBe('/a');
    expect(upFolderHighlightPath('s', '', 's', '/a/')).toBe('/a');
  });

  it('coming from a file selects the file, or the folder that holds it', () => {
    expect(upFolderHighlightPath('s', '/a/', 's', '/a/pic.png')).toBe('/a/pic.png');
    expect(upFolderHighlightPath('s', '/a/', 's', '/a/b/pic.png')).toBe('/a/b');
  });

  it('keeps names with spaces and non-ASCII characters as they are', () => {
    expect(upFolderHighlightPath('s', '/x y/', 's', '/x y/写真 アルバム/z/')).toBe('/x y/写真 アルバム');
  });

  it('nothing when going down, sideways or reloading the same folder', () => {
    expect(upFolderHighlightPath('s', '/a/b/', 's', '/a/')).toBeNull();
    expect(upFolderHighlightPath('s', '/a/c/', 's', '/a/b/')).toBeNull();
    expect(upFolderHighlightPath('s', '/a/', 's', '/a/')).toBeNull();
    expect(upFolderHighlightPath('s', '/a/', 's', '/a')).toBeNull();
  });

  it('a name prefix is not an ancestor', () => {
    expect(upFolderHighlightPath('s', '/ab/', 's', '/abc/d/')).toBeNull();
  });

  it('nothing across sources or without a previous listing', () => {
    expect(upFolderHighlightPath('s', '/a/', 't', '/a/b/')).toBeNull();
    expect(upFolderHighlightPath('', '/a/', '', '/a/b/')).toBeNull();
    expect(upFolderHighlightPath('s', '/a/', undefined, undefined)).toBeNull();
    expect(upFolderHighlightPath('s', '/a/', 's', undefined)).toBeNull();
  });
});
