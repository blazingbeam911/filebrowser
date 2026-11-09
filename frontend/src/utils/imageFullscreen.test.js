import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  escapeLeavesImageFullscreen,
  imageFullscreen,
  setImageFullscreenAvailable,
  toggleImageFullscreen,
} from '@/utils/imageFullscreen.js';

function setFullscreenElement(element) {
  Object.defineProperty(document, 'fullscreenElement', { configurable: true, value: element });
  document.dispatchEvent(new Event('fullscreenchange'));
}

describe('image fullscreen', () => {
  const root = document.documentElement;

  beforeEach(() => {
    vi.useFakeTimers();
    root.requestFullscreen = vi.fn(() => Promise.resolve());
    document.exitFullscreen = vi.fn(() => Promise.resolve());
    setFullscreenElement(null);
    imageFullscreen.available = false;
  });

  afterEach(() => vi.useRealTimers());

  it('does nothing unless an image is showing', () => {
    toggleImageFullscreen();
    expect(root.requestFullscreen).not.toHaveBeenCalled();
  });

  it('asks for the whole page and marks it', () => {
    setImageFullscreenAvailable(true);
    toggleImageFullscreen();
    expect(root.requestFullscreen).toHaveBeenCalled();
    setFullscreenElement(root);
    expect(imageFullscreen.active).toBe(true);
    expect(root.classList.contains('fb-image-fullscreen')).toBe(true);
  });

  it('leaves when the image goes away', () => {
    setImageFullscreenAvailable(true);
    setFullscreenElement(root);
    setImageFullscreenAvailable(false);
    expect(document.exitFullscreen).toHaveBeenCalled();
  });

  it('ignores fullscreen that belongs to the video player', () => {
    setFullscreenElement(document.createElement('div'));
    expect(imageFullscreen.active).toBe(false);
    setImageFullscreenAvailable(false);
    expect(document.exitFullscreen).not.toHaveBeenCalled();
  });

  it('gives Escape to fullscreen just after leaving it', () => {
    setImageFullscreenAvailable(true);
    setFullscreenElement(root);
    expect(escapeLeavesImageFullscreen()).toBe(true);
    setFullscreenElement(null);
    expect(escapeLeavesImageFullscreen()).toBe(true);
    vi.advanceTimersByTime(500);
    expect(escapeLeavesImageFullscreen()).toBe(false);
  });
});
