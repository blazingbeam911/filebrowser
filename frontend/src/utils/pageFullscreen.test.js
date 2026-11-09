import { nextTick } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  escapeLeavesPageFullscreen,
  mediaMayFollow,
  pageFullscreen,
  setImageShowing,
  togglePageFullscreen,
} from '@/utils/pageFullscreen.js';
import { view } from '@/store/index.ts';

vi.mock('@/store/index.ts', async () => {
  const { reactive } = await import('vue');
  const current = reactive({ transitioning: false, name: 'preview', type: 'image' });
  return {
    view: current,
    state: { navigation: { get isTransitioning() { return current.transitioning; } } },
    getters: { currentView: () => current.name, previewType: () => current.type },
  };
});

function setFullscreenElement(element) {
  Object.defineProperty(document, 'fullscreenElement', { configurable: true, value: element });
  document.dispatchEvent(new Event('fullscreenchange'));
}

describe('page fullscreen', () => {
  const root = document.documentElement;

  beforeEach(() => {
    vi.useFakeTimers();
    root.requestFullscreen = vi.fn(() => Promise.resolve());
    document.exitFullscreen = vi.fn(() => Promise.resolve());
    setFullscreenElement(null);
    Object.assign(view, { transitioning: false, name: 'preview', type: 'image' });
    pageFullscreen.imageShowing = false;
  });

  afterEach(() => vi.useRealTimers());

  it('does nothing unless an image is showing', () => {
    togglePageFullscreen();
    expect(root.requestFullscreen).not.toHaveBeenCalled();
  });

  it('asks for the whole page and marks it', () => {
    setImageShowing(true);
    togglePageFullscreen();
    expect(root.requestFullscreen).toHaveBeenCalled();
    setFullscreenElement(root);
    expect(pageFullscreen.active).toBe(true);
    expect(root.classList.contains('fb-page-fullscreen')).toBe(true);
  });

  it('stays through a switch to a video', async () => {
    setFullscreenElement(root);
    view.transitioning = true;
    view.name = 'loading';
    await nextTick();
    Object.assign(view, { name: 'preview', type: 'video', transitioning: false });
    await nextTick();
    expect(document.exitFullscreen).not.toHaveBeenCalled();
  });

  it('leaves when the page moves to something else', () => {
    setFullscreenElement(root);
    Object.assign(view, { name: 'listingView', type: 'directory' });
    expect(document.exitFullscreen).toHaveBeenCalled();
  });

  it('leaves for audio', () => {
    setFullscreenElement(root);
    view.type = 'audio';
    expect(document.exitFullscreen).toHaveBeenCalled();
  });

  it('counts an image the file type does not reveal', () => {
    view.type = 'pdf';
    expect(mediaMayFollow()).toBe(false);
    setImageShowing(true);
    expect(mediaMayFollow()).toBe(true);
  });

  it('ignores fullscreen on another element', () => {
    setFullscreenElement(document.createElement('div'));
    expect(pageFullscreen.active).toBe(false);
    view.type = 'audio';
    expect(document.exitFullscreen).not.toHaveBeenCalled();
  });

  it('gives Escape to fullscreen just after leaving it', () => {
    setFullscreenElement(root);
    expect(escapeLeavesPageFullscreen()).toBe(true);
    setFullscreenElement(null);
    expect(escapeLeavesPageFullscreen()).toBe(true);
    vi.advanceTimersByTime(500);
    expect(escapeLeavesPageFullscreen()).toBe(false);
  });
});
