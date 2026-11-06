<template>
  <div class="image-ex-container" ref="container" data-cursor-idle-hide @touchstart="touchStart" @touchmove.prevent="touchMove" @touchend="touchEnd" @dblclick="zoomAuto"
    @mousedown="mousedownStart" @mousemove="mouseMove" @mouseup="mouseUp" @wheel="wheelMove">
    <!-- Placeholder: the image's cached listing thumbnail, decoded first and scaled to the
         size the full image will have, shown while the full image loads. -->
    <img
      v-if="placeholderSrc && !isTiff"
      :src="placeholderSrc"
      class="image-ex-img image-ex-placeholder"
      ref="thumbnail"
      aria-hidden="true"
    />

    <!-- Loading spinner overlay (fades in only if loading takes a moment; not over the
         thumbnails a fast hold moves through) -->
    <div v-if="!fullImageLoaded && !(holdFast && placeholderSrc)" class="image-loading-overlay">
      <LoadingSpinner size="medium" />
    </div>

    <!-- Full image: the new image is downloaded and decoded off-screen, then put here, so the
         previous image (or the placeholder) stays on screen until it can paint at once. -->
    <img
      v-if="!isTiff"
      class="image-ex-img"
      ref="imgex"
      @load="onLoad"
      @error="onImageError"
      :style="{ visibility: placeholderSrc ? 'hidden' : 'visible' }"
      aria-hidden="true"
    />
    <canvas
      v-else
      ref="imgex"
      class="image-ex-img"
      :style="{ display: (cachedThumbnailUrl && !fullImageLoaded) ? 'none' : 'block' }"
    ></canvas>
  </div>
</template>

<script>
import LoadingSpinner from "@/components/LoadingSpinner.vue";
import { notify } from "@/notify";
import { getters, mutations, state } from '@/store';
import { globalVars } from "@/utils/constants";
import { getBestCachedImage } from "@/utils/imageCache";
import { imageHold, reportShown } from "@/utils/imageHold.js";
import { isImagePreloaded, pauseImagePreloads, resumeImagePreloads } from "@/utils/imagePreloader.js";
import { getTypeInfo, isRawImageMimeType } from "@/utils/mimetype";
import { largeThumbnailUrl } from "@/utils/previewRawUrl.js";
import { refreshViewToken, requestViewIdentity } from "@/api/viewToken.js";
import throttle from "@/utils/throttle";

/** How long the previous image stays up before the next one's thumbnail replaces it. */
const PLACEHOLDER_DELAY_MS = 150;

/**
 * Downloads and decodes `url` off-screen. Resolves either way, to whether it worked (for the
 * full image, errors surface on the visible <img> through onImageError).
 */
async function decodeOffscreen(url) {
  const img = new Image();
  img.decoding = "async";
  img.src = url;
  try {
    if (typeof img.decode === "function") {
      await img.decode();
      return true;
    }
    return await new Promise((resolve) => {
      img.onload = () => resolve(true);
      img.onerror = () => resolve(false);
    });
  } catch (_e) {
    return false;
  }
}

export default {
  components: {
    LoadingSpinner,
  },
  props: {
    src: {
      type: String,
      default: null,
    },
  },
  data() {
    return {
      moveDisabledTime: 200,
      classList: [],
      zoomStep: 0.25,
      scale: 1,
      lastX: null,
      lastY: null,
      inDrag: false,
      touches: 0,
      lastTouchDistance: 0,
      moveDisabled: false,
      disabledTimer: null,
      imageLoaded: false,
      fullImageLoaded: false,
      /** Cached thumbnail shown (scaled) while the full image loads; null when none. */
      placeholderSrc: null,
      placeholderTimer: null,
      /** Bumped per load so a slow, superseded load can't replace a newer image. */
      loadGeneration: 0,
      /** Zoom/pan reset waits until the new image (or its placeholder) is on screen. */
      viewResetPending: false,
      /** { generation, src } of a full image not downloaded yet because of a fast hold. */
      deferredFull: null,
      loadTimeout: null,
      position: {
        center: { x: 0, y: 0 },
        relative: { x: 0, y: 0 },
      },
      maxScale: 4,
      minScale: 1,
      isTiff: false,
      // Full-frame edge gestures (touch + mouse), zoom scale === 1 only
      edgeKind: null, // null | 'horizontal' | 'vertical-dismiss'
      edgeGestureActive: false, // true only when this touch started at scale 1
      edgeStartX: 0,
      edgeStartY: 0,
      edgeStartClientX: 0,
      edgeDx: 0,
      edgeDy: 0,
      dragOffsetX: 0,
      dragOffsetY: 0,
      gestureDecided: false,
      gestureSnapBack: false,
      edgeMouseActive: false,
      showNavHint: false,
      navHintDir: 'next', // 'prev' | 'next' — chevrons match nextPrevious.vue
      showDismissHint: false,
      dismissFlashActive: false,
      edgeHintPx: 44,
      edgeCommitX: 130,
      edgeCommitY: 110,
      edgeRubberMax: 100,
      pinchActive: false,
      viewTokenRetried: false,
      zoomTapStartX: 0,
      zoomTapStartY: 0,
      zoomTapMoved: false,
      zoomTapToggleTimer: null,
      zoomMouseMoved: false,
      zoomMouseStartX: 0,
      zoomMouseStartY: 0,
    };
  },
  computed: {
    source() {
      return state.req?.source;
    },
    path() {
      return state.req?.path;
    },
    isLoaded() {
      return !("preview-img" in (state?.loading || {}));
    },
    cachedThumbnailUrl() {
      if (!state?.req) {
        return null;
      }
      if (!this.path || !state.req.hasPreview) {
        return null;
      }
      // Don't use thumbnail when we show original embedded preview (HEIC/HEIF or raw)
      const isHeicOrHeif = state.req?.type === "image/heic" || state.req?.type === "image/heif";
      const useOriginalForHeic = isHeicOrHeif && (state.isSafari || (globalVars.mediaAvailable && globalVars.enableHeicConversion));
      const useOriginalForRaw = isRawImageMimeType(state.req?.type);
      if (useOriginalForHeic || useOriginalForRaw) {
        return null;
      }
      // Get cached thumbnail URL (prefers large, falls back to small)
      // For shares, use shareInfo.hash as the source; otherwise use this.source
      const source = getters.isShare() ? state.shareInfo?.hash : this.source;
      return getBestCachedImage(source, this.path, state.req?.modified);
    },
    /** A fast hold (Shift + held arrow) is moving through images: thumbnails are enough. */
    holdFast() {
      return imageHold.fast;
    },
    /** Large thumbnail shown during a fast hold; only for images (not converted documents). */
    holdThumbnailUrl() {
      if (!state?.req || getTypeInfo(state.req.type).simpleType !== "image") {
        return null;
      }
      return largeThumbnailUrl(state.req);
    },
    navigationGestureAllowed() {
      return state.navigation.enabled && getters.currentPrompt() === null;
    },
    hasImagePrevious() {
      return this.navigationGestureAllowed && state.navigation.previousLink !== '';
    },
    hasImageNext() {
      return this.navigationGestureAllowed && state.navigation.nextLink !== '';
    },
    navPrevCommitReady() {
      const ax = Math.abs(this.edgeDx);
      const ay = Math.abs(this.edgeDy);
      return (
        this.hasImagePrevious &&
        this.edgeDx >= this.edgeCommitX &&
        ax >= ay
      );
    },
    navNextCommitReady() {
      const ax = Math.abs(this.edgeDx);
      const ay = Math.abs(this.edgeDy);
      return (
        this.hasImageNext &&
        this.edgeDx <= -this.edgeCommitX &&
        ax >= ay
      );
    },
    dismissCommitReady() {
      const ax = Math.abs(this.edgeDx);
      const ay = Math.abs(this.edgeDy);
      return this.edgeDy >= this.edgeCommitY && ay >= ax;
    },
  },
  async mounted() {
    this.isTiff = this.checkIfTiff(this.src);

    // Step 1: Cache check happens automatically via thumbnailUrl computed property

    // Step 2: Always start loading the real image
    if (this.isTiff) {
      this.decodeTiff(this.src);
    }

    const container = this.$refs.container;
    for (const className of this.classList) {
      container.classList.add(className);
    }
    if (getComputedStyle(container).width === "0px") {
      container.style.width = "100%";
    }
    if (getComputedStyle(container).height === "0px") {
      container.style.height = "100%";
    }

    window.addEventListener("resize", this.onResize);

    if (!this.isTiff) {
      // Use nextTick to ensure element exists
      await this.$nextTick();
      this.loadFullImage();
    }
  },
  beforeUnmount() {
    this.loadGeneration++;
    this.deferredFull = null;
    this.clearPlaceholderTimer();
    pauseImagePreloads();
    // A load still in flight never reaches onLoad now: clear its global loading flag, or
    // the next view (e.g. the folder listing) would keep showing "Loading..." for good.
    mutations.setLoading("preview-img", false);
    // Clear any pending timeout
    if (this.loadTimeout) {
      clearTimeout(this.loadTimeout);
      this.loadTimeout = null;
    }
    if (this.disabledTimer) {
      clearTimeout(this.disabledTimer);
      this.disabledTimer = null;
    }
    this.cancelZoomTapNavToggle();
    this.teardownEdgeMouseListeners();
    mutations.setNavigationGestureHint({});
    window.removeEventListener("resize", this.onResize);
    document.removeEventListener("mouseup", this.onMouseUp);
  },
  methods: {
    syncNavigationGestureHintToStore() {
      let kind = null;
      let commitReady = false;
      let flashClose = false;
      if (this.dismissFlashActive) {
        kind = 'close';
        commitReady = this.dismissCommitReady;
        flashClose = true;
      } else if (this.showDismissHint) {
        kind = 'close';
        commitReady = this.dismissCommitReady;
      } else if (this.showNavHint && this.navHintDir === 'prev' && this.hasImagePrevious) {
        kind = 'previous';
        commitReady = this.navPrevCommitReady;
      } else if (this.showNavHint && this.navHintDir === 'next' && this.hasImageNext) {
        kind = 'next';
        commitReady = this.navNextCommitReady;
      }
      mutations.setNavigationGestureHint({ kind, commitReady, flashClose });
    },
    cancelZoomTapNavToggle() {
      if (this.zoomTapToggleTimer) {
        clearTimeout(this.zoomTapToggleTimer);
        this.zoomTapToggleTimer = null;
      }
    },
    scheduleZoomTapNavToggle() {
      this.cancelZoomTapNavToggle();
      this.zoomTapToggleTimer = setTimeout(() => {
        this.zoomTapToggleTimer = null;
        if (this.navigationGestureAllowed) {
          mutations.toggleNavigationChrome();
        }
      }, 300);
    },
    tryToggleNavOnZoomedTap(startX, startY, endX, endY) {
      const ax = Math.abs(endX - startX);
      const ay = Math.abs(endY - startY);
      if (ax < this.edgeHintPx && ay < this.edgeHintPx) {
        this.scheduleZoomTapNavToggle();
      }
    },
    async loadFullImage() {
      if (!this.src) return;
      mutations.setLoading("preview-img", true);
      const generation = ++this.loadGeneration;
      this.clearPlaceholderTimer();
      this.deferredFull = null;
      reportShown(this.src, "none");

      // Set src directly via JavaScript to avoid Vue's HTML entity encoding in template bindings
      // Vue HTML-encodes & to &amp; when using :src="src" in templates
      // Decode any HTML entities (Vue shouldn't encode props, but decode just in case)
      const cleanSrc = String(this.src).replace(/&amp;/g, '&');
      // No new preloads compete with this image; resumed in onLoad / finishImageError.
      pauseImagePreloads(cleanSrc);

      if (imageHold.fast && this.holdThumbnailUrl && !isImagePreloaded(cleanSrc)) {
        // Fast hold: show the large thumbnail straight away and leave the full image until
        // the hold ends (or the thumbnail fails), so passing images costs no full downloads.
        this.deferredFull = { generation, src: cleanSrc };
        void this.showHoldThumbnail(generation);
        return;
      }

      const imgex = this.$refs.imgex;
      const somethingOnScreen = Boolean(imgex?.getAttribute?.('src')) || this.placeholderSrc !== null;
      const thumbnail = this.cachedThumbnailUrl;
      if (thumbnail && !isImagePreloaded(cleanSrc)) {
        // With nothing on screen, show the thumbnail as soon as it's decoded. Otherwise keep
        // the previous image a moment longer: on a fast connection the full image arrives
        // first and the thumbnail never needs to flash up.
        const delay = somethingOnScreen ? PLACEHOLDER_DELAY_MS : 0;
        this.placeholderTimer = setTimeout(() => {
          this.placeholderTimer = null;
          void this.showPlaceholder(thumbnail, generation);
        }, delay);
      }

      if (!somethingOnScreen && !thumbnail) {
        // First image and no thumbnail: let it render progressively as it downloads.
        await this.$nextTick();
        if (generation === this.loadGeneration) {
          this.showFullImage(cleanSrc);
        }
        return;
      }

      await this.fetchAndShowFull(cleanSrc, generation);
    },
    async fetchAndShowFull(cleanSrc, generation) {
      // Download and decode off-screen, then hand the URL to the visible <img>: the browser
      // reuses the decoded image, so it paints in one go and onLoad follows. A preloaded
      // image is already downloaded and decoded; decoding it again could only wait behind
      // whatever the preloader is decoding next.
      if (!isImagePreloaded(cleanSrc)) {
        await decodeOffscreen(cleanSrc);
      }
      await this.$nextTick();
      if (generation !== this.loadGeneration) return;
      this.showFullImage(cleanSrc);
    },
    /** Starts the full image a fast hold left waiting (hold ended, or thumbnail failed). */
    startDeferredFull() {
      const deferred = this.deferredFull;
      this.deferredFull = null;
      if (!deferred || deferred.generation !== this.loadGeneration) return;
      void this.fetchAndShowFull(deferred.src, deferred.generation);
    },
    /**
     * Fast hold: shows the large thumbnail at once and reports it, so the hold can move on.
     * If it fails (e.g. a format the browser can't show), the full image is loaded instead
     * and the hold waits for that.
     */
    async showHoldThumbnail(generation) {
      const src = this.src;
      const thumbnail = this.holdThumbnailUrl;
      const ok = thumbnail ? await decodeOffscreen(thumbnail) : false;
      if (generation !== this.loadGeneration || this.fullImageLoaded) return;
      if (!ok) {
        this.startDeferredFull();
        return;
      }
      this.clearPlaceholderTimer();
      this.placeholderSrc = thumbnail;
      this.applyPendingViewReset();
      await this.$nextTick();
      if (generation !== this.loadGeneration || this.fullImageLoaded) return;
      reportShown(src, "thumbnail");
    },
    /** Puts `url` in the visible <img>; onLoad / onImageError follow. */
    showFullImage(url) {
      const img = this.$refs.imgex;
      if (!img || !('src' in img)) return;
      if (img.getAttribute('src') === url && img.complete && img.naturalWidth > 0) {
        // Already showing this image (e.g. moved away and straight back before the other
        // image arrived): setting the same src fires no load event, so finish here.
        this.onLoad();
        return;
      }
      img.src = url;
    },
    async showPlaceholder(thumbnail, generation) {
      const src = this.src;
      await decodeOffscreen(thumbnail);
      if (generation !== this.loadGeneration || this.fullImageLoaded) return;
      this.placeholderSrc = thumbnail;
      this.applyPendingViewReset();
      reportShown(src, "thumbnail");
    },
    clearPlaceholderTimer() {
      if (this.placeholderTimer) {
        clearTimeout(this.placeholderTimer);
        this.placeholderTimer = null;
      }
    },
    applyPendingViewReset() {
      if (!this.viewResetPending) return;
      this.viewResetPending = false;
      this.scale = 1;
      this.position.relative = { x: 0, y: 0 };
      this.resetEdgeGestureImmediate();
      this.applyImgTransform();
    },
    onLoad() {
      // Real image loaded: drop the placeholder and show the image
      this.clearPlaceholderTimer();
      this.placeholderSrc = null;
      this.applyPendingViewReset();
      resumeImagePreloads();
      this.imageLoaded = true;
      this.fullImageLoaded = true;
      // Clear the timeout if image loaded successfully
      if (this.loadTimeout) {
        clearTimeout(this.loadTimeout);
        this.loadTimeout = null;
      }
      mutations.setLoading("preview-img", false);
      reportShown(this.src, "full");
      // fullImageLoaded flips display from none → block via :style. Vue hasn't
      // updated the DOM yet, so setCenter() in the same tick sees clientWidth/Height 0.
      // Defer until after layout so centering / transform apply correctly.
      this.scheduleSetCenter();
    },
    onImageError(event) {
      const img = event.target;
      const actualSrc = img?.src || '';

      // If the error is due to &amp; in URL, try to fix it
      if (actualSrc?.includes('&amp;')) {
        const fixedSrc = actualSrc.replace(/&amp;/g, '&');
        if (img) {
          img.src = fixedSrc;
          return; // Let it retry with fixed URL
        }
      }

      if (!this.viewTokenRetried && state.req?.source) {
        this.viewTokenRetried = true;
        const viewIdentity = requestViewIdentity(state.req);
        void refreshViewToken(state.req.source, state.req.viewToken)
          .then((data) => {
            if (requestViewIdentity(state.req) !== viewIdentity) return;
            if (data?.viewToken && data.viewToken !== state.req.viewToken) {
              // New token, new URL: the src watcher loads the image again.
              mutations.setRequestViewToken(data.viewToken);
            } else {
              // Same token (tokens are reused per scope): the URL doesn't change and nothing
              // would retry, so the image itself is broken. Without this the spinner stayed
              // up for good (and a held arrow would wait on it forever).
              this.finishImageError(img);
            }
          })
          .catch(() => {
            this.finishImageError(img);
          });
        return;
      }

      this.finishImageError(img);
    },
    finishImageError(img) {
      this.clearPlaceholderTimer();
      this.placeholderSrc = null;
      this.applyPendingViewReset();
      resumeImagePreloads();
      this.imageLoaded = true;
      this.fullImageLoaded = true;
      mutations.setLoading("preview-img", false);
      // Failed counts as done for a held arrow: it moves on instead of getting stuck.
      reportShown(this.src, "full");
      if (img) {
        img.style.display = 'block';
      }
      this.scheduleSetCenter();
    },
    checkIfTiff(src) {
      const sufs = ["tif", "tiff", "dng", "cr2", "nef"];
      const suff = src.split(".").pop().toLowerCase();
      return sufs.includes(suff);
    },
    async decodeTiff(src) {
      try {
        const response = await fetch(src);
        if (!response.ok) {
          throw new Error("Network response was not ok");
        }
        const blob = await response.blob();
        const imgex = this.$refs.imgex;
        if (imgex) {
          imgex.src = URL.createObjectURL(blob);
          imgex.onload = () => URL.revokeObjectURL(imgex.src);
        }
      } catch (_e) {
        notify.showError("Error decoding TIFF");
      }
    },
    onMouseUp() {
      this.inDrag = false;
    },
    async scheduleSetCenter() {
      await this.$nextTick();
      requestAnimationFrame(() => {
        this.setCenter();
        const img = this.$refs.imgex;
        const container = this.$refs.container;
        if (
          img &&
          this.fullImageLoaded &&
          (!img.clientWidth || !img.clientHeight)
        ) {
          requestAnimationFrame(() => this.setCenter());
        }
        // Force layout + compositor to pick up the decoded bitmap. Without this,
        // WebKit/Blink sometimes leave the promoted layer black until display/position
        // is toggled in DevTools or the window is resized.
        requestAnimationFrame(() => {
          if (!img || !this.fullImageLoaded) {
            return;
          }
          void container?.offsetHeight;
          void img.offsetHeight;
          const prevOp = img.style.opacity;
          img.style.opacity = '0.9999';
          requestAnimationFrame(() => {
            img.style.opacity = prevOp;
          });
        });
      });
    },
    onResize: throttle(function () {
      if (this.imageLoaded) {
        this.setCenter();
      }
    }, 100),
    rubberband(value, max) {
      const sign = value < 0 ? -1 : 1;
      const a = Math.abs(value);
      if (a <= max) {
        return value;
      }
      return sign * (max + (a - max) * 0.32);
    },
    applyImgTransform() {
      const img = this.$refs.imgex;
      if (!img) {
        return;
      }
      const transition = this.gestureSnapBack
        ? 'transform 0.22s cubic-bezier(0.32, 0.72, 0, 1)'
        : 'none';
      img.style.transition = transition;
      if (this.scale === 1) {
        img.style.transform = `translate3d(calc(-50% + ${this.dragOffsetX}px), calc(-50% + ${this.dragOffsetY}px), 0) scale(1)`;
      } else {
        const { x: rx, y: ry } = this.position.relative;
        img.style.transform = `translate3d(calc(-50% + ${rx}px), calc(-50% + ${ry}px), 0) scale(${this.scale})`;
      }
    },
    decideEdgeKind() {
      if (this.edgeKind) {
        return;
      }
      const ax = Math.abs(this.edgeDx);
      const ay = Math.abs(this.edgeDy);
      if (ax < 12 && ay < 12) {
        return;
      }
      if (this.edgeDy > ax * 1.12 && this.edgeDy > 14) {
        this.edgeKind = 'vertical-dismiss';
        this.gestureDecided = true;
      } else if (ax > ay * 1.12 && ax > 14) {
        this.edgeKind = 'horizontal';
        this.gestureDecided = true;
      }
    },
    applyEdgeVisuals() {
      if (!this.edgeKind) {
        const ax = Math.abs(this.edgeDx);
        const ay = Math.abs(this.edgeDy);
        if (ax <= 8 && ay <= 8) {
          this.dragOffsetX = 0;
          this.dragOffsetY = 0;
          this.showNavHint = false;
          this.showDismissHint = false;
          this.applyImgTransform();
          this.syncNavigationGestureHintToStore();
          return;
        }
        if (ax > ay) {
          this.dragOffsetX = this.rubberband(this.edgeDx, this.edgeRubberMax);
          this.dragOffsetY = 0;
          this.showNavHint = ax >= this.edgeHintPx;
          this.navHintDir = this.edgeDx > 0 ? 'prev' : 'next';
          if (this.navHintDir === 'prev' && !this.hasImagePrevious) {
            this.showNavHint = false;
          }
          if (this.navHintDir === 'next' && !this.hasImageNext) {
            this.showNavHint = false;
          }
          this.showDismissHint = false;
        } else {
          this.dragOffsetX = 0;
          const downward = this.edgeDy > 0 ? this.edgeDy : 0;
          this.dragOffsetY = this.rubberband(downward, this.edgeRubberMax);
          this.showDismissHint = this.edgeDy >= this.edgeHintPx;
          this.showNavHint = false;
        }
        this.applyImgTransform();
        this.syncNavigationGestureHintToStore();
        return;
      }
      if (this.edgeKind === 'horizontal') {
        this.dragOffsetX = this.rubberband(this.edgeDx, this.edgeRubberMax);
        this.dragOffsetY = 0;
        const adx = Math.abs(this.edgeDx);
        this.showNavHint = adx >= this.edgeHintPx;
        this.navHintDir = this.edgeDx > 0 ? 'prev' : 'next';
        if (this.navHintDir === 'prev' && !this.hasImagePrevious) {
          this.showNavHint = false;
        }
        if (this.navHintDir === 'next' && !this.hasImageNext) {
          this.showNavHint = false;
        }
        this.showDismissHint = false;
      } else {
        this.dragOffsetX = 0;
        const downward = this.edgeDy > 0 ? this.edgeDy : 0;
        this.dragOffsetY = this.rubberband(downward, this.edgeRubberMax);
        this.showDismissHint = this.edgeDy >= this.edgeHintPx;
        this.showNavHint = false;
      }
      this.applyImgTransform();
      this.syncNavigationGestureHintToStore();
    },
    snapBackEdgeGesture() {
      this.gestureSnapBack = true;
      this.dragOffsetX = 0;
      this.dragOffsetY = 0;
      this.showNavHint = false;
      this.showDismissHint = false;
      this.edgeKind = null;
      this.gestureDecided = false;
      this.edgeDx = 0;
      this.edgeDy = 0;
      this.applyImgTransform();
      mutations.setNavigationGestureHint({});
      setTimeout(() => {
        this.gestureSnapBack = false;
        this.applyImgTransform();
      }, 240);
    },
    resetEdgeGestureImmediate() {
      this.edgeKind = null;
      this.edgeGestureActive = false;
      this.gestureDecided = false;
      this.edgeDx = 0;
      this.edgeDy = 0;
      this.dragOffsetX = 0;
      this.dragOffsetY = 0;
      this.showNavHint = false;
      this.showDismissHint = false;
      this.gestureSnapBack = false;
      this.dismissFlashActive = false;
      mutations.setNavigationGestureHint({});
    },
    finishEdgeGesture() {
      if (this.scale !== 1) {
        this.resetEdgeGestureImmediate();
        this.applyImgTransform();
        return;
      }
      let kind = this.edgeKind;
      if (!kind) {
        const ax = Math.abs(this.edgeDx);
        const ay = Math.abs(this.edgeDy);
        if (ax < this.edgeHintPx && ay < this.edgeHintPx) {
          if (this.navigationGestureAllowed) {
            mutations.toggleNavigationChrome();
          }
          this.snapBackEdgeGesture();
          return;
        }
        kind = ax >= ay ? 'horizontal' : 'vertical-dismiss';
      }
      if (kind === 'horizontal') {
        if (this.edgeDx >= this.edgeCommitX && this.hasImagePrevious) {
          this.$emit('navigate-previous');
          this.resetEdgeGestureImmediate();
          this.applyImgTransform();
          return;
        }
        if (this.edgeDx <= -this.edgeCommitX && this.hasImageNext) {
          this.$emit('navigate-next');
          this.resetEdgeGestureImmediate();
          this.applyImgTransform();
          return;
        }
      } else if (kind === 'vertical-dismiss') {
        if (this.edgeDy >= this.edgeCommitY) {
          this.dismissFlashActive = true;
          this.showDismissHint = true;
          this.dragOffsetX = 0;
          this.dragOffsetY = 0;
          this.edgeKind = null;
          this.gestureDecided = false;
          this.applyImgTransform();
          this.syncNavigationGestureHintToStore();
          setTimeout(() => {
            this.$emit('close-preview');
          }, 120);
          setTimeout(() => {
            this.dismissFlashActive = false;
            this.showDismissHint = false;
            mutations.setNavigationGestureHint({});
          }, 420);
          return;
        }
      }
      this.snapBackEdgeGesture();
    },
    teardownEdgeMouseListeners() {
      document.removeEventListener('mousemove', this.onEdgeMouseMove, true);
      document.removeEventListener('mouseup', this.onEdgeMouseUp, true);
      this.edgeMouseActive = false;
    },
    onEdgeMouseMove(event) {
      if (!this.edgeMouseActive || this.scale !== 1) {
        return;
      }
      this.edgeDx = event.clientX - this.edgeStartX;
      this.edgeDy = event.clientY - this.edgeStartY;
      this.decideEdgeKind();
      this.applyEdgeVisuals();
      event.preventDefault();
    },
    onEdgeMouseUp(event) {
      if (!this.edgeMouseActive) {
        return;
      }
      this.teardownEdgeMouseListeners();
      this.edgeDx = event.clientX - this.edgeStartX;
      this.edgeDy = event.clientY - this.edgeStartY;
      this.finishEdgeGesture();
      event.preventDefault();
    },
    setCenter() {
      const container = this.$refs.container;
      const img = this.$refs.imgex;
      if (!container || !img?.clientWidth || !img?.clientHeight) {
        return;
      }
      this.position.relative = { x: 0, y: 0 };
      this.position.center.x = Math.floor((container.clientWidth - img.clientWidth) / 2);
      this.position.center.y = Math.floor((container.clientHeight - img.clientHeight) / 2);
      this.resetEdgeGestureImmediate();
      this.applyImgTransform();
    },
    mousedownStart(event) {
      if (event.button !== 0) return;
      if (this.scale === 1) {
        this.teardownEdgeMouseListeners();
        this.edgeMouseActive = true;
        this.edgeStartX = event.clientX;
        this.edgeStartY = event.clientY;
        this.edgeStartClientX = event.clientX;
        this.edgeDx = 0;
        this.edgeDy = 0;
        this.edgeKind = null;
        this.gestureDecided = false;
        document.addEventListener('mousemove', this.onEdgeMouseMove, true);
        document.addEventListener('mouseup', this.onEdgeMouseUp, true);
        event.preventDefault();
        return;
      }
      this.lastX = null;
      this.lastY = null;
      this.inDrag = true;
      this.zoomMouseMoved = false;
      this.cancelZoomTapNavToggle();
      this.zoomMouseStartX = event.clientX;
      this.zoomMouseStartY = event.clientY;
      event.preventDefault();
    },
    mouseMove(event) {
      if (event.button !== 0) return;
      if (this.scale > 1 && this.inDrag) {
        if (event.movementX !== 0 || event.movementY !== 0) {
          this.zoomMouseMoved = true;
        }
        this.doMove(event.movementX, event.movementY);
        event.preventDefault();
      }
    },
    mouseUp(event) {
      if (event.button !== 0) return;
      if (this.scale > 1) {
        if (!this.zoomMouseMoved) {
          this.tryToggleNavOnZoomedTap(
            this.zoomMouseStartX,
            this.zoomMouseStartY,
            event.clientX,
            event.clientY,
          );
        }
        this.inDrag = false;
        event.preventDefault();
      }
    },
    touchStart(event) {
      this.lastX = null;
      this.lastY = null;
      this.lastTouchDistance = null;
      if (event.targetTouches.length >= 2) {
        this.pinchActive = true;
        this.resetEdgeGestureImmediate();
        this.applyImgTransform();
        event.preventDefault();
        return;
      }
      if (event.targetTouches.length === 1 && this.scale === 1) {
        const touch = event.targetTouches[0];
        this.edgeGestureActive = true;
        this.edgeStartX = touch.pageX;
        this.edgeStartY = touch.pageY;
        this.edgeStartClientX = touch.clientX;
        this.edgeDx = 0;
        this.edgeDy = 0;
        this.edgeKind = null;
        this.gestureDecided = false;
      } else if (event.targetTouches.length === 1 && this.scale > 1) {
        this.cancelZoomTapNavToggle();
        const touch = event.targetTouches[0];
        this.zoomTapStartX = touch.pageX;
        this.zoomTapStartY = touch.pageY;
        this.zoomTapMoved = false;
      } else {
        this.resetEdgeGestureImmediate();
        this.applyImgTransform();
      }
      // Sequential single-finger taps within 300ms → double-tap zoom
      // (@dblclick is unreliable on mobile because touchend preventDefault blocks click synthesis)
      if (event.targetTouches.length === 1) {
        setTimeout(() => {
          this.touches = 0;
        }, 300);
        this.touches++;
        if (this.touches > 1) {
          this.cancelZoomTapNavToggle();
          // zoomAuto may set scale back to 1 before touchend; do not treat that
          // touchend as an edge swipe (stale edgeStart would navigate).
          this.edgeGestureActive = false;
          this.zoomAuto(event);
          event.preventDefault();
        }
      }
      if (this.scale > 1 || event.targetTouches.length >= 2) {
        event.preventDefault();
      }
    },
    zoomAuto(event) {
      switch (this.scale) {
        case 1:
          this.scale = 2;
          break;
        case 2:
          this.scale = 4;
          break;
        default:
          this.scale = 1;
          this.setCenter();
          break;
      }
      this.setZoom();
      event.preventDefault();
    },
    touchMove(event) {
      // Default is prevented via @touchmove.prevent so the browser cannot steal
      // vertical drags (pull-to-refresh) before our edge-gesture thresholds.
      if (event.targetTouches.length === 2) {
        if (!this.pinchActive) {
          this.pinchActive = true;
          this.resetEdgeGestureImmediate();
          this.applyImgTransform();
        }
        this.moveDisabled = true;
        clearTimeout(this.disabledTimer);
        this.disabledTimer = setTimeout(() => (this.moveDisabled = false), this.moveDisabledTime);

        const p1 = event.targetTouches[0];
        const p2 = event.targetTouches[1];
        const touchDistance = Math.sqrt((p2.pageX - p1.pageX) ** 2 + (p2.pageY - p1.pageY) ** 2);

        if (!this.lastTouchDistance) {
          this.lastTouchDistance = touchDistance;
          return;
        }
        let newScale = this.scale + (touchDistance - this.lastTouchDistance) / (this.$refs.imgex.width / 5);
        newScale = Math.max(this.minScale, Math.min(this.maxScale, newScale));
        if (newScale !== this.scale) {
          this.scale = newScale;
        }
        this.lastTouchDistance = touchDistance;
        this.setZoom();
        return;
      }
      if (event.targetTouches.length === 1 && this.scale === 1 && !this.pinchActive) {
        const touch = event.targetTouches[0];
        this.edgeDx = touch.pageX - this.edgeStartX;
        this.edgeDy = touch.pageY - this.edgeStartY;
        this.decideEdgeKind();
        this.applyEdgeVisuals();
        return;
      }
      if (event.targetTouches.length === 1 && this.scale > 1) {
        if (this.moveDisabled) return;
        const touch = event.targetTouches[0];
        const ax = Math.abs(touch.pageX - this.zoomTapStartX);
        const ay = Math.abs(touch.pageY - this.zoomTapStartY);
        if (ax > 10 || ay > 10) {
          this.zoomTapMoved = true;
        }
        if (this.lastX === null) {
          this.lastX = touch.pageX;
          this.lastY = touch.pageY;
          return;
        }
        const x = touch.pageX - this.lastX;
        const y = touch.pageY - this.lastY;
        this.lastX = touch.pageX;
        this.lastY = touch.pageY;
        this.doMove(x, y);
      }
    },
    doMove(x, y) {
      this.position.relative.x += x;
      this.position.relative.y += y;
      this.applyImgTransform();
    },
    wheelMove(event) {
      event.preventDefault()
      this.scale += -Math.sign(event.deltaY) * this.zoomStep;
      this.setZoom();
    },
    setZoom() {
      this.scale = Math.max(this.minScale, Math.min(this.maxScale, this.scale));
      if (this.scale === 1) {
        this.position.relative = { x: 0, y: 0 };
        this.resetEdgeGestureImmediate();
      }
      this.applyImgTransform();
    },
    pxStringToNumber(style) {
      return +style.replace("px", "");
    },
    touchEnd(event) {
      if (this.pinchActive) {
        if (event.touches.length === 0) {
          this.pinchActive = false;
          this.resetEdgeGestureImmediate();
          this.applyImgTransform();
        }
        event.preventDefault();
        return;
      }
      // Only finish edge gestures that started at scale 1. Double-tap zoom-out
      // sets scale to 1 on touchstart; without this guard touchend would treat
      // stale edgeStart coords as a swipe and navigate.
      if (this.scale === 1 && this.edgeGestureActive && event.changedTouches.length > 0) {
        const t = event.changedTouches[0];
        this.edgeDx = t.pageX - this.edgeStartX;
        this.edgeDy = t.pageY - this.edgeStartY;
        this.finishEdgeGesture();
        event.preventDefault();
      } else if (this.scale > 1 && event.changedTouches.length > 0) {
        if (!this.zoomTapMoved) {
          const t = event.changedTouches[0];
          this.tryToggleNavOnZoomedTap(
            this.zoomTapStartX,
            this.zoomTapStartY,
            t.pageX,
            t.pageY,
          );
        }
        event.preventDefault();
      }
      this.edgeGestureActive = false;
    },
  },
  watch: {
    holdFast(fast) {
      if (!fast) {
        // Fast hold over: the image it stopped on loads in full.
        this.startDeferredFull();
        return;
      }
      // A fast hold began while this image is still loading in full with nothing of it on
      // screen yet: show its thumbnail too, so the hold needn't wait for the full image.
      if (!this.fullImageLoaded && this.placeholderSrc === null && this.holdThumbnailUrl) {
        void this.showHoldThumbnail(this.loadGeneration);
      }
    },
    src: async function (newSrc) {
      if (!newSrc) {
        mutations.setLoading("preview-img", false);
        return;
      }

      // Clear any existing timeout
      if (this.loadTimeout) {
        clearTimeout(this.loadTimeout);
        this.loadTimeout = null;
      }

      // Reset and reload when src changes
      this.viewTokenRetried = false;
      this.fullImageLoaded = false;
      this.imageLoaded = false;
      this.isTiff = this.checkIfTiff(newSrc);

      // Cache check happens automatically via thumbnailUrl computed property

      // Always load the real image
      if (this.isTiff) {
        this.decodeTiff(newSrc);
      }

      // Zoom/pan reset happens when the new image (or its placeholder) appears, so the
      // current image doesn't jump while the next one loads. TIFF swaps immediately.
      this.viewResetPending = true;
      if (this.isTiff) {
        this.applyPendingViewReset();
      }

      if (!this.isTiff) {
        await this.$nextTick();
        this.loadFullImage();
        // Set a timeout to handle cases where image never loads
        this.loadTimeout = setTimeout(() => {
          if (!this.fullImageLoaded && !this.imageLoaded && !this.deferredFull) {
            // Show the image even if load event didn't fire (might be partially loaded)
            this.fullImageLoaded = true;
            this.imageLoaded = true;
            mutations.setLoading("preview-img", false);
            resumeImagePreloads();
            this.scheduleSetCenter();
          }
        }, 30000); // 30 second timeout
      }
    },
  },
};
</script>

<style>
.image-ex-container {
  width: 100%;
  height: 100%;
  min-height: 0;
  max-width: 100%;
  max-height: 100%;
  overflow: hidden;
  position: relative;
  display: flex;
  justify-content: center;
  /* Block browser pull-to-refresh / overscroll so vertical dismiss & edge gestures win */
  overscroll-behavior: none;
  touch-action: none;
}

.image-ex-img {
  max-width: 100%;
  max-height: 100%;
  position: absolute;
  top: 50%;
  left: 50%;
  /* translate3d tends to composite more reliably than translate with decoded <img> bitmaps */
  transform: translate3d(-50%, -50%, 0);
  object-fit: contain;
}

.image-ex-placeholder {
  /* Same box as the full image will have (it shares .image-ex-img's centering). */
  width: 100%;
  height: 100%;
}

.image-loading-overlay {
  /* Appears only if loading takes a moment, so quick swaps don't flash a spinner. */
  opacity: 0;
  animation: image-loading-overlay-in 0.15s ease 0.3s forwards;
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate3d(-50%, -50%, 0);
  z-index: 10;
  display: flex;
  align-items: center;
  justify-content: center;
}

@keyframes image-loading-overlay-in {
  to {
    opacity: 1;
  }
}
</style>
