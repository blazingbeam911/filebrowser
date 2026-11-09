<template>
  <!-- fufu: live progress of thumbnail jobs (patch 0036), bottom left (the bottom right is the
       notifications'), past the sidebar when it's pinned open; admins only. Shows
       running and queued jobs, and the summary of each one that finished while this page was
       open, until dismissed. State: utils/thumbnailJobs.js. Kept current by "thumbnailJob"
       events when this user has the realtime event stream, otherwise by polling every 2 s
       while a job is queued or running. -->
  <div v-if="jobs.length" id="thumbnail-job-progress" class="floating-window" data-test="thumbnail-progress" :style="placement">
    <div class="tjp-header">
      <i class="material-symbols">photo_library</i>
      <span class="tjp-title">{{ $t("thumbnailJobs.panelTitle") }}</span>
      <button type="button" class="tjp-link" data-test="details" @click="openPanel">{{ $t("thumbnailJobs.details") }}</button>
    </div>
    <div v-for="job in jobs" :key="job.id" class="tjp-job" :data-job="job.id" :data-state="job.state">
      <div class="tjp-row">
        <span class="tjp-path" :title="`${job.source}: ${job.path}`">{{ job.path }}</span>
        <button
          v-if="isActive(job) && !job.cancelRequested"
          type="button"
          class="tjp-icon"
          data-test="cancel"
          :aria-label="$t('thumbnailJobs.cancel')"
          :title="$t('thumbnailJobs.cancel')"
          @click="cancel(job)"
        ><i class="material-symbols">cancel</i></button>
        <button
          v-if="!isActive(job)"
          type="button"
          class="tjp-icon"
          data-test="dismiss"
          :aria-label="$t('thumbnailJobs.dismiss')"
          :title="$t('thumbnailJobs.dismiss')"
          @click="dismiss(job.id)"
        ><i class="material-symbols">close</i></button>
      </div>
      <template v-if="job.state === 'queued'">
        <div class="tjp-line">{{ $t("thumbnailJobs.state.queued") }}</div>
      </template>
      <template v-else-if="job.state === 'running'">
        <div class="tjp-bar"><div :style="{ width: `${percent(job)}%` }"></div></div>
        <div class="tjp-line" data-test="progress-line">
          <span>{{ $t("thumbnailJobs.progressLine", { done: job.done, total: job.total, percent: percent(job) }) }}</span>
          <span v-if="job.cancelRequested">{{ $t("thumbnailJobs.cancelling") }}</span>
          <span v-else-if="job.secondsLeft > 0">{{ $t("thumbnailJobs.timeLeft", { time: formatDuration(job.secondsLeft) }) }}</span>
          <span v-if="job.failed > 0" class="tjp-failed">{{ $t("thumbnailJobs.failedCount", { count: job.failed }) }}</span>
        </div>
      </template>
      <template v-else>
        <div class="tjp-line" data-test="summary">
          <span class="tjp-state" :class="{ 'tjp-failed': job.state === 'failed' }">{{ stateLabel(job) }}</span>
          <span v-if="job.startedAt">{{ $t("thumbnailJobs.summary", job) }}</span>
        </div>
        <div v-if="job.error" class="tjp-line tjp-failed">{{ $t("thumbnailJobs.jobError", { error: job.error }) }}</div>
      </template>
    </div>
  </div>
</template>

<script>
import { thumbnailJobStateLabel } from "@/utils/thumbnailJobState.js";
import * as thumbnailJobsApi from "@/api/thumbnailJobs.js";
import { getters, mutations, state } from "@/store";
import { notify } from "@/notify";
import {
  applyStatus, dismiss, errorText, formatDuration, isActive, percent, progressJobs, refreshThumbnailJobs,
} from "@/utils/thumbnailJobs.js";

const POLL_MS = 2000;

export default {
  name: "ThumbnailJobProgress",
  data() {
    return { pollTimer: null };
  },
  computed: {
    jobs() {
      return progressJobs();
    },
    placement() {
      const pinned = getters.isSidebarVisible() && getters.isStickySidebar();
      return { left: pinned ? `${state.sidebar.width + 1}em` : "1em" };
    },
    /** Poll while a job is active and no event stream brings its updates. */
    needsPolling() {
      return getters.isAdmin() && !state.realtimeActive && this.jobs.some((j) => isActive(j));
    },
  },
  watch: {
    needsPolling: {
      handler(on) {
        if (on && !this.pollTimer) {
          this.pollTimer = setInterval(() => void refreshThumbnailJobs(), POLL_MS);
        } else if (!on && this.pollTimer) {
          clearInterval(this.pollTimer);
          this.pollTimer = null;
        }
      },
      immediate: true,
    },
  },
  mounted() {
    // Page load: jobs still running come back (the event stream, when there is one, also
    // reloads them when it connects).
    void refreshThumbnailJobs();
  },
  beforeUnmount() {
    if (this.pollTimer) clearInterval(this.pollTimer);
  },
  methods: {
    stateLabel(job) {
      return thumbnailJobStateLabel(this.$t, job.state);
    },
    isActive,
    percent,
    dismiss,
    formatDuration(seconds) {
      return formatDuration(seconds, this.$t);
    },
    async cancel(job) {
      try {
        applyStatus(await thumbnailJobsApi.cancel(job.id));
      } catch (err) {
        notify.showError(errorText(err));
      }
    },
    openPanel() {
      mutations.showPrompt({ name: "ThumbnailJobs", props: { title: this.$t("thumbnailJobs.panelTitle") } });
    },
  },
};
</script>

<style scoped>
#thumbnail-job-progress {
  position: fixed;
  bottom: 3.5em; /* clear of the status bar (2.5em, 3em on phones) */
  z-index: 19; /* under the notifications (20) */
  color: var(--textPrimary);
  width: 22em;
  max-width: calc(100vw - 2em);
  max-height: 40vh;
  overflow-y: auto;
  padding: 0.6em 0.8em;
  border-radius: var(--borderRadius);
}

.tjp-header {
  display: flex;
  align-items: center;
  gap: 0.4em;
  margin-bottom: 0.3em;
}

.tjp-header .material-symbols {
  color: var(--primaryColor);
  font-size: 1.2em;
}

.tjp-title {
  font-weight: 500;
  flex: 1;
}

.tjp-link {
  background: none;
  border: none;
  padding: 0;
  color: var(--primaryColor);
  cursor: pointer;
  font: inherit;
}

.tjp-job {
  padding: 0.35em 0;
  border-top: 1px solid var(--divider);
}

.tjp-row {
  display: flex;
  align-items: center;
  gap: 0.4em;
}

.tjp-path {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tjp-icon {
  background: none;
  border: none;
  padding: 0;
  cursor: pointer;
  color: var(--textSecondary);
  display: flex;
}

.tjp-icon .material-symbols {
  font-size: 1.15em;
}

.tjp-bar {
  height: 0.3em;
  margin: 0.3em 0;
  border-radius: 1em;
  background: var(--surfaceSecondary);
  overflow: hidden;
}

.tjp-bar > div {
  height: 100%;
  background: var(--primaryColor);
  transition: width 0.4s ease;
}

.tjp-line {
  display: flex;
  flex-wrap: wrap;
  gap: 0 0.8em;
  font-size: 0.9em;
  color: var(--textSecondary);
  font-variant-numeric: tabular-nums;
}

.tjp-failed {
  color: var(--red);
}

.tjp-state {
  font-weight: 500;
}
</style>
