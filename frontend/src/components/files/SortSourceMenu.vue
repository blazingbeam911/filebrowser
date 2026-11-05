<template>
  <!-- fufu: right-click menu of the listing's sort header ("Sorting options", utils/sortSource.js). -->
  <Teleport to="body">
    <div
      ref="menu"
      class="sort-source-menu no-select"
      role="menu"
      :aria-label="title"
      :style="{ top: `${top}px`, left: `${left}px` }"
      @contextmenu.prevent
    >
      <!-- The current sort (e.g. "Name · A→Z · this folder") is the title's tooltip. -->
      <p class="sort-source-menu-line sort-source-menu-title" :title="status">{{ title }}</p>
      <template v-for="(group, index) in groups" :key="index">
        <hr class="sort-source-menu-divider" />
        <button
          v-for="item in group"
          :key="item.action"
          type="button"
          role="menuitem"
          class="sort-source-menu-line sort-source-menu-item"
          :disabled="item.disabled"
          :data-action="item.action"
          @click="$emit('select', item.action)"
        >
          <i class="material-symbols">{{ item.icon }}</i>
          <span>{{ item.label }}</span>
        </button>
      </template>
    </div>
  </Teleport>
</template>

<script>
const MARGIN = 8;

export default {
  name: "SortSourceMenu",
  props: {
    x: { type: Number, required: true },
    y: { type: Number, required: true },
    title: { type: String, required: true },
    status: { type: String, required: true },
    /** Lists of { action, icon, label, disabled }, separated by dividers. */
    groups: { type: Array, default: () => [] },
  },
  emits: ["select", "close"],
  data() {
    return { top: this.y, left: this.x };
  },
  mounted() {
    this.place();
    // Next tick: the event that opened the menu mustn't close it again.
    setTimeout(() => {
      document.addEventListener("pointerdown", this.onPointerDown, true);
    }, 0);
    document.addEventListener("keydown", this.onKeydown, true);
    window.addEventListener("scroll", this.close, true);
    window.addEventListener("resize", this.close);
  },
  beforeUnmount() {
    document.removeEventListener("pointerdown", this.onPointerDown, true);
    document.removeEventListener("keydown", this.onKeydown, true);
    window.removeEventListener("scroll", this.close, true);
    window.removeEventListener("resize", this.close);
  },
  methods: {
    /** At the pointer, kept inside the viewport. */
    place() {
      const el = this.$refs.menu;
      if (!el) return;
      const { width, height } = el.getBoundingClientRect();
      this.left = Math.max(MARGIN, Math.min(this.x, window.innerWidth - width - MARGIN));
      this.top = Math.max(MARGIN, Math.min(this.y, window.innerHeight - height - MARGIN));
    },
    onPointerDown(event) {
      if (!this.$refs.menu?.contains(event.target)) {
        this.close();
      }
    },
    onKeydown(event) {
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        this.close();
      }
    },
    close() {
      this.$emit("close");
    },
  },
};
</script>

<style scoped>
.sort-source-menu {
  position: fixed;
  z-index: 1000;
  background-color: var(--background);
  color: var(--textPrimary);
  min-width: 14em;
  max-width: calc(100vw - 16px);
  border-radius: var(--borderRadius);
  padding: 0.35em 0;
  box-shadow: 0 0.2em 1em rgba(0, 0, 0, 0.35);
  display: flex;
  flex-direction: column;
  align-items: stretch;
  font-size: 1rem;
  animation: .1s sort-source-menu-show forwards;
}

@keyframes sort-source-menu-show {
  from { opacity: 0; }
  to { opacity: 1; }
}

/* Title and items: one font size, one padding, all left-aligned. */
.sort-source-menu-line {
  box-sizing: border-box;
  width: 100%;
  margin: 0;
  padding: 0.5em 0.9em;
  font-size: 1em;
  line-height: 1.3;
  text-align: left;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.sort-source-menu-title {
  font-weight: bold;
  cursor: default;
}

.sort-source-menu-divider {
  width: 100%;
  margin: 0.35em 0;
  border: 0;
  border-top: 1px solid var(--divider);
}

.sort-source-menu-item {
  display: flex;
  justify-content: flex-start; /* app buttons center their content */
  align-items: center;
  gap: 0.6em;
  min-height: 0;
  border: 0;
  border-radius: 0;
  background: transparent;
  color: inherit;
  font-family: inherit;
  cursor: pointer;
}

/* App buttons grow on hover (scale 1.02), which shifts a full-width row sideways. */
.sort-source-menu-item:hover {
  transform: none;
}

.sort-source-menu-item i {
  font-size: 1.25em;
  line-height: 1;
  flex-shrink: 0;
}

.sort-source-menu-item span {
  overflow: hidden;
  text-overflow: ellipsis;
}

.sort-source-menu-item:disabled {
  opacity: 0.35;
  cursor: not-allowed;
  box-shadow: none !important;
}
</style>
