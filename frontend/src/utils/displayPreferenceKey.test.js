import { describe, expect, it } from 'vitest';
import { displayPreferenceKey, displayPreferenceLookupKeys } from './displayPreferenceKey.js';

describe('displayPreferenceKey', () => {
  it('gives a folder the same key with or without a trailing slash', () => {
    expect(displayPreferenceKey('/files/src/a/b/')).toBe('/files/src/a/b/');
    expect(displayPreferenceKey('/files/src/a/b')).toBe('/files/src/a/b/');
    expect(displayPreferenceKey('/files/src/a/b//')).toBe('/files/src/a/b/');
  });

  it('leaves paths built by the app unchanged', () => {
    const name = "写真 (1) a&b+c'd";
    const built = `/files/src/${encodeURIComponent(name)}/`;
    expect(displayPreferenceKey(built)).toBe(built);
    expect(displayPreferenceKey(built.slice(0, -1))).toBe(built);
  });

  it('gives a typed, partly unescaped path the same key as the app-built one', () => {
    const typed = "/files/src/%E5%86%99%E7%9C%9F%20(1)%20a&b+c'd/";
    expect(displayPreferenceKey(typed)).toBe(`/files/src/${encodeURIComponent("写真 (1) a&b+c'd")}/`);
    expect(displayPreferenceKey('/files/src/a b')).toBe('/files/src/a%20b/');
  });

  it('keeps a malformed escape as it is', () => {
    expect(displayPreferenceKey('/files/src/100%/x')).toBe('/files/src/100%/x/');
  });

  it('root and empty input', () => {
    expect(displayPreferenceKey('/')).toBe('/');
    expect(displayPreferenceKey('')).toBeNull();
    expect(displayPreferenceKey(undefined)).toBeNull();
  });
});

describe('displayPreferenceLookupKeys', () => {
  it('canonical key first, then the old spellings', () => {
    expect(displayPreferenceLookupKeys('/files/src/a')).toEqual(['/files/src/a/', '/files/src/a']);
    expect(displayPreferenceLookupKeys('/files/src/a/')).toEqual(['/files/src/a/', '/files/src/a']);
    expect(displayPreferenceLookupKeys('/files/src/a&b/')).toEqual([
      '/files/src/a%26b/', '/files/src/a&b/', '/files/src/a&b',
    ]);
  });

  it('root and empty input', () => {
    expect(displayPreferenceLookupKeys('/')).toEqual(['/']);
    expect(displayPreferenceLookupKeys('')).toEqual([]);
  });
});
