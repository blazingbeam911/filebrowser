import { describe, expect, it } from 'vitest';
import { listingEdgeKey } from './listingEdgeKeys.js';

const ev = (key, extra = {}) => ({ key, target: document.body, ...extra });

describe('listingEdgeKey', () => {
  it('maps Home and End to the first and last item', () => {
    expect(listingEdgeKey(ev('Home'))).toBe('first');
    expect(listingEdgeKey(ev('End'))).toBe('last');
    expect(listingEdgeKey(ev('End', { ctrlKey: true }))).toBe('last');
  });

  it('ignores other keys and Shift/Alt/Meta combinations', () => {
    expect(listingEdgeKey(ev('PageDown'))).toBe(null);
    expect(listingEdgeKey(ev('Home', { shiftKey: true }))).toBe(null);
    expect(listingEdgeKey(ev('End', { altKey: true }))).toBe(null);
    expect(listingEdgeKey(ev('End', { metaKey: true }))).toBe(null);
  });

  it('leaves text fields alone', () => {
    const input = document.createElement('input');
    const textarea = document.createElement('textarea');
    expect(listingEdgeKey(ev('Home', { target: input }))).toBe(null);
    expect(listingEdgeKey(ev('End', { target: textarea }))).toBe(null);
    const range = document.createElement('input');
    range.type = 'range';
    expect(listingEdgeKey(ev('End', { target: range }))).toBe('last');
  });
});
