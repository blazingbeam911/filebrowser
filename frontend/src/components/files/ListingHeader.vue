<template>
  <div class="listing-item-header card" :class="{ 'desktop-view': !isMobile }" @contextmenu="openSortMenu">
    <p
      :class="columnClass('name')"
      class="name"
      role="button"
      tabindex="0"
      @click="sort('name')"
      @keydown.enter.prevent="sort('name')"
      @keydown.space.prevent="sort('name')"
      :title="columnTitle('name', 'files.sortByName')"
      :aria-label="$t('files.sortByName')"
    >
      <span>{{ $t("general.name") }}</span>
      <i class="material-symbols">{{ nameIcon }}</i>
    </p>

    <p
      :class="columnClass('size')"
      class="size"
      role="button"
      tabindex="0"
      @click="sort('size')"
      @keydown.enter.prevent="sort('size')"
      @keydown.space.prevent="sort('size')"
      :title="columnTitle('size', 'files.sortBySize')"
      :aria-label="$t('files.sortBySize')"
    >
      <i class="material-symbols">{{ sizeIcon }}</i>
      <span>{{ $t("general.size") }}</span>
    </p>

    <p
      :class="columnClass('modified')"
      class="modified"
      role="button"
      tabindex="0"
      @click="sort('modified')"
      @keydown.enter.prevent="sort('modified')"
      @keydown.space.prevent="sort('modified')"
      :title="columnTitle('modified', 'files.sortByLastModified')"
      :aria-label="$t('files.sortByLastModified')"
    >
      <i class="material-symbols">{{ modifiedIcon }}</i>
      <span>{{ $t("files.lastModified") }}</span>
    </p>

    <p
      v-if="showKindColumn"
      :class="columnClass('kind')"
      class="kind"
      role="button"
      tabindex="0"
      @click="sort('kind')"
      @keydown.enter.prevent.stop="sort('kind')"
      @keydown.space.prevent.stop="sort('kind')"
      :title="columnTitle('kind', 'files.sortByType')"
      :aria-label="$t('files.sortByType')"
    >
      <span>{{ $t("general.type") }}</span>
      <i class="material-symbols">{{ kindIcon }}</i>
    </p>

    <p
      v-if="showCreatedColumn"
      :class="columnClass('created')"
      class="created"
      role="button"
      tabindex="0"
      @click="sort('created')"
      @keydown.enter.prevent.stop="sort('created')"
      @keydown.space.prevent.stop="sort('created')"
      :title="columnTitle('created', 'files.sortByCreationTime')"
      :aria-label="$t('files.sortByCreationTime')"
    >
      <i class="material-symbols">{{ createdIcon }}</i>
      <span>{{ $t("files.creationTime") }}</span>
    </p>

    <p
      v-if="hasDuration"
      :class="columnClass('duration')"
      class="duration"
      role="button"
      tabindex="0"
      @click="sort('duration')"
      @keydown.enter.prevent="sort('duration')"
      @keydown.space.prevent="sort('duration')"
      :title="columnTitle('duration', 'files.sortByDuration')"
      :aria-label="$t('files.sortByDuration')"
    >
      <i class="material-symbols">{{ durationIcon }}</i>
      <span>{{ $t("files.duration") }}</span>
    </p>
    <span v-if="quickDownloadEnabled" class="placeholder"></span>
    <SortSourceMenu
      v-if="sortMenu"
      :x="sortMenu.x"
      :y="sortMenu.y"
      :title="$t('files.sortingOptions')"
      :status="sortMenuStatus"
      :groups="sortMenuGroups"
      @select="onSortMenuSelect"
      @close="sortMenu = null"
    />
  </div>
</template>

<script>
import { state, getters, mutations } from "@/store";
import SortSourceMenu from "@/components/files/SortSourceMenu.vue";
import { COLUMN_LABEL_KEYS, nextListingSortAsc, OWN_SORT_SOURCES, sortDirectionLabel, sortSourceLabel } from "@/utils/sortSource.js";

export default {
  name: "ListingHeader",
  components: { SortSourceMenu },
  props: {
    hasDuration: {
      type: Boolean,
      default: false,
    },
    /** When true, sort via pickerSorting (destination pickers) instead of the main listing sort. */
    usePickerSorting: {
      type: Boolean,
      default: false,
    },
    useNavigationSorting: {
      type: Boolean,
      default: false,
    },
  },
  data() {
    return {
      /** fufu: open sort menu position, or null. */
      sortMenu: null,
    };
  },
  computed: {
    /** fufu: the main listing's header shows where its sort comes from (not pickers). */
    showsSortSource() {
      return !this.usePickerSorting && !this.useNavigationSorting;
    },
    /** fufu: { source, from } of the main listing's sort (utils/sortSource.js), or null. */
    sortSourceInfo() {
      return this.showsSortSource ? getters.sortingSource() : null;
    },
    sortSource() {
      return this.sortSourceInfo?.source ?? null;
    },
    sortMenuStatus() {
      const { by, asc } = this.sortConfig;
      const t = this.$t.bind(this);
      return [
        this.$t(COLUMN_LABEL_KEYS[by] || "general.name"),
        sortDirectionLabel(t, by, asc),
        sortSourceLabel(t, this.sortSource, this.sortSourceInfo?.from),
      ].join(" · ");
    },
    /** fufu: the options menu's actions; one with nothing to do is greyed out. */
    sortMenuGroups() {
      if (!this.sortMenu) return [];
      const { by, asc } = this.sortConfig;
      const global = state.globalSorting;
      const own = OWN_SORT_SOURCES.includes(this.sortSource);
      const savedHere = getters.sortingSavedUnder();
      const anythingSaved = getters.sortingSavedCount() > 0 || global !== null;
      return [
        [
          {
            action: "apply-recursive",
            icon: "account_tree",
            label: this.$t("files.applySortRecursively"),
            disabled: this.sortSource === "tree" && getters.sortingSavedUnder({ includeSelf: false }) === 0,
          },
          {
            action: "apply-global",
            icon: "public",
            label: this.$t("files.applySortGlobally"),
            disabled: global !== null && global.by === by && global.asc === asc,
          },
        ],
        [
          { action: "reset-folder", icon: "folder", label: this.$t("files.resetFolderSort"), disabled: !own },
          { action: "reset-recursive", icon: "folder_delete", label: this.$t("files.resetSortRecursively"), disabled: savedHere === 0 },
          { action: "reset", icon: "restart_alt", label: this.$t("files.resetSortToDefault"), disabled: !anythingSaved },
        ],
        [{ action: "manage", icon: "tune", label: this.$t("files.manageSortingOptions"), disabled: false }],
      ];
    },
    isMobile() {
      return getters.isMobile();
    },
    sortConfig() {
      if (this.useNavigationSorting) return getters.navigationSorting();
      return this.usePickerSorting ? getters.pickerSorting() : getters.sorting();
    },
    nameSorted() {
      return this.sortConfig.by === "name";
    },
    sizeSorted() {
      return this.sortConfig.by === "size";
    },
    modifiedSorted() {
      return this.sortConfig.by === "modified";
    },
    createdSorted() {
      return this.sortConfig.by === "created";
    },
    kindSorted() {
      return this.sortConfig.by === "kind";
    },
    durationSorted() {
      return this.sortConfig.by === "duration";
    },
    ascOrdered() {
      return this.sortConfig.asc;
    },
    galleryView() {
      return getters.viewMode() === "gallery";
    },
    isListMode() {
      const mode = getters.viewMode();
      return mode === "list" || mode === "compact";
    },
    showKindColumn() {
      return this.isListMode && !this.usePickerSorting && !this.useNavigationSorting && state.user?.showTypeColumn;
    },
    showCreatedColumn() {
      return this.isListMode && !this.usePickerSorting && !this.useNavigationSorting && state.user?.showCreationDateColumn;
    },
    // fufu: the arrow points the way of the sort: up = ascending (A→Z, smallest, oldest,
    // shortest first), down = descending. An unsorted column shows up, what a click on it
    // gives (sortArrow).
    nameIcon() {
      return this.sortArrow(this.nameSorted);
    },
    sizeIcon() {
      return this.sortArrow(this.sizeSorted);
    },
    modifiedIcon() {
      return this.sortArrow(this.modifiedSorted);
    },
    createdIcon() {
      return this.sortArrow(this.createdSorted);
    },
    kindIcon() {
      return this.sortArrow(this.kindSorted);
    },
    durationIcon() {
      return this.sortArrow(this.durationSorted);
    },
    quickDownloadEnabled() {
      if (this.usePickerSorting || this.useNavigationSorting) {
        return false;
      }
      if (getters.isMobile()) {
        return false
      }
      if (getters.isShare()) {
        return state.shareInfo?.quickDownload;
      }
      return state.user?.quickDownload && !this.galleryView;
    },
  },
  methods: {
    /** fufu: arrow icon of a column: its direction when sorted, else ascending. */
    sortArrow(sorted) {
      return sorted && !this.ascOrdered ? "arrow_downward" : "arrow_upward";
    },
    /** fufu: the active column's look says where its sort comes from. */
    columnClass(field) {
      const active = this.sortConfig.by === field;
      return {
        active,
        [`sort-source-${this.sortSource}`]: active && this.sortSource !== null,
      };
    },
    /** fufu: the active column's tooltip, e.g. "Newest first · this folder". */
    columnTitle(field, sortByKey) {
      if (this.sortConfig.by !== field || this.sortSource === null) {
        return this.$t(sortByKey);
      }
      const t = this.$t.bind(this);
      return `${sortDirectionLabel(t, field, this.sortConfig.asc)} · ${sortSourceLabel(t, this.sortSource, this.sortSourceInfo?.from)}`;
    },
    openSortMenu(event) {
      if (!this.showsSortSource || getters.currentView() !== "listingView") {
        return; // the browser's own menu, as before
      }
      event.preventDefault();
      event.stopPropagation();
      this.sortMenu = { x: event.clientX, y: event.clientY };
    },
    /** fufu: "Name (A→Z)", for the confirmations. */
    sortDescription() {
      const { by, asc } = this.sortConfig;
      return `${this.$t(COLUMN_LABEL_KEYS[by] || "general.name")} (${sortDirectionLabel(this.$t.bind(this), by, asc)})`;
    },
    /** fufu: asks first; runs `run` on the confirm button. */
    confirmSortAction(title, body, confirmLabel, run) {
      this.chooseSortAction(title, body, [{ label: confirmLabel, run }]);
    },
    /** fufu: asks first, with Cancel and one button per choice ({ label, run }). */
    chooseSortAction(title, body, choices) {
      mutations.showPrompt({
        name: "generic",
        props: {
          title,
          body,
          buttons: [
            { label: this.$t("general.cancel"), className: "button--grey", action: () => mutations.closeTopPrompt() },
            ...choices.map(({ label, run }) => ({
              label,
              action: () => {
                mutations.closeTopPrompt();
                run();
              },
            })),
          ],
        },
      });
    },
    onSortMenuSelect(action) {
      this.sortMenu = null;
      switch (action) {
        case "apply-recursive": {
          const count = getters.sortingSavedUnder({ includeSelf: false });
          const title = this.$t("files.applySortRecursivelyTitle");
          const body = this.$t("files.applySortRecursivelyBody", { sort: this.sortDescription() });
          if (count === 0) {
            this.confirmSortAction(title, body, this.$t("files.sortApply"), () => mutations.applySortingRecursively());
          } else {
            // Subfolders with their own sorting option: keep theirs, or reset them too.
            this.chooseSortAction(title, `${body} ${this.$t("files.sortSubfoldersHaveOwn", count)}`, [
              { label: this.$t("files.sortKeepTheirs"), run: () => mutations.applySortingRecursively({ override: false }) },
              { label: this.$t("files.sortOverride"), run: () => mutations.applySortingRecursively({ override: true }) },
            ]);
          }
          break;
        }
        case "apply-global": {
          const count = getters.sortingSavedCount();
          const body = [this.$t("files.applySortGloballyBody", { sort: this.sortDescription() })];
          if (count > 0) body.push(this.$t("files.sortFoldersKeepOwn", count));
          this.confirmSortAction(this.$t("files.applySortGloballyTitle"), body.join(" "), this.$t("files.sortApply"),
            () => mutations.applySortingGlobally());
          break;
        }
        case "reset-folder":
          if (this.sortSource === "tree") {
            this.confirmSortAction(this.$t("files.resetFolderSortTitle"), this.$t("files.resetFolderRuleBody"), this.$t("files.sortReset"),
              () => mutations.clearFolderSorting());
          } else {
            mutations.clearFolderSorting();
          }
          break;
        case "reset-recursive": {
          const count = getters.sortingSavedUnder();
          this.confirmSortAction(this.$t("files.resetSortRecursivelyTitle"),
            `${this.$t("files.resetSortRecursivelyBody")} ${this.$t("files.sortSavedRemoved", count)}`, this.$t("files.sortReset"),
            () => mutations.clearSortingRecursively());
          break;
        }
        case "manage":
          mutations.showPrompt({ name: "SortingManager", props: { title: this.$t("files.savedSortingOptions") } });
          break;
        case "reset": {
          const count = getters.sortingSavedCount();
          const body = [this.$t("files.resetSortBody")];
          if (count > 0) body.push(this.$t("files.sortSavedRemoved", count));
          this.confirmSortAction(this.$t("files.resetSortTitle"), body.join(" "), this.$t("files.sortReset"),
            () => mutations.resetSortingToDefault());
          break;
        }
      }
    },
    sort(field) {
      // fufu: the direction is decided here instead of read off the arrow icons: picker and
      // navigation headers flip the sorted column, else ascending (as upstream); the main
      // listing follows nextListingSortAsc() (a click in a folder without its own sort saves
      // the shown direction).
      const current = this.sortConfig;
      const asc = this.showsSortSource
        ? nextListingSortAsc(current, this.sortSource, field)
        : current.by === field ? !current.asc : true;
      // Commit the updateSort mutation
      if (this.useNavigationSorting) {
        mutations.updateNavigationSortConfig({ field, asc });
        return;
      }
      if (this.usePickerSorting) {
        mutations.updatePickerSortConfig({ field, asc });
        return;
      }
      mutations.updateListingSortConfig({ field, asc });
      mutations.updateListingItems();
    },
  },
};
</script>

<style scoped>
.listing-item-header {
  display: flex;
  background: var(--surfacePrimary);
  border: 1px solid var(--divider);
  z-index: 999;
  padding: .85em;
  width: 100%;
  box-sizing: border-box;
  border-top-left-radius: 1em;
  border-top-right-radius: 1em;
  margin-bottom: 0 !important;
  justify-content: space-between;
  user-select: none;
}

p {
  margin: 0;
  cursor: pointer;
  box-sizing: border-box;
  display: flex;
  align-items: center;
  width: 100%;
}

span {
  vertical-align: middle;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.name {
  flex: 1;
  min-width: 0;
}

.size,
.modified,
.duration {
  flex: 1;
  justify-content: flex-end;
  text-align: end;
}

/* Column widths are shared with the list/compact rows (see listing.css) so the
   header lines up with the items and the name column gets the remaining space. */
/* stylelint-disable no-unknown-custom-properties -- defined in css/listing.css */
.desktop-view {
  /* same right inset as the list/compact rows */
  padding-right: var(--listing-col-gap);
}

/* With quick download the rows end in a ~2.4rem download icon after the inset */
.desktop-view:has(> .placeholder) {
  padding-right: 0;
}

.desktop-view > .placeholder {
  flex: 0 0 auto;
  width: calc(var(--listing-col-gap) + 2.4rem);
}

.desktop-view .size,
.desktop-view .modified,
.desktop-view .kind,
.desktop-view .created,
.desktop-view .duration {
  flex: 0 0 auto;
  padding-left: var(--listing-col-gap);
  justify-content: flex-end;
  text-align: right;
}

.desktop-view .size {
  width: var(--listing-col-size);
}

.desktop-view .modified {
  width: var(--listing-col-modified);
}

.desktop-view .kind {
  width: var(--listing-col-kind);
  padding-left: calc(var(--listing-col-gap) + 0.5rem);
  justify-content: flex-start;
  text-align: left;
}

.desktop-view .kind span {
  flex-shrink: 0;
}

.desktop-view .created {
  width: var(--listing-col-created);
}

.desktop-view .duration {
  width: var(--listing-col-duration);
}
/* stylelint-enable no-unknown-custom-properties */

i {
  font-size: 1.2em;
  vertical-align: middle;
  margin-left: .1em;
  opacity: 0;
  transition: opacity 0.1s ease;
  flex-shrink: 0;
}

.active i,
p:hover i,
.active:hover i {
  opacity: 1;
}

.active,
p:hover {
  font-weight: bold;
}

/* fufu: where the active column's sort comes from (utils/sortSource.js): its own (also as a
   rule for subfolders) = accent color, bold; inherited from a parent's rule = accent color,
   normal weight; global and default = normal weight. The arrow is always shown. */
.active.sort-source-folder,
.active.sort-source-tree,
.active.sort-source-inherited {
  color: var(--primaryColor);
}

.active.sort-source-inherited,
.active.sort-source-global,
.active.sort-source-default {
  font-weight: normal;
}

.active.sort-source-inherited:hover,
.active.sort-source-global:hover,
.active.sort-source-default:hover {
  font-weight: bold;
}
</style>
