import { describe, expect, it } from 'vitest';
import { listingShowsDuration } from './durationColumn.js';

describe('listingShowsDuration', () => {
  it('shows the column as soon as there is a video or audio file, before any duration', () => {
    expect(listingShowsDuration([{ type: 'directory' }, { type: 'video/mp4' }])).toBe(true);
    expect(listingShowsDuration([{ type: 'audio/mpeg' }])).toBe(true);
  });

  it('shows it when a file already has a duration', () => {
    expect(listingShowsDuration([{ type: 'application/octet-stream', metadata: { duration: 12 } }])).toBe(true);
  });

  it('hides it for folders without audio or video', () => {
    expect(listingShowsDuration([{ type: 'directory' }, { type: 'image/jpeg' }, { type: 'text/plain' }])).toBe(false);
    expect(listingShowsDuration([{ type: 'directory', metadata: { duration: 5 } }])).toBe(false);
    expect(listingShowsDuration([])).toBe(false);
    expect(listingShowsDuration(undefined)).toBe(false);
    expect(listingShowsDuration([null, { type: 'image/png', metadata: { duration: 0 } }])).toBe(false);
  });
});
