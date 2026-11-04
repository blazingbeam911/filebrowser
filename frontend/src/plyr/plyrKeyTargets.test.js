import { describe, expect, it } from 'vitest';
import { PLYR_EDITABLE_SELECTOR, isTextEntryTarget } from '@/plyr/plyrKeyTargets.js';

const el = (html) => {
  const wrap = document.createElement('div');
  wrap.innerHTML = html;
  return wrap.firstElementChild;
};

describe('isTextEntryTarget', () => {
  it('keeps shortcuts on sliders such as the volume control', () => {
    expect(isTextEntryTarget(el('<input type="range" data-plyr="volume">'))).toBe(false);
  });

  it('gives way to text fields, text areas, selects and editable content', () => {
    expect(isTextEntryTarget(el('<input>'))).toBe(true);
    expect(isTextEntryTarget(el('<input type="text">'))).toBe(true);
    expect(isTextEntryTarget(el('<input type="search">'))).toBe(true);
    expect(isTextEntryTarget(el('<textarea></textarea>'))).toBe(true);
    expect(isTextEntryTarget(el('<select></select>'))).toBe(true);
    expect(isTextEntryTarget(el('<div contenteditable="true"></div>'))).toBe(true);
  });

  it('keeps shortcuts on buttons, the page and non-elements', () => {
    expect(isTextEntryTarget(el('<button type="button"></button>'))).toBe(false);
    expect(isTextEntryTarget(document.body)).toBe(false);
    expect(isTextEntryTarget(window)).toBe(false);
    expect(isTextEntryTarget(null)).toBe(false);
  });

  it('is the selector Plyr gets, so both agree', () => {
    expect(el('<input type="range">').matches(PLYR_EDITABLE_SELECTOR)).toBe(false);
    expect(el('<textarea></textarea>').matches(PLYR_EDITABLE_SELECTOR)).toBe(true);
  });
});
