import { beforeEach, describe, expect, it } from "vitest";
import {
  PSD_VIEW_CAP,
  finishPsdFullSize,
  notePsdCappedImage,
  psdFullSize,
  psdFullSizeOffered,
  requestPsdFullSize,
  resetPsdFullSize,
} from "./psdFullSize.js";

describe("psdFullSize", () => {
  beforeEach(() => resetPsdFullSize());

  it("offers full size only when the capped image is exactly the cap on its long side", () => {
    resetPsdFullSize("a");
    notePsdCappedImage("a", PSD_VIEW_CAP, 2458);
    expect(psdFullSizeOffered()).toBe(true);

    resetPsdFullSize("b");
    notePsdCappedImage("b", 2458, PSD_VIEW_CAP); // portrait
    expect(psdFullSizeOffered()).toBe(true);

    resetPsdFullSize("c");
    notePsdCappedImage("c", 3007, 4095); // already full size
    expect(psdFullSizeOffered()).toBe(false);
  });

  it("ignores a load reported for another file", () => {
    resetPsdFullSize("new");
    notePsdCappedImage("old", PSD_VIEW_CAP, 100);
    expect(psdFullSizeOffered()).toBe(false);
  });

  it("goes loading → on, and hides the button once full size is shown", () => {
    resetPsdFullSize("a");
    notePsdCappedImage("a", PSD_VIEW_CAP, 100);
    requestPsdFullSize();
    expect(psdFullSize.loading).toBe(true);
    expect(psdFullSize.requests).toBeGreaterThan(0);
    const before = psdFullSize.requests;
    requestPsdFullSize(); // a second press while loading does nothing
    expect(psdFullSize.requests).toBe(before);
    finishPsdFullSize("a", true);
    expect(psdFullSize.loading).toBe(false);
    expect(psdFullSize.on).toBe(true);
    expect(psdFullSizeOffered()).toBe(false);
    // The full image's own load must not re-offer the button.
    notePsdCappedImage("a", PSD_VIEW_CAP, 100);
    expect(psdFullSizeOffered()).toBe(false);
  });

  it("keeps the button after a failure, and resets per file", () => {
    resetPsdFullSize("a");
    notePsdCappedImage("a", PSD_VIEW_CAP, 100);
    requestPsdFullSize();
    finishPsdFullSize("a", false);
    expect(psdFullSize.loading).toBe(false);
    expect(psdFullSizeOffered()).toBe(true);

    requestPsdFullSize();
    resetPsdFullSize("b"); // moved to the next file while loading
    finishPsdFullSize("a", true); // the old file's result arrives late
    expect(psdFullSize.on).toBe(false);
    expect(psdFullSize.loading).toBe(false);
    expect(psdFullSizeOffered()).toBe(false);
  });

  it("does nothing on a press when not offered", () => {
    resetPsdFullSize("a");
    const before = psdFullSize.requests;
    requestPsdFullSize();
    expect(psdFullSize.requests).toBe(before);
    expect(psdFullSize.loading).toBe(false);
  });
});
