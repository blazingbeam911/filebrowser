<template>
  <!-- fufu: "Saved sorting options" manager, opened from the sort header's menu ("Manage…").
       Lists this browser's saved sorting options (utils/sortSource.js listSavedSortings). -->
  <div class="card-content sorting-manager">
    <div class="sorting-manager-search">
      <input
        v-model="query"
        type="search"
        class="input input--block"
        :class="{ invalid: regexInvalid }"
        :placeholder="$t('files.filterByFolder')"
        :aria-label="$t('files.filterByFolder')"
        :title="regexInvalid ? $t('files.invalidRegex') : ''"
      />
      <button
        type="button"
        class="sorting-manager-regex"
        :class="{ on: regex }"
        data-test="regex"
        :aria-pressed="regex"
        :title="$t('files.useRegex')"
        :aria-label="$t('files.useRegex')"
        @click="regex = !regex"
      >{{ regexToggleText }}</button>
    </div>
    <div v-if="rows.length" class="sorting-manager-toolbar">
      <label class="sorting-manager-check">
        <input type="checkbox" data-test="select-all" :checked="allShownSelected" @change="toggleAllShown($event.target.checked)" />
        <span>{{ $t("files.selectAllShown") }}</span>
      </label>
      <span class="sorting-manager-bulk">
        <button
          v-if="missingRows.length"
          type="button"
          class="button button--flat button--grey"
          data-test="reset-missing"
          @click="resetMissing"
        >
          {{ $t("files.resetMissingCount", { count: missingRows.length }) }}
        </button>
        <button
          type="button"
          class="button button--flat button--grey"
          data-test="reset-selected"
          :disabled="selectedRows.length === 0"
          @click="resetSelected"
        >
          {{ $t("files.resetSelectedCount", { count: selectedRows.length }) }}
        </button>
      </span>
    </div>

    <p v-if="!rows.length && !global" class="sorting-manager-empty">{{ $t("files.noSavedSortingOptions") }}</p>

    <div class="sorting-manager-list">
      <div v-if="global" class="sorting-manager-row sorting-manager-global" data-row="global">
        <span></span>
        <span class="sorting-manager-path">{{ $t("files.sortGlobalRow") }}</span>
        <span class="sorting-manager-controls">
          <select class="input sorting-manager-column" :value="global.by" :aria-label="$t('files.sortGlobalRow')" @change="setGlobal({ by: $event.target.value, asc: global.asc })">
            <option v-for="col in columns" :key="col.by" :value="col.by">{{ col.label }}</option>
          </select>
          <button type="button" class="sorting-manager-icon" data-test="arrow" :title="directionLabel(global)" @click="setGlobal({ by: global.by, asc: !global.asc })">
            <i class="material-symbols">{{ arrowIcon(global) }}</i>
          </button>
          <span class="sorting-manager-flag"></span>
          <button type="button" class="sorting-manager-icon" data-test="reset" :title="$t('files.sortReset')" @click="setGlobal(null)">
            <i class="material-symbols">restart_alt</i>
          </button>
        </span>
      </div>

      <template v-for="group in groups" :key="group.source">
        <h3 class="sorting-manager-group">{{ group.label }}</h3>
        <div
          v-for="row in group.rows"
          :key="row.id"
          class="sorting-manager-row"
          :class="{ missing: existence[row.id] === false }"
          :data-row="row.path"
        >
          <input type="checkbox" data-test="select" :checked="Boolean(selected[row.id])" :aria-label="row.path" @change="select(row, $event.target.checked)" />
          <!-- The whole path cell is the link, so short paths like "/" are easy to hit. -->
          <a class="sorting-manager-path sorting-manager-link" :href="row.key" :title="row.path" @click.prevent="open(row)">
            <span class="sorting-manager-path-text">{{ row.path }}</span>
            <span v-if="existence[row.id] === false" class="sorting-manager-missing"><i class="material-symbols">warning</i> {{ $t("files.folderNotFound") }}</span>
          </a>
          <span class="sorting-manager-controls">
            <select class="input sorting-manager-column" :value="row.sorting.by" :aria-label="row.path" @change="setRow(row, { by: $event.target.value, asc: row.sorting.asc }, row.recursive)">
              <option v-for="col in columns" :key="col.by" :value="col.by">{{ col.label }}</option>
            </select>
            <button type="button" class="sorting-manager-icon" data-test="arrow" :title="directionLabel(row.sorting)" @click="setRow(row, { by: row.sorting.by, asc: !row.sorting.asc }, row.recursive)">
              <i class="material-symbols">{{ arrowIcon(row.sorting) }}</i>
            </button>
            <label class="sorting-manager-check sorting-manager-flag">
              <input type="checkbox" data-test="subfolders" :checked="row.recursive" @change="setRow(row, row.sorting, $event.target.checked)" />
              <span>{{ $t("files.sortSubfolders") }}</span>
            </label>
            <button type="button" class="sorting-manager-icon" data-test="reset" :title="$t('files.sortReset')" @click="resetRow(row)">
              <i class="material-symbols">restart_alt</i>
            </button>
          </span>
        </div>
      </template>
    </div>
  </div>
  <div class="card-actions">
    <button type="button" class="button button--flat" data-test="close" @click="close">
      {{ $t("general.close") }}
    </button>
  </div>
</template>

<script>
import { state, mutations } from "@/store";
import { folderExists } from "@/api/resources.js";
import {
  COLUMN_LABEL_KEYS,
  filterSortingRows,
  listSavedSortings,
  relativeFolderPath,
  sortDirectionLabel,
  sortingFilterRegex,
} from "@/utils/sortSource.js";

const CHECK_CONCURRENCY = 4;
const SHARE_KEY_PREFIX = "/public/share/";

export default {
  name: "SortingManager",
  props: {
    promptId: {
      type: [String, Number],
      default: null,
    },
  },
  data() {
    return {
      query: "",
      regex: false,
      regexToggleText: ".*",
      /** id → true when selected */
      selected: {},
      /** id → true (exists), false (missing), null (unknown) */
      existence: {},
      abort: null,
    };
  },
  computed: {
    rows() {
      return listSavedSortings(state.displayPreferences);
    },
    shownRows() {
      return filterSortingRows(this.rows, this.query, { regex: this.regex });
    },
    regexInvalid() {
      return this.regex && this.query.trim() !== "" && sortingFilterRegex(this.query) === null;
    },
    groups() {
      const groups = [];
      for (const row of this.shownRows) {
        let group = groups[groups.length - 1];
        if (!group || group.source !== row.source) {
          const label = row.key.startsWith(SHARE_KEY_PREFIX)
            ? this.$t("files.sortShareGroup", { hash: row.source.slice(0, 8) })
            : row.source;
          group = { source: row.source, label, rows: [] };
          groups.push(group);
        }
        group.rows.push(row);
      }
      return groups;
    },
    global() {
      return state.globalSorting;
    },
    columns() {
      return Object.entries(COLUMN_LABEL_KEYS).map(([by, key]) => ({ by, label: this.$t(key) }));
    },
    allShownSelected() {
      return this.shownRows.length > 0 && this.shownRows.every((row) => this.selected[row.id]);
    },
    selectedRows() {
      return this.rows.filter((row) => this.selected[row.id]);
    },
    missingRows() {
      return this.rows.filter((row) => this.existence[row.id] === false);
    },
  },
  mounted() {
    this.checkFolders();
  },
  beforeUnmount() {
    this.abort?.abort();
  },
  methods: {
    arrowIcon(sorting) {
      return sorting.asc ? "arrow_upward" : "arrow_downward";
    },
    directionLabel(sorting) {
      return sortDirectionLabel(this.$t.bind(this), sorting.by, sorting.asc);
    },
    /** Checks in the background which folders still exist, a few at a time. */
    async checkFolders() {
      this.abort = new AbortController();
      const { signal } = this.abort;
      const queue = [...this.rows];
      const worker = async () => {
        while (queue.length && !signal.aborted) {
          const row = queue.shift();
          const isShare = row.key.startsWith(SHARE_KEY_PREFIX);
          const exists = await folderExists(row.source, relativeFolderPath(row.key), {
            hash: isShare ? row.source : "",
            signal,
          });
          if (!signal.aborted) this.existence = { ...this.existence, [row.id]: exists };
        }
      };
      await Promise.all(Array.from({ length: CHECK_CONCURRENCY }, worker));
    },
    select(row, on) {
      this.selected = { ...this.selected, [row.id]: on };
    },
    toggleAllShown(on) {
      const next = { ...this.selected };
      for (const row of this.shownRows) next[row.id] = on;
      this.selected = next;
    },
    setRow(row, sorting, recursive) {
      mutations.setFolderSorting({ source: row.source, key: row.key, rawKeys: row.rawKeys, sorting, recursive });
    },
    setGlobal(sorting) {
      mutations.setGlobalSorting(sorting);
    },
    open(row) {
      this.close();
      this.$router.push(row.key);
    },
    forget(rows) {
      const next = { ...this.selected };
      for (const row of rows) delete next[row.id];
      this.selected = next;
    },
    /** Asks first (generic prompt on top of this one); `run` on the confirm button. */
    confirm(title, body, run) {
      mutations.showPrompt({
        name: "generic",
        props: {
          title,
          body,
          buttons: [
            { label: this.$t("general.cancel"), className: "button--grey", action: () => mutations.closeTopPrompt() },
            {
              label: this.$t("files.sortReset"),
              action: () => {
                mutations.closeTopPrompt();
                run();
              },
            },
          ],
        },
      });
    },
    resetRow(row) {
      const run = () => {
        mutations.resetFolderSortings([row]);
        this.forget([row]);
      };
      if (row.recursive) {
        // A rule: its subfolders stop inheriting it (as the menu's "Reset this folder").
        this.confirm(this.$t("files.resetFolderSortTitle"), this.$t("files.resetFolderRuleBody"), run);
      } else {
        run();
      }
    },
    resetSelected() {
      const rows = this.selectedRows;
      if (!rows.length) return;
      this.confirm(this.$t("files.resetSelectedTitle"), this.$t("files.resetSelectedBody", rows.length), () => {
        mutations.resetFolderSortings(rows);
        this.forget(rows);
      });
    },
    resetMissing() {
      const rows = this.missingRows;
      if (!rows.length) return;
      this.confirm(this.$t("files.resetMissingTitle"), this.$t("files.resetMissingBody", rows.length), () => {
        mutations.resetFolderSortings(rows);
        this.forget(rows);
      });
    },
    close() {
      mutations.closeTopPrompt(this.promptId ?? undefined);
    },
  },
};
</script>

<style scoped>
.sorting-manager {
  display: flex;
  flex-direction: column;
  gap: 0.5em;
  min-height: 0;
}

/* The list takes the window's remaining height and scrolls by itself, so a bigger (or
   maximized) window shows more rows while the search and toolbar stay put. */
.sorting-manager > .sorting-manager-search,
.sorting-manager > .sorting-manager-toolbar,
.sorting-manager > .sorting-manager-empty {
  flex-shrink: 0;
}

.sorting-manager-search {
  position: relative;
  display: flex;
  align-items: center;
}

.sorting-manager-search input {
  margin: 0;
  padding-right: 3em;
}

.sorting-manager-search input.invalid {
  outline: 2px solid var(--red, #e74c3c);
  outline-offset: -2px;
}

/* ".*": switches the search to a regular expression. */
.sorting-manager-regex {
  position: absolute;
  right: 0.4em;
  min-width: 0;
  min-height: 0;
  padding: 0.15em 0.45em;
  font-family: monospace;
  font-size: 0.95em;
  background: transparent;
  color: inherit;
  opacity: 0.6;
  border-radius: var(--borderRadius);
}

.sorting-manager-regex:hover {
  transform: none;
}

.sorting-manager-regex.on {
  opacity: 1;
  background: var(--primaryColor);
  color: var(--background);
}

.sorting-manager-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.5em;
}

.sorting-manager-bulk {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5em;
}

.sorting-manager-bulk .button {
  margin: 0;
}

.sorting-manager-empty {
  opacity: 0.8;
}

.sorting-manager-list {
  flex: 1 1 auto;
  min-height: 3em;
  overflow-y: auto;
  /* Rows stack by the list's own width (the window's), not the page's. */
  container-type: inline-size;
  /* The list reaches into the window's side padding, so its scrollbar sits at the window's
     edge, clear of the reset buttons; the rows stay within a few pixels of the search box's
     edge whether a (thin) scrollbar shows or not. */
  margin-right: -1em;
  padding-right: 0.75em;
}

/* The header text, its divider and the first row's controls sit equally far apart
   (0.45em: padding below the text; margin + the row's own 0.15em padding below the line). */
.sorting-manager-group {
  margin: 0.75em 0 0.3em;
  padding-bottom: 0.45em;
  line-height: 1.2;
  font-size: 1em;
  font-weight: bold;
  text-align: left;
  border-bottom: 1px solid var(--divider);
}

/* Checkbox | path | controls; on narrow screens the controls go under the path. */
.sorting-manager-row {
  display: grid;
  grid-template-columns: 1.1em minmax(0, 1fr) auto;
  align-items: center;
  column-gap: 0.5em;
  row-gap: 0.15em;
  padding: 0.15em 0;
}

.sorting-manager-row > input[type="checkbox"] {
  margin: 0;
}

.sorting-manager-path {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 0.5em;
  overflow: hidden;
}

.sorting-manager-link {
  align-self: stretch;
  min-height: 2em;
  color: inherit;
  cursor: pointer;
}

.sorting-manager-link:hover .sorting-manager-path-text {
  text-decoration: underline;
}

.sorting-manager-path-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sorting-manager-global .sorting-manager-path {
  font-weight: bold;
}

.sorting-manager-controls {
  display: flex;
  align-items: center;
  gap: 0.4em;
}

@container (max-width: 500px) {
  .sorting-manager-controls {
    grid-column: 2 / 4;
  }
}

.sorting-manager-column {
  width: 9.5em;
  margin: 0;
  padding: 0.25em 0.4em;
}

.sorting-manager-icon {
  min-width: 0;
  min-height: 0;
  padding: 0.25em;
  background: transparent;
  color: inherit;
  border-radius: var(--borderRadius);
}

.sorting-manager-icon:hover {
  transform: none;
}

.sorting-manager-icon i {
  font-size: 1.25em;
}

.sorting-manager-check {
  display: flex;
  align-items: center;
  gap: 0.35em;
  cursor: pointer;
}

.sorting-manager-flag {
  width: 6.5em;
  flex-shrink: 0;
}

/* A folder that no longer exists: greyed, with a mark. */
.sorting-manager-row.missing .sorting-manager-path,
.sorting-manager-row.missing .sorting-manager-column,
.sorting-manager-row.missing .sorting-manager-flag {
  opacity: 0.5;
}

.sorting-manager-missing {
  display: inline-flex;
  align-items: center;
  gap: 0.2em;
  white-space: nowrap;
  font-size: 0.9em;
}

.sorting-manager-missing i {
  font-size: 1.1em;
}
</style>
