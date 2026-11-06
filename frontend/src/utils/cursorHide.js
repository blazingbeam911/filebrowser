/**
 * Hides the mouse cursor while the keyboard is in use, and over idle images.
 *
 * - Any key hides it, except modifier and lock keys pressed on their own, so Shift+click and
 *   Ctrl+click keep a visible cursor.
 * - Moving the mouse, pressing a mouse button or turning the wheel shows it again. Browsers
 *   also send mousemove events when content moves under a still mouse (a new image, the
 *   listing scrolling to the selection); those have unchanged screen coordinates and are
 *   ignored.
 * - Over an element marked `data-cursor-idle-hide`, it also hides after IDLE_HIDE_MS without
 *   movement, unless the mouse is over a button, link, form field or the top bar.
 *
 * While hidden, the root element has the CURSOR_HIDDEN_CLASS class: styles.css hides the
 * cursor and hover effects check for it, so the row under a hidden cursor doesn't look
 * selected. The video player hides the cursor on its own, with its controls (styles.css).
 */

export const CURSOR_HIDDEN_CLASS = "fb-cursor-hidden";
export const IDLE_HIDE_MS = 3000;

const LONE_KEYS = new Set([
  "Shift",
  "Control",
  "Alt",
  "AltGraph",
  "Meta",
  "OS",
  "Super",
  "Hyper",
  "Fn",
  "FnLock",
  "CapsLock",
  "NumLock",
  "ScrollLock",
  "Symbol",
  "SymbolLock",
]);

const IDLE_ZONE_SELECTOR = "[data-cursor-idle-hide]";
const IDLE_EXEMPT_SELECTOR =
  'button, a, input, select, textarea, header, [role="button"], .nav-button';

/** True for keys that don't hide the cursor when pressed on their own. */
export function isLoneModifierKey(key) {
  return LONE_KEYS.has(key);
}

/** True when the cursor may idle-hide over this element. */
export function isIdleHideTarget(element) {
  if (!element || typeof element.closest !== "function") {
    return false;
  }
  return !!element.closest(IDLE_ZONE_SELECTOR) && !element.closest(IDLE_EXEMPT_SELECTOR);
}

/**
 * Starts listening on `win` (default: window). Returns a function that stops and shows the
 * cursor again. Installing twice on the same window does nothing more.
 */
export function installCursorHide(win = window, { idleMs = IDLE_HIDE_MS } = {}) {
  if (win.__fbCursorHide) {
    return win.__fbCursorHide;
  }
  const doc = win.document;
  let screenX = null;
  let screenY = null;
  let clientX = null;
  let clientY = null;
  let idleTimer = null;

  const setHidden = (hidden) => {
    doc.documentElement.classList.toggle(CURSOR_HIDDEN_CLASS, hidden);
  };

  const onIdle = () => {
    idleTimer = null;
    if (clientX === null || typeof doc.elementFromPoint !== "function") {
      return;
    }
    if (isIdleHideTarget(doc.elementFromPoint(clientX, clientY))) {
      setHidden(true);
    }
  };

  const restartIdle = () => {
    if (idleTimer !== null) {
      win.clearTimeout(idleTimer);
    }
    idleTimer = win.setTimeout(onIdle, idleMs);
  };

  const onKeydown = (event) => {
    if (!isLoneModifierKey(event.key)) {
      setHidden(true);
    }
  };

  const onMousemove = (event) => {
    clientX = event.clientX;
    clientY = event.clientY;
    if (event.screenX === screenX && event.screenY === screenY) {
      return;
    }
    screenX = event.screenX;
    screenY = event.screenY;
    setHidden(false);
    restartIdle();
  };

  const onMouseUse = (event) => {
    if (typeof event.clientX === "number") {
      clientX = event.clientX;
      clientY = event.clientY;
    }
    setHidden(false);
    restartIdle();
  };

  const options = { capture: true, passive: true };
  win.addEventListener("keydown", onKeydown, options);
  win.addEventListener("mousemove", onMousemove, options);
  win.addEventListener("mousedown", onMouseUse, options);
  win.addEventListener("wheel", onMouseUse, options);

  const uninstall = () => {
    win.removeEventListener("keydown", onKeydown, options);
    win.removeEventListener("mousemove", onMousemove, options);
    win.removeEventListener("mousedown", onMouseUse, options);
    win.removeEventListener("wheel", onMouseUse, options);
    if (idleTimer !== null) {
      win.clearTimeout(idleTimer);
    }
    setHidden(false);
    delete win.__fbCursorHide;
  };
  win.__fbCursorHide = uninstall;
  return uninstall;
}
