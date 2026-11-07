<template>
  <!-- fufu: start dialog for thumbnail jobs (patch 0036), opened from the folder menu.
       One job is queued per folder; the server skips files already in the cache. -->
  <div class="card-content thumbnail-job">
    <p v-if="loading" class="thumbnail-job-loading" data-test="counting">{{ $t("thumbnailJobs.counting") }}</p>
    <p v-else-if="error" class="thumbnail-job-error" data-test="count-error">{{ $t("thumbnailJobs.countFailed", { error }) }}</p>
    <template v-else>
      <ul class="thumbnail-job-folders">
        <li v-for="row in rows" :key="row.path" data-test="folder-row">
          <span class="thumbnail-job-path" :title="row.path">{{ row.path }}</span>
          <span class="thumbnail-job-count">{{ $t("thumbnailJobs.folderCount", { count: row.count }, row.count) }}</span>
        </li>
      </ul>
      <p v-if="rows.length > 1" class="thumbnail-job-total" data-test="total">
        {{ $t("thumbnailJobs.totalCount", { count: total }, total) }}
      </p>
      <p class="thumbnail-job-explain">{{ $t("thumbnailJobs.explain") }}</p>
      <p class="thumbnail-job-estimate" data-test="estimate">
        {{ estimateText }}
      </p>
    </template>
  </div>
  <div class="card-actions">
    <button
      type="button"
      class="button button--flat button--grey"
      :aria-label="$t('general.cancel')"
      :title="$t('general.cancel')"
      @click="close"
    >
      {{ $t("general.cancel") }}
    </button>
    <button
      type="button"
      class="button button--flat"
      data-test="start"
      :disabled="loading || starting || !!error || total === 0"
      :aria-label="$t('thumbnailJobs.start')"
      :title="$t('thumbnailJobs.start')"
      @click="start"
    >
      {{ $t("thumbnailJobs.start") }}
    </button>
  </div>
</template>

<script>
import * as thumbnailJobsApi from "@/api/thumbnailJobs.js";
import { mutations } from "@/store";
import { notify } from "@/notify";
import { applyStatus, errorText, formatDuration, thumbnailJobs } from "@/utils/thumbnailJobs.js";

export default {
  name: "ThumbnailJob",
  props: {
    source: { type: String, required: true },
    paths: { type: Array, required: true },
    promptId: { type: [Number, String], default: null },
  },
  data() {
    return {
      loading: true,
      starting: false,
      error: "",
      /** @type {{path: string, count: number}[]} */
      rows: [],
      secondsPerFile: 0,
    };
  },
  computed: {
    total() {
      return this.rows.reduce((n, r) => n + r.count, 0);
    },
    estimateText() {
      if (!(this.secondsPerFile > 0)) return this.$t("thumbnailJobs.estimateUnknown");
      const seconds = this.total * this.secondsPerFile;
      if (seconds < 60) return this.$t("thumbnailJobs.estimateUnderAMinute");
      return this.$t("thumbnailJobs.estimate", { time: formatDuration(seconds, this.$t) });
    },
  },
  async mounted() {
    try {
      const results = await Promise.all(this.paths.map((p) => thumbnailJobsApi.count(this.source, p)));
      this.rows = this.paths.map((p, i) => ({ path: p, count: results[i].count }));
      this.secondsPerFile = results[0]?.secondsPerFile || thumbnailJobs.secondsPerFile || 0;
    } catch (err) {
      this.error = errorText(err);
    } finally {
      this.loading = false;
    }
  },
  methods: {
    close() {
      mutations.closeTopPrompt();
    },
    async start() {
      this.starting = true;
      try {
        const resp = await thumbnailJobsApi.start(this.source, this.paths);
        for (const job of resp.jobs || []) applyStatus(job);
        notify.showSuccessToast(this.$t("thumbnailJobs.queued", { count: this.paths.length }, this.paths.length));
        this.close();
      } catch (err) {
        notify.showError(errorText(err));
        this.starting = false;
      }
    },
  },
};
</script>

<style scoped>
.thumbnail-job-folders {
  list-style: none;
  margin: 0 0 0.75em;
  padding: 0;
  max-height: 14em;
  overflow-y: auto;
}

.thumbnail-job-folders li {
  display: flex;
  gap: 1em;
  justify-content: space-between;
  padding: 0.2em 0;
}

.thumbnail-job-path {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.thumbnail-job-count {
  flex-shrink: 0;
  font-variant-numeric: tabular-nums;
}

.thumbnail-job-total {
  font-weight: 500;
}

.thumbnail-job-explain,
.thumbnail-job-estimate {
  color: var(--textSecondary);
  margin: 0.4em 0;
}

.thumbnail-job-error {
  color: var(--red);
}
</style>
