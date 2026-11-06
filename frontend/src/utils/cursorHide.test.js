import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  CURSOR_HIDDEN_CLASS,
  IDLE_HIDE_MS,
  installCursorHide,
  isIdleHideTarget,
  isLoneModifierKey,
} from './cursorHide.js';

const hidden = () => document.documentElement.classList.contains(CURSOR_HIDDEN_CLASS);
const key = (k) => window.dispatchEvent(new KeyboardEvent('keydown', { key: k }));
const move = (screenX, screenY, clientX = screenX, clientY = screenY) =>
  window.dispatchEvent(new MouseEvent('mousemove', { screenX, screenY, clientX, clientY }));

describe('isLoneModifierKey', () => {
  it('is true for modifier and lock keys', () => {
    for (const k of ['Shift', 'Control', 'Alt', 'AltGraph', 'Meta', 'CapsLock', 'NumLock']) {
      expect(isLoneModifierKey(k)).toBe(true);
    }
  });

  it('is false for everything else', () => {
    for (const k of ['a', 'ArrowDown', 'PageDown', 'Enter', ' ', 'Escape', 'F5', 'Backspace']) {
      expect(isLoneModifierKey(k)).toBe(false);
    }
  });
});

describe('isIdleHideTarget', () => {
  beforeEach(() => {
    document.body.innerHTML = `
      <header><button id="hb">x</button></header>
      <div data-cursor-idle-hide><img id="img"><button id="b"><span id="bs">i</span></button>
        <div class="nav-button" id="nav"></div><a id="a" href="#">l</a></div>
      <div id="plain"></div>`;
  });

  it('is true inside an idle zone', () => {
    expect(isIdleHideTarget(document.getElementById('img'))).toBe(true);
  });

  it('is false over buttons, links, nav buttons and the top bar', () => {
    for (const id of ['b', 'bs', 'nav', 'a', 'hb']) {
      expect(isIdleHideTarget(document.getElementById(id))).toBe(false);
    }
  });

  it('is false outside an idle zone, or without an element', () => {
    expect(isIdleHideTarget(document.getElementById('plain'))).toBe(false);
    expect(isIdleHideTarget(null)).toBe(false);
  });
});

describe('installCursorHide', () => {
  let uninstall;
  let underMouse;

  beforeEach(() => {
    vi.useFakeTimers();
    document.body.innerHTML = '<div data-cursor-idle-hide><img id="img"></div><div id="plain"></div>';
    underMouse = document.getElementById('plain');
    document.elementFromPoint = () => underMouse;
    uninstall = installCursorHide(window);
  });

  afterEach(() => {
    uninstall();
    vi.useRealTimers();
    delete document.elementFromPoint;
  });

  it('hides on a key and shows again on real movement', () => {
    move(10, 10);
    key('ArrowDown');
    expect(hidden()).toBe(true);
    move(15, 10);
    expect(hidden()).toBe(false);
  });

  it('ignores mousemoves with unchanged screen coordinates', () => {
    move(10, 10);
    key('j');
    move(10, 10, 40, 40);
    expect(hidden()).toBe(true);
  });

  it('does not hide on a lone modifier', () => {
    key('Shift');
    key('Control');
    expect(hidden()).toBe(false);
  });

  it('shows again on a mouse button or the wheel', () => {
    key('a');
    window.dispatchEvent(new MouseEvent('mousedown', { clientX: 1, clientY: 1 }));
    expect(hidden()).toBe(false);
    key('a');
    window.dispatchEvent(new WheelEvent('wheel', { deltaY: 100 }));
    expect(hidden()).toBe(false);
  });

  it('idle-hides over an idle zone only', () => {
    move(10, 10);
    vi.advanceTimersByTime(IDLE_HIDE_MS + 10);
    expect(hidden()).toBe(false);

    underMouse = document.getElementById('img');
    move(11, 10);
    vi.advanceTimersByTime(IDLE_HIDE_MS - 10);
    expect(hidden()).toBe(false);
    vi.advanceTimersByTime(20);
    expect(hidden()).toBe(true);
  });

  it('restarts the idle timer on movement', () => {
    underMouse = document.getElementById('img');
    move(10, 10);
    vi.advanceTimersByTime(IDLE_HIDE_MS - 100);
    move(12, 10);
    vi.advanceTimersByTime(IDLE_HIDE_MS - 100);
    expect(hidden()).toBe(false);
    vi.advanceTimersByTime(200);
    expect(hidden()).toBe(true);
  });

  it('installs once and uninstalls cleanly', () => {
    expect(installCursorHide(window)).toBe(uninstall);
    key('a');
    uninstall();
    expect(hidden()).toBe(false);
    key('a');
    expect(hidden()).toBe(false);
    uninstall = installCursorHide(window);
  });
});
