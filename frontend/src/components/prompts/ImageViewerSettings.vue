<template>
  <div class="card-content image-viewer-settings-content settings-items">
    <p class="device-note item">{{ $t("imageViewer.deviceOnly") }}</p>
    <div class="setting-row item">
      <div class="setting-label">
        <label for="iv-preload-ahead">{{ $t("imageViewer.preloadAhead") }}</label>
        <HelpTooltipIcon :text="$t('imageViewer.preloadAheadDescription')" />
      </div>
      <div class="setting-control">
        <input
          id="iv-preload-ahead"
          type="number"
          class="input"
          v-model.number="preloadAhead"
          :min="limits.preloadAhead.min"
          :max="limits.preloadAhead.max"
          :aria-label="$t('imageViewer.preloadAhead')"
        />
      </div>
    </div>
    <div class="setting-row item">
      <div class="setting-label">
        <label for="iv-preload-behind">{{ $t("imageViewer.preloadBehind") }}</label>
        <HelpTooltipIcon :text="$t('imageViewer.preloadBehindDescription')" />
      </div>
      <div class="setting-control">
        <input
          id="iv-preload-behind"
          type="number"
          class="input"
          v-model.number="preloadBehind"
          :min="limits.preloadBehind.min"
          :max="limits.preloadBehind.max"
          :aria-label="$t('imageViewer.preloadBehind')"
        />
      </div>
    </div>
    <div class="setting-row item">
      <div class="setting-label">
        <label for="iv-preload-max-mb">{{ $t("imageViewer.preloadMaxMB") }}</label>
        <HelpTooltipIcon :text="$t('imageViewer.preloadMaxMBDescription')" />
      </div>
      <div class="setting-control">
        <input
          id="iv-preload-max-mb"
          type="number"
          class="input"
          v-model.number="preloadMaxMB"
          :min="limits.preloadMaxMB.min"
          :max="limits.preloadMaxMB.max"
          :aria-label="$t('imageViewer.preloadMaxMB')"
        />
      </div>
    </div>
  </div>
  <div class="card-actions">
    <button
      type="button"
      class="button button--flat button--grey"
      @click="resetDefaults"
      :aria-label="$t('imageViewer.resetDefaults')"
      :title="$t('imageViewer.resetDefaults')"
    > {{ $t("imageViewer.resetDefaults") }}
    </button>
    <button
      type="button"
      class="button button--flat"
      @click="closeTopPrompt"
      :aria-label="$t('general.ok')"
      :title="$t('general.ok')"
    > {{ $t("general.ok") }}
    </button>
  </div>
</template>

<script>
import HelpTooltipIcon from "@/components/HelpTooltipIcon.vue";
import { mutations } from "@/store";
import {
  IMAGE_VIEWER_LIMITS,
  imageViewerConfig,
  resetImageViewerConfig,
  saveImageViewerConfig,
} from "@/utils/imageViewerConfig.js";

export default {
  name: "ImageViewerSettings",
  components: { HelpTooltipIcon },
  props: {
    promptId: { type: [String, Number], default: null },
  },
  data() {
    return {
      limits: IMAGE_VIEWER_LIMITS,
    };
  },
  computed: {
    // Invalid input (e.g. an emptied field) is ignored by saveImageViewerConfig, which keeps
    // the last valid value; out-of-range numbers are clamped.
    preloadAhead: {
      get() { return imageViewerConfig.preloadAhead; },
      set(value) { saveImageViewerConfig({ preloadAhead: value }); },
    },
    preloadBehind: {
      get() { return imageViewerConfig.preloadBehind; },
      set(value) { saveImageViewerConfig({ preloadBehind: value }); },
    },
    preloadMaxMB: {
      get() { return imageViewerConfig.preloadMaxMB; },
      set(value) { saveImageViewerConfig({ preloadMaxMB: value }); },
    },
  },
  methods: {
    closeTopPrompt() {
      mutations.closeTopPrompt();
    },
    resetDefaults() {
      resetImageViewerConfig();
    },
  },
};
</script>

<style scoped>
.settings-items .item {
  padding-top: 0.75em;
  padding-bottom: 0.75em;
}

.image-viewer-settings-content {
  display: flex;
  flex-direction: column;
  min-width: 0;
  padding: 0 0.75em;
}

.device-note {
  margin: 0;
  color: var(--textSecondary);
  font-size: 0.9em;
}

.setting-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1em;
  min-width: 0;
}

.setting-row label {
  color: var(--textPrimary);
  font-size: 0.95em;
  flex-shrink: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.setting-label {
  display: flex;
  align-items: center;
  gap: 0.3em;
  min-width: 0;
  flex-shrink: 1;
}

.setting-control {
  flex: 0 0 auto;
  display: flex;
  justify-content: flex-end;
}

.setting-control input[type="number"] {
  width: 6em;
  text-align: right;
}
</style>
