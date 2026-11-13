<template>
  <!-- fufu: the thumbnail job box (ThumbnailJobProgress.vue) minimized into the status bar:
       an icon, the percentage across the active jobs and a short bar, or a check mark and the
       number of finished jobs. A pause icon while the running job waits for browsing.
       Clicking it expands the box again. Admins only; the status bar itself shows only in
       listings, search and the editor, so there's nothing elsewhere while minimized. -->
  <button
    v-if="show"
    type="button"
    class="tjs"
    data-test="thumbnail-status"
    :title="title"
    :aria-label="title"
    @click="expand"
  >
    <i v-if="overall.active && overall.paused" class="material-symbols tjs-icon" data-test="status-paused">pause_circle</i>
    <i v-else class="material-symbols tjs-icon">photo_library</i>
    <template v-if="overall.active">
      <span class="tjs-text" data-test="status-text">{{ $t("thumbnailJobs.statusActive", { percent: overall.percent }) }}</span>
      <span class="tjs-bar"><span :style="{ width: `${overall.percent}%` }"></span></span>
    </template>
    <template v-else>
      <i class="material-symbols tjs-check">check</i>
      <span class="tjs-text" data-test="status-text">{{ $t("thumbnailJobs.statusFinished", { count: overall.finished }, overall.finished) }}</span>
    </template>
  </button>
</template>

<script>
import { canUseThumbnailJobs, overallProgress, progressJobs, setMinimized, thumbnailJobs } from "@/utils/thumbnailJobs.js";

export default {
  name: "ThumbnailJobStatus",
  computed: {
    jobs() {
      return progressJobs();
    },
    overall() {
      return overallProgress(this.jobs);
    },
    show() {
      return canUseThumbnailJobs() && thumbnailJobs.minimized && this.jobs.length > 0;
    },
    title() {
      const { active, finished, percent, paused } = this.overall;
      let text = active
        ? this.$t("thumbnailJobs.statusActiveTitle", { count: active, percent }, active)
        : this.$t("thumbnailJobs.statusFinishedTitle", { count: finished }, finished);
      if (active && paused) text += ` · ${this.$t("thumbnailJobs.pausedForBrowsing")}`;
      return `${text} · ${this.$t("thumbnailJobs.expand")}`;
    },
  },
  methods: {
    expand() {
      setMinimized(false);
    },
  },
};
</script>

<style scoped>
.tjs {
  display: flex;
  align-items: center;
  gap: 0.35em;
  background: none;
  border: none;
  padding: 0.2em 0.3em;
  border-radius: var(--borderRadius);
  color: var(--textSecondary);
  font: inherit;
  font-weight: 500;
  cursor: pointer;
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}

.tjs:hover {
  background: var(--surfaceSecondary);
}

.tjs .material-symbols {
  font-size: 1.25em;
}

.tjs-icon {
  color: var(--primaryColor);
}

.tjs-bar {
  display: block;
  width: 3em;
  height: 0.3em;
  border-radius: 1em;
  background: var(--surfaceSecondary);
  overflow: hidden;
}

.tjs-bar > span {
  display: block;
  height: 100%;
  background: var(--primaryColor);
  transition: width 0.4s ease;
}
</style>
