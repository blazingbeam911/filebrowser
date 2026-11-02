import { beforeEach, describe, expect, it, vi } from "vitest";

const { store } = vi.hoisted(() => ({
  store: {
    state: {
      req: { name: "a.jpg", type: "image/jpeg", path: "/album/a.jpg" },
      prompts: [],
      navigation: { isTransitioning: false },
    },
  },
}));

vi.mock("@/store", () => ({
  state: store.state,
  mutations: {},
  getters: { fileViewingDisabled: () => false },
}));
vi.mock("@/utils/throttle", () => ({ default: (fn) => fn }));
vi.mock("@/utils/constants", () => ({ previewViews: ["preview"] }));
vi.mock("@/utils", () => ({ url: {} }));
vi.mock("@/api", () => ({ resourcesApi: {} }));
vi.mock("@/utils/playbackQueue.js", () => ({ navigatePlaybackQueue: vi.fn() }));
vi.mock("@/utils/navigationEdgeZones.js", () => ({}));
vi.mock("@/utils/imagePreloader.js", () => ({ setPreloadHold: vi.fn() }));
vi.mock("@/utils/previewRawUrl.js", () => ({ previewRawUrl: (req) => `url:${req.path}` }));
vi.mock("@/utils/navigationConfig.js", () => ({ navigationConfig: { sameCategory: false } }));

import NextPrevious from "./nextPrevious.vue";
import { endHold, imageHold, reportShown } from "@/utils/imageHold.js";

const { methods } = NextPrevious;

function keyEvent(key, { repeat = false, shiftKey = false, ctrlKey = false } = {}) {
  return {
    key, repeat, shiftKey, ctrlKey,
    preventDefault: vi.fn(),
    stopImmediatePropagation: vi.fn(),
  };
}

/** The component's `this`: computed values as plain fields, navigation as spies. */
function component(overrides = {}) {
  const ctx = {
    enabled: true,
    currentView: "preview",
    isMediaFile: false,
    isMediaQueueMode: false,
    hasNext: true,
    hasPrevious: true,
    next: vi.fn(),
    prev: vi.fn(),
    ...overrides,
  };
  ctx.handleHeldArrow = methods.handleHeldArrow.bind(ctx);
  return ctx;
}

const press = (ctx, event) => methods.keyEvent.call(ctx, event);

describe("nextPrevious: holding an arrow", () => {
  beforeEach(() => {
    endHold();
    reportShown(null, "none");
    store.state.req = { name: "a.jpg", type: "image/jpeg", path: "/album/a.jpg" };
    store.state.navigation.isTransitioning = false;
  });

  it("a single press moves as before, and doesn't start a hold", () => {
    const ctx = component();
    press(ctx, keyEvent("ArrowRight"));
    expect(ctx.next).toHaveBeenCalledTimes(1);
    expect(imageHold.active).toBe(false);
  });

  it("a held arrow waits for the full image, then moves", () => {
    const ctx = component();
    reportShown("url:/album/a.jpg", "thumbnail");
    press(ctx, keyEvent("ArrowRight", { repeat: true }));
    expect(ctx.next).not.toHaveBeenCalled();
    expect(imageHold).toMatchObject({ active: true, fast: false });

    reportShown("url:/album/a.jpg", "full");
    press(ctx, keyEvent("ArrowRight", { repeat: true }));
    expect(ctx.next).toHaveBeenCalledTimes(1);
  });

  it("a held Shift+arrow moves on once the thumbnail is shown", () => {
    const ctx = component();
    reportShown("url:/album/a.jpg", "thumbnail");
    press(ctx, keyEvent("ArrowLeft", { repeat: true, shiftKey: true }));
    expect(ctx.prev).toHaveBeenCalledTimes(1);
    expect(imageHold.fast).toBe(true);
  });

  it("never moves while the next file's metadata is loading or on another image's report", () => {
    const ctx = component();
    reportShown("url:/album/previous.jpg", "full");
    press(ctx, keyEvent("ArrowRight", { repeat: true }));
    reportShown("url:/album/a.jpg", "full");
    store.state.navigation.isTransitioning = true;
    press(ctx, keyEvent("ArrowRight", { repeat: true }));
    expect(ctx.next).not.toHaveBeenCalled();
  });

  it("stops at the end of the folder", () => {
    const ctx = component({ hasNext: false });
    reportShown("url:/album/a.jpg", "full");
    press(ctx, keyEvent("ArrowRight", { repeat: true }));
    expect(ctx.next).not.toHaveBeenCalled();
  });

  it("ignores repeats on files that aren't images, as before", () => {
    store.state.req = { name: "clip.mp4", type: "video/mp4", path: "/album/clip.mp4" };
    const ctx = component({ isMediaFile: true });
    reportShown("url:/album/clip.mp4", "full");
    press(ctx, keyEvent("ArrowRight", { repeat: true, ctrlKey: true }));
    expect(ctx.next).not.toHaveBeenCalled();
    expect(imageHold.active).toBe(false);
  });

  it("ignores repeats in the editor and in playback queue mode", () => {
    reportShown("url:/album/a.jpg", "full");
    for (const overrides of [{ currentView: "editor" }, { isMediaQueueMode: true }]) {
      const ctx = component(overrides);
      press(ctx, keyEvent("ArrowRight", { repeat: true }));
      expect(ctx.next).not.toHaveBeenCalled();
    }
  });

  it("does nothing while a prompt is open", () => {
    store.state.prompts = [{ name: "info" }];
    try {
      const ctx = component();
      reportShown("url:/album/a.jpg", "full");
      press(ctx, keyEvent("ArrowRight", { repeat: true }));
      expect(ctx.next).not.toHaveBeenCalled();
    } finally {
      store.state.prompts = [];
    }
  });

  describe("holdGuard (capture phase)", () => {
    it("swallows the rest of a hold that reached a video", () => {
      press(component(), keyEvent("ArrowRight", { repeat: true }));
      store.state.req = { name: "clip.mp4", type: "video/mp4", path: "/album/clip.mp4" };
      const event = keyEvent("ArrowRight", { repeat: true });
      methods.holdGuard(event);
      expect(event.stopImmediatePropagation).toHaveBeenCalled();
      expect(event.preventDefault).toHaveBeenCalled();
    });

    it("leaves keys alone on images, without a hold, and for new presses", () => {
      press(component(), keyEvent("ArrowRight", { repeat: true }));
      const onImage = keyEvent("ArrowRight", { repeat: true });
      methods.holdGuard(onImage);
      expect(onImage.stopImmediatePropagation).not.toHaveBeenCalled();

      store.state.req = { name: "clip.mp4", type: "video/mp4", path: "/album/clip.mp4" };
      const newPress = keyEvent("ArrowRight");
      methods.holdGuard(newPress);
      expect(newPress.stopImmediatePropagation).not.toHaveBeenCalled();

      endHold();
      const noHold = keyEvent("ArrowRight", { repeat: true });
      methods.holdGuard(noHold);
      expect(noHold.stopImmediatePropagation).not.toHaveBeenCalled();
    });
  });

  describe("keyUp", () => {
    it("releasing the arrow ends the hold", () => {
      press(component(), keyEvent("ArrowRight", { repeat: true, shiftKey: true }));
      methods.keyUp(keyEvent("ArrowRight"));
      expect(imageHold).toMatchObject({ active: false, fast: false });
    });

    it("releasing Shift turns a fast hold into a plain one", () => {
      press(component(), keyEvent("ArrowRight", { repeat: true, shiftKey: true }));
      methods.keyUp(keyEvent("Shift"));
      expect(imageHold).toMatchObject({ active: true, fast: false });
    });
  });
});
