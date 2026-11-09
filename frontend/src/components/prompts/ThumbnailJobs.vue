<template>
  <!-- fufu: "Thumbnail jobs" panel (patch 0036), opened from Settings → System & Admin and from
       the progress box. Lists the server's running, queued and finished jobs. -->
  <div class="card-content thumbnail-jobs">
    <div class="thumbnail-jobs-toolbar">
      <span v-if="secondsPerFile > 0" class="thumbnail-jobs-rate" data-test="rate">
        {{ $t("thumbnailJobs.rate", { seconds: secondsPerFile.toFixed(2) }) }}
      </span>
      <button type="button" class="button button--flat button--grey" data-test="refresh" :disabled="loading" @click="load">
        {{ $t("thumbnailJobs.refresh") }}
      </button>
    </div>
    <p v-if="!jobs.length" class="thumbnail-jobs-empty" data-test="no-jobs">
      {{ loading ? $t("thumbnailJobs.loading") : $t("thumbnailJobs.noJobs") }}
    </p>
    <div v-for="job in jobs" :key="job.id" class="thumbnail-jobs-job" :data-job="job.id" :data-state="job.state">
      <div class="thumbnail-jobs-head">
        <span class="thumbnail-jobs-path" :title="`${job.source}: ${job.path}`">{{ job.path }}</span>
        <span class="thumbnail-jobs-state" :class="`state-${job.state}`">{{ stateLabel(job) }}</span>
        <button
          v-if="isActive(job) && !job.cancelRequested"
          type="button"
          class="button button--flat button--red thumbnail-jobs-cancel"
          data-test="cancel"
          @click="cancel(job)"
        >
          {{ $t("thumbnailJobs.cancel") }}
        </button>
      </div>
      <div v-if="job.state === 'running'" class="thumbnail-jobs-bar">
        <div :style="{ width: `${percent(job)}%` }"></div>
      </div>
      <div class="thumbnail-jobs-line">
        <span v-if="job.startedAt">{{ $t("thumbnailJobs.progressLine", { done: job.done, total: job.total, percent: percent(job) }) }}</span>
        <span v-if="job.state === 'running' && job.secondsLeft > 0">{{ $t("thumbnailJobs.timeLeft", { time: formatDuration(job.secondsLeft) }) }}</span>
        <span v-if="job.state === 'running' && job.cancelRequested">{{ $t("thumbnailJobs.cancelling") }}</span>
      </div>
      <div v-if="job.startedAt" class="thumbnail-jobs-line">
        <span>{{ $t("thumbnailJobs.summary", job) }}</span>
        <span v-if="job.secondsPerGenerated > 0">{{ $t("thumbnailJobs.rate", { seconds: job.secondsPerGenerated.toFixed(2) }) }}</span>
      </div>
      <div v-if="job.error" class="thumbnail-jobs-line thumbnail-jobs-error">{{ $t("thumbnailJobs.jobError", { error: job.error }) }}</div>
      <details v-if="job.failed > 0" class="thumbnail-jobs-failures">
        <summary>{{ $t("thumbnailJobs.failures", { count: job.failed }) }}</summary>
        <p v-if="!job.failures" class="thumbnail-jobs-note">{{ $t("thumbnailJobs.failuresAfterRefresh") }}</p>
        <template v-else>
          <p v-if="job.failures.length < job.failed" class="thumbnail-jobs-note">
            {{ $t("thumbnailJobs.failuresTruncated", { shown: job.failures.length, count: job.failed }) }}
          </p>
          <ul>
            <li v-for="(f, i) in job.failures" :key="i">
              <span class="thumbnail-jobs-fpath">{{ f.path }}</span>
              <span class="thumbnail-jobs-reason">{{ f.reason }}</span>
            </li>
          </ul>
        </template>
      </details>
    </div>
  </div>
</template>

<script>
import { thumbnailJobStateLabel } from "@/utils/thumbnailJobState.js";
import * as thumbnailJobsApi from "@/api/thumbnailJobs.js";
import { notify } from "@/notify";
import {
  allJobs, applyList, applyStatus, errorText, formatDuration, isActive, percent, thumbnailJobs,
} from "@/utils/thumbnailJobs.js";

export default {
  name: "ThumbnailJobs",
  props: {
    promptId: { type: [Number, String], default: null },
  },
  data() {
    return { loading: false };
  },
  computed: {
    jobs() {
      return allJobs();
    },
    secondsPerFile() {
      return thumbnailJobs.secondsPerFile;
    },
    /** A finished job with failures whose list hasn't been loaded (events don't carry it). */
    needsFailures() {
      return this.jobs.some((j) => !isActive(j) && j.failed > 0 && !j.failures);
    },
  },
  watch: {
    needsFailures(now) {
      if (now && !this.loading) void this.load();
    },
  },
  mounted() {
    void this.load();
  },
  methods: {
    isActive,
    percent,
    formatDuration(seconds) {
      return formatDuration(seconds, this.$t);
    },
    stateLabel(job) {
      return thumbnailJobStateLabel(this.$t, job.state);
    },
    async load() {
      this.loading = true;
      try {
        applyList(await thumbnailJobsApi.list());
      } catch (err) {
        notify.showError(errorText(err));
      } finally {
        this.loading = false;
      }
    },
    async cancel(job) {
      try {
        applyStatus(await thumbnailJobsApi.cancel(job.id));
      } catch (err) {
        notify.showError(errorText(err));
      }
    },
  },
};
</script>

<style scoped>
.thumbnail-jobs-toolbar {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 1em;
  margin-bottom: 0.5em;
}

.thumbnail-jobs-rate {
  margin-right: auto;
  color: var(--textSecondary);
}

.thumbnail-jobs-empty {
  color: var(--textSecondary);
}

.thumbnail-jobs-job {
  padding: 0.6em 0;
  border-top: 1px solid var(--divider);
}

.thumbnail-jobs-head {
  display: flex;
  align-items: center;
  gap: 0.75em;
}

.thumbnail-jobs-path {
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
  flex: 1;
}

.thumbnail-jobs-state {
  flex-shrink: 0;
  color: var(--textSecondary);
}

.thumbnail-jobs-state.state-running {
  color: var(--primaryColor);
}

.thumbnail-jobs-state.state-failed {
  color: var(--red);
}

.thumbnail-jobs-cancel {
  flex-shrink: 0;
  padding: 0.2em 0.6em;
}

.thumbnail-jobs-bar {
  height: 0.35em;
  margin: 0.4em 0;
  border-radius: 1em;
  background: var(--surfaceSecondary);
  overflow: hidden;
}

.thumbnail-jobs-bar > div {
  height: 100%;
  background: var(--primaryColor);
  transition: width 0.4s ease;
}

.thumbnail-jobs-line {
  display: flex;
  flex-wrap: wrap;
  gap: 0 1em;
  color: var(--textSecondary);
  font-variant-numeric: tabular-nums;
}

.thumbnail-jobs-error {
  color: var(--red);
}

.thumbnail-jobs-failures {
  margin-top: 0.4em;
}

.thumbnail-jobs-failures summary {
  cursor: pointer;
  color: var(--red);
}

.thumbnail-jobs-failures ul {
  list-style: none;
  margin: 0.4em 0 0;
  padding: 0;
  max-height: 16em;
  overflow-y: auto;
}

.thumbnail-jobs-failures li {
  display: flex;
  flex-direction: column;
  padding: 0.2em 0;
}

.thumbnail-jobs-fpath {
  word-break: break-all;
}

.thumbnail-jobs-reason,
.thumbnail-jobs-note {
  color: var(--textSecondary);
  font-size: 0.9em;
}
</style>
