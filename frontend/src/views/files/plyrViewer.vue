<template>
  <div class="plyr-viewer" @wheel.capture="handleWheelVolume">
    <!-- Audio with plyr -->
    <div
      v-if="previewType === 'audio'"
      ref="audioPlayerGestureRoot"
      class="audio-player-container audio-player-container--plyr-gestures"
      :class="{ 'audio-player-container--lyrics-open': isMobile && showMobileLyrics && lyrics.length }"
    >
      <!-- Desktop panel button, will auto‑hide only when panel is closed -->
      <FloatingActionButton
        v-if="previewType === 'audio' && !isMobile"
        :icon="showDesktopPanel ? 'close' : 'queue_music'"
        :badge="!showDesktopPanel ? queueCount : null"
        :auto-hide="!showDesktopPanel"
        :offset="{ zIndex: 9999 }"
        @click="showDesktopPanel = !showDesktopPanel"
        :label="desktopPanelToggleLabel"
      />

      <!-- Two‑column layout -->
        <div class="audio-player-content" :class="{ 'panel-open': !isMobile && showDesktopPanel }" >
        <!-- Left column: art + meta -->
        <div class="audio-left-column">
          <!-- Album art with a generic icon if no image/metadata -->
          <div class="album-art-container"
              :class="{ 'no-artwork': !albumArtUrl }"
              :style="{
                width: `${displayArtSize}em`,
                maxWidth: '100%',
                aspectRatio: '1 / 1'
              }"
              @mouseenter="onAlbumArtHover"
              @mouseleave="onAlbumArtLeave"
              @wheel="onAlbumArtScroll">
            <img class="no-select album-art" v-if="albumArtUrl" :src="albumArtUrl" :alt="metadata.album || 'Album art'" />
            <div v-else class="album-art-fallback">
              <i class="material-symbols">music_note</i>
            </div>
          </div>

          <!-- Metadata info -->
          <div class="audio-metadata" v-if="metadata">
            <div class="audio-title">
              {{ metadata.title }}
            </div>
            <div class="audio-artist" v-if="metadata.artist">
              {{ formattedArtist }}
            </div>
            <div class="audio-album" v-if="metadata.album">
              {{ metadata.album }}
            </div>
            <div class="audio-year" v-if="metadata.album">
              {{ metadata.year }}
              <span class="filetype-badge">{{ filetype }}</span>
            </div>
          </div>
        </div>

        <!-- Right column: panel with Queue & Lyrics tabs (desktop) -->
        <Transition name="panel-slide">
          <AudioPanel
            v-if="!isMobile && showDesktopPanel"
            :lyrics="lyrics"
            :lyrics-meta="lyricsMeta"
            :active-lyric-index="activeLyricIndex"
            :active-word-index="activeWordIndex"
            :player="player"
            :audio-context="audioContext"
            :audio-source="audioSource"
            class="lyrics-panel"
            @seek="seekToLyric"
            @needs-audio-graph="setupAudioVisualizer"
          />
        </Transition>
      </div>

      <!-- Mobile inline lyrics -->
      <div v-if="isMobile && showMobileLyrics && lyrics.length" class="lyrics-mobile">
        <!-- Scrollable area -->
        <div class="lyrics-mobile-scrollable" ref="lyricsMobileScrollable">
          <p v-if="lyricsMeta" class="lyrics-meta-header" aria-hidden="true">{{ lyricsMeta }}</p>
          <p
            v-for="(line, i) in lyrics"
            :key="i"
            :class="{ active: syncedLyrics && lyrics[i].timestamp === lyrics[activeLyricIndex]?.timestamp }"
            class="lyric-line"
            @click.stop="syncedLyrics && seekToLyric(line.timestamp)"
            tabindex="0"
            role="button"
            :aria-label="syncedLyrics ? `Seek to ${line.text}` : undefined"
          >
            <template v-if="i === activeLyricIndex && line.words && line.words.length">
              <span
                v-for="(word, w) in line.words"
                :key="w"
                class="lyric-word"
                :class="{ sung: w <= activeWordIndex, current: w === activeWordIndex }"
              >{{ word.text }}&nbsp;</span>
            </template>
            <template v-else>{{ line.text }}</template>
          </p>
        </div>
      </div>

      <!-- Audio controls -->
      <div class="audio-controls-container">
        <div class="plyr-audio-container" ref="plyrAudioContainer">
          <audio :src="raw" :type="req.type" :autoplay="shouldAutoplay" @play="handlePlay" ref="audioElement"></audio>
        </div>
      </div>
    </div>

    <!-- Video with plyr -->
    <div
      v-else-if="previewType === 'video'"
      ref="videoPlayerContainer"
      class="video-player-container"
      :class="{ 'no-captions': !hasSubtitles }"
    >
      <div class="plyr-video-container" ref="plyrVideoContainer">
        <video
          ref="videoElement"
          :type="req.type"
          preload="none"
          :src="nativeVideoSrc"
          :poster="posterUrl"
          :autoplay="videoElementAutoplay"
          @play="handlePlay"
          playsinline
        >
          <track kind="captions" v-for="(sub, index) in subtitlesList" :key="index" :src="sub.src"
            :label="sub.name || subtitleTrackLabel(sub)" :srclang="sub.srclang || sub.language" />
        </video>
      </div>
      <div
        v-if="videoPlaybackLoading"
        class="video-loading-overlay"
        aria-hidden="true"
        aria-busy="true"
      >
        <LoadingSpinner size="large" />
      </div>
    </div>

    <!-- Feedback for seeks, speed and volume, over either player -->
    <div v-if="osdText" class="media-osd" role="status">{{ osdText }}</div>
    <dl v-if="statsVisible" class="media-stats">
      <template v-for="row in statsRows" :key="row.label">
        <dt>{{ row.label }}</dt>
        <dd>{{ row.value }}</dd>
      </template>
    </dl>
    <div
      class="media-volume-bar"
      :class="{ 'media-volume-bar--visible': volumeBarVisible }"
      aria-hidden="true"
    >
      <div class="media-volume-bar__fill" :style="{ height: `${volumeBarPercent}%` }"></div>
    </div>
    <div
      class="video-skip-feedback-layer"
      :class="{
        'video-skip-feedback-layer--visible': skipFeedbackVisible,
        'video-skip-feedback-layer--left': skipFeedbackSide === 'left',
        'video-skip-feedback-layer--right': skipFeedbackSide === 'right',
      }"
      aria-hidden="true"
    >
      <span
        :key="skipFeedbackKey"
        class="video-skip-feedback-layer__icon"
        :class="{ 'video-skip-feedback-layer__icon--held': skipFeedbackHeld }"
      >
        <svg viewBox="0 0 960 960" focusable="false"><path :d="skipFeedbackPath" /></svg>
      </span>
    </div>

    <!-- Right detection zone – always for video/mobile audio queue button & desktop panel toggle -->
    <FloatingActionButton
      v-if="showQueueButton"
      icon="queue_music"
      :badge="queueCount"
      group="plyr-controls"
      @click="showQueuePrompt"
      :label="queueButtonLabel"
    />

    <!-- Lyrics button (left side) – only on mobile when lyrics exist -->
    <FloatingActionButton
      v-if="isMobile && lyrics.length"
      icon="lyrics"
      position="top-left"
      zone-height="8.5em"
      group="plyr-controls"
      @click="showMobileLyrics = !showMobileLyrics"
      :label="lyricsToggleLabel"
    />

    <!-- Lyrics scroll lock (mobile, bottom‑right) – visible while lyrics overlay is open -->
    <FloatingActionButton
      v-if="isMobile && previewType === 'audio' && showMobileLyrics && lyrics.length && syncedLyrics"
      icon="lock"
      :icon-outlined="mobileLyricsScrollLocked"
      position="bottom-right"
      size="small"
      :offset="{ bottom: 'calc(env(safe-area-inset-bottom, 0px) + 6rem)' }"
      :auto-hide="false"
      @click="mobileLyricsScrollLocked = !mobileLyricsScrollLocked"
      :label="mobileLyricsLockToggleLabel"
    />

    <!-- Toast when you change playback modes in the media player -->
    <div :class="['playback-toast', toastVisible ? 'visible' : '']">
      <!-- Loop icon for "single playback", "loop single file" and "loop all files" -->
      <i class="material-symbols">{{ toastIcon }}</i>
      <span>{{ playbackModeMessage }}</span>

      <!-- Status indicator for loop -->
      <span v-if="toastType === 'loop'" :class="[
          'status-indicator', loop !== 'off' ? 'status-on' : 'status-off',]"></span>
    </div>
    <!-- Speed toast (long-press) -->
    <div v-if="speedToastVisible" class="playback-toast visible">
      <i class="material-symbols">speed</i>
      <span>{{ speedToastMessage }}</span>
    </div>
  </div>
</template>

<script>
import Plyr from 'plyr';
import {
  buildPlaybackQueue,
  navigatePlaybackQueue,
  getEndOfMediaAction,
  getNextItem,
  cyclePlaybackModes,
  toggleSingleLoop,
  clearPlaybackQueue,
  getModeLabel,
  getModeIcon,
  getLoopLabel,
  getLoopIcon,
  formatArtist
} from '@/utils/playbackQueue.js';
import { getTypeFromMime } from '@/utils/files.js';
import { buildItemUrl } from '@/utils/url.js';
import AudioPanel from "@/components/files/AudioPanel.vue";
import { getters, mutations, state } from '@/store';
import { getObjectProperty } from '@/utils/object.js';
import { globalVars } from '@/utils/constants';
import { getSubtitleFormatExtension } from '@/utils/subtitles';
import { PLYR_EDITABLE_SELECTOR, isTextEntryTarget } from '@/plyr/plyrKeyTargets';
import {
  parseStoredSpeed,
  steppedSpeed,
  mediaKeyAction,
  wheelVolumeStep,
  seekIconPath,
} from '@/plyr/plyrMediaKeys.js';
import {
  DEFAULT_FRAME_SECONDS,
  createFrameTracker,
  frameOffset,
  frameStepTarget,
} from '@/plyr/plyrFrameTracker.js';
import {
  EMPTY_AB_LOOP,
  abLoopActive,
  abLoopMarkers,
  nextAbLoop,
} from '@/plyr/plyrAbLoop.js';
import { buildStatsRows } from '@/plyr/plyrStats.js';
import {
  THEATER_BAR_HIDDEN_CLASS,
  applyTheaterLayout,
  claimTheaterLayout,
  readTheaterSetting,
  releaseTheaterLayout,
  writeTheaterSetting,
} from '@/utils/theaterMode.js';
import { holdVideoFrame, releaseVideoFrame, settleVideoFrame } from '@/utils/videoSwitchHold.js';
import { pageFullscreen } from '@/utils/pageFullscreen.js';
import { getPreviewURL, getPreviewURLPublic } from '@/api/resources';
import {
  blockPlyrSeekOnInput,
  enablePlyrSeekOnRelease,
} from '@/plyr/plyrSeekOnRelease';
import { enablePlyrScrubPreview } from '@/plyr/plyrScrubPreview';
import { enablePlyrVideoLoadingIndicator } from '@/plyr/plyrVideoLoading';
import { enableOverlaidHintController } from '@/plyr/overlaidHintController';
import {
  appendMediaFragment,
  clearPendingInlineResumeFor,
  exitPictureInPictureProgrammatically,
  getPendingInlineResume,
  hasActiveSession as hasActivePipSession,
  initPipSession,
  isPipBlockedForPreview,
  onPendingInlineResume,
  onPipAvailabilityChange,
  pendingMatchesPreview,
  registerSession as registerPipSession,
  resolvePipMediaKey,
  setPendingInlineResume,
  shouldDeferVideoStreamAttach,
  takeSessionSnapshot,
} from '@/plyr/pipSession.js';
import LoadingSpinner from '@/components/LoadingSpinner.vue';
import FloatingActionButton from '@/components/settings/FloatingActionButton.vue';
import {
  parsePlaybackTimeFromQuery,
  playbackQueryChanged,
} from '@/utils/playbackQuery';
import { isInLeftNavTapZone, isInRightNavTapZone } from '@/utils/navigationEdgeZones.js';
import { zoneFromClientX } from '@/plyr/gestureZones.js';
import { ownsMediaSession as ownsMediaSessionSlot } from '@/utils/mediaSessionOwnership';

const PLYR_CAPTION_SIZE_IDS = ['small', 'medium', 'large', 'xlarge'];
/** Same localStorage key Plyr uses for `captions`, `language`, etc. (see Plyr defaults `storage.key`). */
const PLYR_LOCALSTORAGE_KEY = 'plyr';
/** Custom field inside Plyr’s JSON blob so caption size travels with other Plyr prefs. */
const PLYR_CAPTION_SIZE_FIELD = 'captionSize';
/** Per-device video speed; separate from Plyr's `speed` field, which audio files write too. */
const VIDEO_SPEED_STORAGE_KEY = 'fbVideoSpeed';
const AUDIO_SPEED_STORAGE_KEY = 'fbAudioSpeed';
const OSD_DURATION_MS = 1500;
const FRAME_STEP_TIMEOUT_MS = 1500;
const WHEEL_VOLUME_STEP = 0.05;
const THEATER_BAR_LEAVE_MS = 250;
const VIDEO_SWITCH_SETTLE_MS = 1500;
const STATS_BRIEF_MS = 5000;
const STATS_UPDATE_MS = 1000;
// Stays on from one file to the next, until turned off or the page reloads.
let statsPinned = false;
// Places in the viewer where the wheel scrolls content.
const WHEEL_SCROLLS_HERE = '.plyr__menu__container, .lyrics-panel, .lyrics-mobile';
const AB_LOOP_POLL_MS = 30;
let pendingFullscreenResume = false;
// Set while moving between files, so hidden controls don't flash up on the next player.
let pendingControlsHidden = false;
let pendingPipResume = false;
let pendingResumeTimestamp = 0;
let mediaSessionOwner = 0;

export default {
  name: "plyrViewer",
  components: {
    AudioPanel,
    LoadingSpinner,
    FloatingActionButton,
  },
  props: {
    previewType: {
      type: String,
      required: true,
    },
    raw: {
      type: String,
      required: true,
    },
    subtitlesList: {
      type: Array,
      default: () => [],
    },
    lyrics: {
      type: Array,
      default: () => [],
    },
    req: {
      type: Object,
      required: true,
    },
    autoPlayEnabled: {
      type: Boolean,
      default: false,
    },
    listing: {
      type: Array,
      default: () => [],
    },
  },
  emits: ['play', 'navigate-previous', 'navigate-next', 'close-preview'],
  data() {
    return {
      // Toast
      toastVisible: false,
      toastTimeout: null,
      toastType: 'mode', // 'mode' for the playback modes and 'loop'... for loop.

      // Metadata & Art
      metadata: null, // Null by default, will be loaded from the audio file.
      albumArtUrl: null,
      albumArtSize: parseFloat(sessionStorage.getItem('plyrAlbumArtSize')) || 24,
      isHovering: false, // Track hover state

      // Lyrics
      activeLyricIndex: -1,
      activeWordIndex: -1,
      doubleTapSeekCleanup: null,
      rewindForwardFeedback: null,
      mobileLyricsScrollLocked: false,

      // Audio Visualizer
      audioContext: null,
      audioSource: null,
      audioGraphInitialized: false,

      // Playback settings
      playbackMenuInitialized: false,
      loopMenuInitialized: false,
      lastAppliedMode: null,
      showDesktopPanel: localStorage.getItem('plyrShowDesktopPanel') === '1',
      showMobileLyrics: sessionStorage.getItem('plyrShowMobileLyrics') === '1',
      isFullscreen: false,

      // Gestures
      skipFeedbackVisible: false,
      skipFeedbackSide: 'left',
      skipFeedbackPath: seekIconPath(-10),
      skipFeedbackHeld: false,
      skipFeedbackKey: 0,
      skipFeedbackTimer: null,
      skipNextTap: false,
      skipNextTapTimer: null, // Timer for clearing skipNextTap
      edgeTapLastTime: 0,
      edgeTapLastZone: null,
      ignoreClickUntil: 0,

      hasStartedPlayback: false,
      overlaidHintApi: null,
      overlaidHintCleanup: null,

      // Plyr video: full-frame edge gestures
      videoEdgeKind: null,
      videoEdgeStartX: 0,
      videoEdgeStartY: 0,
      videoEdgeDx: 0,
      videoEdgeDy: 0,
      videoDragOffsetX: 0,
      videoDragOffsetY: 0,
      videoGestureSnapBack: false,
      videoEdgeMouseActive: false,
      videoShowNavHint: false,
      videoNavHintDir: 'next',
      videoShowDismissHint: false,
      videoDismissFlashActive: false,
      videoEdgeHintPx: 44,
      videoEdgeCommitX: 130,
      videoEdgeCommitY: 110,
      videoEdgeRubberMax: 100,
      videoSwipeCleanup: null,
      /** When audio scrub/menu steals touchstart, ignore move/end for this touch id (Plyr has no video wrapper). */
      videoSwipeSuppressedTouchId: null,
      videoDismissCloseTimer: null,
      videoDismissHintTimer: null,

      // Long-press for 2x speed (only for touch)
      longPressTimer: null,
      longPressPending: false,
      longPressTriggered: false,
      longPressPreviousSpeed: 1,
      speedToastVisible: false,
      speedToastMessage: '',

      // On-screen feedback for the player keys
      osdText: '',
      osdTimer: null,
      volumeBarVisible: false,
      volumeBarPercent: 0,
      volumeBarTimer: null,
      wheelAccumulated: 0,
      statsVisible: false,
      statsRows: [],
      statsUpdateTimer: null,
      statsBriefTimer: null,
      theaterMode: false,
      theaterHeader: null,
      theaterLeaveTimer: null,

      frameTracker: null,
      frameStepOrigin: null,
      frameStepLastTarget: null,
      frameStepTimer: null,
      abLoop: EMPTY_AB_LOOP,
      // False after a seek past B, so moving there on purpose doesn't jump back.
      abLoopArmed: true,
      abLoopPollTimer: null,

      // Plyr instance
      player: null,
      captionSizeMenuInitialized: false,
      /** Native video: defer stream URL until play so opening preview does not range-fetch the file. */
      videoStreamAttached: false,
      attachVideoStreamResume: null,
      mediaSessionOwnerId: 0,
      nativePlayerPlay: null,
      /** Centered spinner while the video is loading or buffering. */
      videoPlaybackLoading: false,
      seekOnReleaseCleanup: null,
      scrubPreviewCleanup: null,
      videoLoadingCleanup: null,
      querySeekMetadataHandler: null,
      plyrTeardownDone: false,
      /**
       * Set first thing in beforeUnmount. A player must never be built after that: it would sit
       * on a detached <video> with Plyr's global key handler still on window (e.g. f then fails in
       * Firefox, which allows one fullscreen request per key press).
       */
      viewerUnmounted: false,
      /** Inline resume after PiP handoff; applied when the video stream attaches. */
      inlineResumeSnapshot: null,
      /** Seek target passed via Media Fragments URI (#t=) when handing off from PiP. */
      pipHandoffSeekSeconds: null,
      pipReadinessHandler: null,
      pipAvailabilityCleanup: null,
      pipPendingResumeCleanup: null,
      /** Source+path this instance mounted for; guards stale instances during route transitions. */
      mountedPreviewKey: null,
      pipHandoffApplying: false,
      /** Keeps :src stable after PiP handoff so Vue does not rebind without #t=. */
      lockedNativeVideoSrc: null,
    };
  },
  watch: {
    async playbackMode(newMode, oldMode) {
      if (newMode !== oldMode) {
        if (oldMode !== undefined) {
          // can only be "single" via clearPlaybackQueue
          this.toastType = newMode === 'single' ? 'cleared' : 'mode';
          this.showToast();
        }
        this.updateMediaSessionNavHandlers();
        await this.$nextTick();
        this.ensurePlaybackModeApplied();
      }
    },
    async loop(newVal, oldVal) {
      if (newVal !== oldVal) {
        this.toastType = 'loop';
        this.showToast();
        this.updateMediaSessionNavHandlers();
        await this.$nextTick();
        this.ensurePlaybackModeApplied();
      }
    },
    currentQueueIndex() {
      this.updateMediaSessionNavHandlers();
    },
    showDesktopPanel(val) {
      localStorage.setItem('plyrShowDesktopPanel', val ? '1' : '0');
    },
    albumArtSize(val) {
      sessionStorage.setItem('plyrAlbumArtSize', val.toString());
    },
    async activeLyricIndex() {
      if (this.showMobileLyrics && this.isMobile) {
        await this.$nextTick();
        this.scrollMobileLyrics();
      }
    },
    lyrics(newLyrics, oldLyrics) {
      if (newLyrics !== oldLyrics) {
        this.activeLyricIndex = -1;
        this.activeWordIndex = -1;
      }
    },
    async mobileLyricsScrollLocked(val) {
      if (!val && this.showMobileLyrics && this.lyrics.length) {
        await this.$nextTick();
        this.scrollMobileLyrics();
      }
    },
    async showMobileLyrics(val) {
        sessionStorage.setItem('plyrShowMobileLyrics', val ? '1' : '0');
        if (val && this.lyrics.length) {
            await this.$nextTick();
            this.scrollMobileLyrics();
        }
    },
    shouldTogglePlayPause(newVal, oldVal) {
      if (newVal !== oldVal) {
      this.togglePlayPause();
      }
    },
    isPlaying(isPlaying) {
      if (this.previewType === 'video') {
        this.overlaidHintApi?.syncPlayback(isPlaying);
      }
    },
    'req.path'() {
      this.videoStreamAttached = false;
      this.pipHandoffSeekSeconds = null;
      this.lockedNativeVideoSrc = null;
      this.hasStartedPlayback = false;
      this.overlaidHintApi?.reset();
    },
    routePath() {
      this.stopIfRouteLeftThisFile();
    },
    listing: {
      handler(newListing) {
        if (!newListing?.length) return;
        const isShare = getters.isShare();
        const getId = item => isShare ? item.name : item.path;
        const mediaFiles = newListing.filter(item => /^(audio|video)\//.test(item.type || ''));
        const queue = state.playbackQueue.queue;
        const mode = state.playbackQueue.mode;

        if (queue.length && mediaFiles.length === queue.length) {
          const currIds = mediaFiles.map(getId);
          const queIds = queue.map(getId);
          let match;
          if (mode === 'shuffle') {
            const set = new Set(queIds);
            match = currIds.every(id => set.has(id));
          } else {
            match = currIds.every((id, i) => id === queIds.at(i));
          }
          if (match) {
            const byId = new Map(mediaFiles.map(item => [getId(item), item]));
            const playbackQueue = queue.map(item => byId.get(getId(item)) || item);
            const currentItemId = getId(this.req);
            const newIndex = playbackQueue.findIndex(item => getId(item) === currentItemId);
            mutations.setPlaybackQueue({
              queue: playbackQueue,
              currentIndex: newIndex !== -1 ? newIndex : state.playbackQueue.currentIndex,
              mode,
              loop: this.loop
            });
            return;
          }
        }
        this.setupPlaybackQueue();
      },
      immediate: true
    },
    subtitlesList: {
      async handler(newSubs, oldSubs) {
        const gained = newSubs && newSubs.length > 0 && (!oldSubs || oldSubs.length === 0);
        const lost = (!newSubs || newSubs.length === 0) && oldSubs && oldSubs.length > 0;
        if (gained || lost) {
          this.captionSizeMenuInitialized = false;
        }
        if (gained) {
          if (!this.player && this.previewType === 'video') {
            await this.$nextTick();
            void this.initializePlyr();
          } else if (this.player && this.previewType === 'video') {
            await this.$nextTick();
            this.applyCustomSettings(this.player);
            this.syncCaptionSizeSettingsVisibility();
            this.applyCaptionSizeClass();
          }
        } else if (this.player && this.previewType === 'video') {
          await this.$nextTick();
          this.syncCaptionSizeSettingsVisibility();
          this.captionSizeMenuInitialized = false;
          this.applyCustomSettings(this.player);
        }
      },
      deep: true,
    },
    '$route.query': {
      handler(newQuery, oldQuery) {
        if (playbackQueryChanged(oldQuery, newQuery)) {
          this.applyQueryPlaybackSeek();
        }
      },
    },
  },
  computed: {
    routePath() {
      return state.route.path;
    },
    filetype() {
      return getTypeFromMime(this.req.type || '');
    },
    formattedArtist() {
      return formatArtist(this.metadata?.artist);
    },
    desktopPanelToggleLabel() {
      return this.showDesktopPanel ? this.$t('player.closePanel') : this.$t('player.openPanel');
    },
    queueButtonLabel() {
      return this.$t('player.QueueButtonHint');
    },
    lyricsToggleLabel() {
      return this.$t('player.toggleLyrics');
    },
    mobileLyricsLockToggleLabel() {
      return this.mobileLyricsScrollLocked ? this.$t('player.unlockLyrics') : this.$t('player.lockLyrics');
    },
    showQueueButton() {
      if (this.previewType === 'video') {
        if (this.isFullscreen) return false;
        return true;
      }
      if (this.isMobile && this.previewType === 'audio') return true;
      return false;
    },
    mobileLyricsActive() {
      return this.isMobile && this.showMobileLyrics && this.lyrics.length > 0;
    },
    displayArtSize() {
      if (this.isMobile && this.showMobileLyrics && this.lyrics.length) {
        return 5;
      }
      return this.albumArtSize;
    },
    queueCount() {
      return state.playbackQueue.queue.length || 0;
    },
    shouldTogglePlayPause() {
      return state.playbackQueue.shouldTogglePlayPause || false;
    },
    isPlaying() {
      return state.playbackQueue.isPlaying || false;
    },
    playbackQueue() {
      return state.playbackQueue.queue;
    },
    currentQueueIndex() {
      return state.playbackQueue.currentIndex ?? -1;
    },
    playbackMode() {
      return state.playbackQueue.mode;
    },
    loop() {
      return state.playbackQueue.loop || 'off';
    },
    playbackModeMessage() {
      if (this.toastType === 'loop') {
        return getLoopLabel(this.loop, this.$t);
      }
      if (this.toastType === 'cleared') {
        return this.$t('player.queueCleared');
      }
      return getModeLabel(this.playbackMode, this.$t);
    },
    toastIcon() {
      if (this.toastType === 'loop') {
        return getLoopIcon(this.loop);
      }
      if (this.toastType === 'cleared') {
        return 'delete';
      }
      return getModeIcon(this.playbackMode);
    },
    hasSubtitles() {
      return this.subtitlesList && this.subtitlesList.length > 0;
    },
    mediaElement() {
      return this.previewType === 'video'
        ? this.$refs.videoElement
        : this.$refs.audioElement;
    },
    shouldAutoplay() {
      return this.autoPlayEnabled || this.playbackQueue.length > 1;
    },
    fileName() {
      return this.req.name ? this.req.name.replace(/\.[^/.]+$/, "") : '';
    },
    videoSwipeGesturesActive() {
      return (
        (this.previewType === 'video' || this.previewType === 'audio') &&
        !!this.player && !this.mobileLyricsActive
      );
    },
    videoNavigationGestureAllowed() {
      return state.navigation.enabled && getters.currentPrompt() === null;
    },
    hasVideoPreviousNav() {
      if (getters.isPreviewPlaybackQueueNavMode()) {
        return this.videoNavigationGestureAllowed && getters.playbackQueueCanGoPrevious();
      }
      return this.videoNavigationGestureAllowed && state.navigation.previousLink !== '';
    },
    hasVideoNextNav() {
      if (getters.isPreviewPlaybackQueueNavMode()) {
        return this.videoNavigationGestureAllowed && getters.playbackQueueCanGoNext();
      }
      return this.videoNavigationGestureAllowed && state.navigation.nextLink !== '';
    },
    isMobile() {
      return getters.isMobile();
    },
    syncedLyrics() {
      return this.lyrics.length > 0 && !this.lyrics.every(line => line.timestamp === 0);
    },
    // "Title - Artist" shown above the lyrics from the [ti:] [ar:] tags on lyrics (if present)
    lyricsMeta() {
      const lyricsTitle = this.lyrics?.lrcMeta?.title;
      const lyricsArtist = this.lyrics?.lrcMeta?.artist;
      if (lyricsTitle && lyricsArtist) return `${lyricsTitle} - ${lyricsArtist}`;
      return lyricsTitle || lyricsArtist || '';
    },
    scrubPreviewEnabled() {
      return (
        this.previewType === 'video'
        && Boolean(this.req?.hasPreview)
        && getters.previewPerms().video
      );
    },
    posterUrl() {
      if (!this.scrubPreviewEnabled) return undefined;
      return getters.isShare()
        ? getPreviewURLPublic(this.req.path, 'original')
        : `${getPreviewURL(this.req.source, this.req.path, this.req.modified)}&size=original`;
    },
    nativeVideoSrc() {
      if (this.previewType !== 'video') {
        return null;
      }
      if (this.lockedNativeVideoSrc) {
        return this.lockedNativeVideoSrc;
      }
      if (this.shouldAttachVideoStream) {
        const handoffTime = this.pipHandoffSeekSeconds
          ?? this.inlineResumeSnapshot?.currentTime;
        return appendMediaFragment(this.raw, handoffTime);
      }
      return null;
    },
    shouldAttachVideoStream() {
      if (
        this.previewType === 'video'
        && shouldDeferVideoStreamAttach(this.req?.source, this.req?.path)
      ) {
        return false;
      }
      return this.videoStreamAttached || this.shouldAutoplay;
    },
    pipHandoffShouldAutoplay() {
      if (this.previewType !== 'video') {
        return false;
      }
      const snap = this.inlineResumeSnapshot;
      if (!snap && !this.pipHandoffSeekSeconds) {
        return false;
      }
      return snap?.wasPlaying !== false;
    },
    videoElementAutoplay() {
      if (this.previewType !== 'video') {
        return false;
      }
      if (shouldDeferVideoStreamAttach(this.req?.source, this.req?.path)) {
        return false;
      }
      if (this.pipHandoffShouldAutoplay) {
        return true;
      }
      if (this.inlineResumeSnapshot || this.pipHandoffSeekSeconds) {
        return false;
      }
      return this.shouldAutoplay;
    },
    /** Rewind / fast-forward in the control bar only on non-mobile (gestures stay as elsewhere). */
    plyrOptions() {
      const controlsDesktop = [
        'play-large',
        'rewind',
        'play',
        'fast-forward',
        'progress',
        'current-time',
        'duration',
        'mute',
        'volume',
        'captions',
        'pip',
        'settings',
        'fullscreen',
      ];
      const controlsMobile = [
        'play-large',
        'play',
        'progress',
        'current-time',
        'duration',
        'mute',
        'volume',
        'captions',
        'pip',
        'settings',
        'fullscreen',
      ];
      return {
        controls: this.isMobile ? controlsMobile : controlsDesktop,
        settings: ['captions', 'captionSize', 'quality', 'speed', 'playback', 'loop'],
        i18n: {
          playback: this.$t('player.playbackMode'),
          captionSize: this.$t('player.captionSize'),
          loop: this.$t('player.loop'),
        },
        speed: {
          selected: 1,
          options: [0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2, 3, 4],
        },
        disableContextMenu: true,
        seekTime: 10,
        hideControls: true,
        keyboard: { focused: true, global: true },
        // Sliders (volume) aren't text fields: with Plyr's default `input` here, every shortcut
        // stopped working once the volume slider had been used, until a click elsewhere.
        selectors: { editable: PLYR_EDITABLE_SELECTOR },
        tooltips: { controls: true, seek: !this.scrubPreviewEnabled },
        loop: { active: false },
        blankVideo: '',
        muted: false,
        autoplay: false,
        playsinline: true,
        clickToPlay: false, // we manage this ourselves with the gestures, plyr has a issue where this doesn't work in mobile.
        resetOnEnd: false,
        preload: this.previewType === 'video' ? 'none' : 'metadata',
        fullscreen: {
          enabled: true,
          fallback: true,
          // iPhone Safari has no element Fullscreen API; only video.webkitEnterFullscreen() hides the browser UI.
          iosNative: true,
          // The whole page, which outlives this viewer, so moving between files stays fullscreen.
          container: 'html',
        },
        iconUrl: `${globalVars.baseURL}public/static/img/plyr.svg`,
        // Blob/async tracks need addtrack → captions.update; otherwise meta never fills and toggle CC throws (track undefined).
        // Do not call toggleCaptions() here — Plyr already applies `plyr` localStorage for captions on/off.
        captions: {
          active: false,
          language: 'auto',
          update: true,
        },
        listeners: {
          seek: blockPlyrSeekOnInput,
        },
      };
    },
  },
  mounted() {
    initPipSession();
    this.hookEvents();
    this.setupTheater();
    if (this.previewType === "audio") {
      this.loadAudioMetadata();
    }
    document.addEventListener('keydown', this.handleKeydown);
    this.pagehideHandler = (event) => {
      if (event.persisted) return;
      this.cleanupAudioVisualizer(); // to stop the visualizer when viewing another browser tab
    };
    window.addEventListener('pagehide', this.pagehideHandler);
  },
  beforeUnmount() {
    this.viewerUnmounted = true;
    this.teardownTheater();
    // Cleanup timeouts
    for (const timeout of [this.toastTimeout,
    this.skipFeedbackTimer,
    this.videoDismissCloseTimer,
    this.videoDismissHintTimer,
    this.skipNextTapTimer,
    this.osdTimer,
    this.volumeBarTimer,
  ]) {
      if (timeout) clearTimeout(timeout);
    }
    // Cleanup Plyr
    this.destroyPlyr();
    this.clearMediaSession();
    document.removeEventListener('keydown', this.handleKeydown);
    window.removeEventListener('pagehide', this.pagehideHandler);
  },
  methods: {
    /** Plyr captions menu: show format only (e.g. `.srt`, `.ass`), not the video basename. */
    subtitleTrackLabel(sub) {
      const ext = getSubtitleFormatExtension(sub?.name || '');
      return ext || sub?.name || '';
    },
    ownsMediaSession() {
      return ownsMediaSessionSlot(this.mediaSessionOwnerId, mediaSessionOwner);
    },
    showQueuePrompt() {
      mutations.showPrompt({
        name: "PlaybackQueue",
      });
    },
    setupMediaSession() {
      if (!('mediaSession' in navigator) || !this.player) return;
      this.mediaSessionOwnerId = ++mediaSessionOwner;
      // Create a fresh fallback URL with timestamp to prevent caching issues
      const fallbackIcon = globalVars.loginIcon;
      const timestamp = Date.now();
      const fallbackUrl = fallbackIcon.includes('?')
        ? `${fallbackIcon}&t=${timestamp}`
        : `${fallbackIcon}?t=${timestamp}`;
      // Video uses the same thumbnail as the poster; audio uses embedded album art (if any).
      const artworkSrc = this.previewType === 'video' ? this.posterUrl : this.albumArtUrl;
      const metadata = {
        title: this.metadata?.title || this.fileName,
        artist: this.metadata?.artist || globalVars.name || "Filebrowser Quantum",
        album: this.metadata?.album || "",
        // In current versions of Firefox the artwork will not work, seems that doesn't like blob URLs.
        // But testing in 149.0a1 (nightly builds), it seems to work, so this something that will solve over time :)
        artwork: [ { src: artworkSrc || fallbackUrl } ]
      };
      navigator.mediaSession.metadata = new MediaMetadata(metadata);
      // Setup handlers for the media session
      const actionHandlers = [
        ['play', () => this.player?.play()],
        ['pause', () => this.player?.pause()],
        ['seekbackward', (details) => this.player?.rewind(details.seekOffset || 10)],
        ['seekforward', (details) => this.player?.forward(details.seekOffset || 10)],
        ['seekto', (details) => {
          if (details.fastSeek && details.fastSeek === 'optional') return;
          this.player.currentTime = details.seekTime;
        }]
      ];
      for (const [action, handler] of actionHandlers) {
        try {
          navigator.mediaSession.setActionHandler(action, handler);
        } catch (e) {
          console.warn(`The media session action "${String(action)}" is not supported`, e);
        }
      }
      this.updateMediaSessionNavHandlers();
      this.updateMediaSessionPlaybackState();
    },
    updateMediaSessionNavHandlers() {
      if (!('mediaSession' in navigator) || !this.ownsMediaSession()) return;
      const { queue, currentIndex, loop } = state.playbackQueue;
      const hasPrevious = queue.length > 1 && !!getNextItem(queue, currentIndex, loop, -1);
      const hasNext = queue.length > 1 && !!getNextItem(queue, currentIndex, loop, 1);
      try {
        navigator.mediaSession.setActionHandler(
          'previoustrack',
          hasPrevious ? () => this.playPrevious() : null
        );
      } catch (_e) { /*ignore*/ }
      try {
        navigator.mediaSession.setActionHandler(
          'nexttrack',
          hasNext ? () => this.playNext() : null
        );
      } catch (_e) { /*ignore*/ }
    },
    updateMediaSessionPlaybackState() {
      if (!('mediaSession' in navigator)) return;
      if (!this.ownsMediaSession()) return;
      if (this.player) {
        navigator.mediaSession.playbackState = this.player.playing ? 'playing' : 'paused';
        // Update position state
        if (navigator.mediaSession.setPositionState) {
          navigator.mediaSession.setPositionState({
            duration: this.player.duration,
            playbackRate: this.player.speed || 1,
            position: this.player.currentTime,
          });
        }
      }
    },
    clearMediaSession() {
      if (!('mediaSession' in navigator)) return;
      if (!this.ownsMediaSession()) return;
      // Clear metadata
      navigator.mediaSession.metadata = null;
      // Clear all action handlers
      const actions = [ 'play', 'pause', 'previoustrack', 'nexttrack', 'seekbackward', 'seekforward', 'seekto', 'stop' ];
      for (const action of actions) {
        navigator.mediaSession.setActionHandler(action, null);
      }
      // Clear position state
      if (navigator.mediaSession.setPositionState) {
        navigator.mediaSession.setPositionState(null);
      }
      // Reset playback state
      navigator.mediaSession.playbackState = 'none';
      this.mediaSessionOwnerId = 0;
    },
    getActiveMediaElement() {
      return this.player?.media ?? this.mediaElement ?? null;
    },
    isMediaInPictureInPicture(media = null) {
      const el = media ?? this.getActiveMediaElement();
      return Boolean(el && document.pictureInPictureElement === el);
    },
    stopIfRouteLeftThisFile() {
      if (this.plyrTeardownDone || !this.req?.path) {
        return;
      }
      const expected = buildItemUrl(this.req.source, this.req.path);
      const current = state.route.path;
      if (current === expected || current.startsWith(`${expected}#`)) {
        return;
      }
      this.destroyPlyr();
    },
    stopMediaElement(media = null) {
      const el = media ?? this.getActiveMediaElement();
      if (!el) {
        return;
      }
      this.clearAttachVideoStreamWait();
      try {
        el.pause();
      } catch (_) {
        // ignore
      }
      if (document.pictureInPictureElement === el) {
        void exitPictureInPictureProgrammatically();
      }
      el.removeAttribute('src');
      try {
        el.load();
      } catch (_) {
        // ignore
      }
    },
    destroyPlyr() {
      if (this.plyrTeardownDone) {
        return;
      }
      this.plyrTeardownDone = true;

      if (this.scrubPreviewCleanup) {
        this.scrubPreviewCleanup();
        this.scrubPreviewCleanup = null;
      }
      if (this.seekOnReleaseCleanup) {
        this.seekOnReleaseCleanup();
        this.seekOnReleaseCleanup = null;
      }
      if (this.videoLoadingCleanup) {
        this.videoLoadingCleanup();
        this.videoLoadingCleanup = null;
      }
      this.videoPlaybackLoading = false;
      this.teardownFrameControls();
      this.teardownQueryPlaybackSeek();
      this.teardownOverlaidHintController();
      this.teardownPipControlGuard();
      this.pipPendingResumeCleanup?.();
      this.pipPendingResumeCleanup = null;
      this.mountedPreviewKey = null;
      this.pipHandoffApplying = false;
      this.lockedNativeVideoSrc = null;

      const media = this.getActiveMediaElement();
      const inPip = this.isMediaInPictureInPicture(media);
      const isTransition = state.navigation.isTransitioning;
      const leavingInPip = inPip && this.req?.path;

      if (this.player) {
        if (isTransition && !inPip) {
          pendingFullscreenResume = this.player.fullscreen?.active || false;
          if (this.previewType === 'video' && media && (!pendingFullscreenResume || pageFullscreen.active)) {
            holdVideoFrame(media);
          }
          pendingControlsHidden = this.previewType === 'video'
            && this.player.playing
            && Boolean(this.player.elements?.container?.classList.contains('plyr--hide-controls'));
          pendingPipResume = false;
          pendingResumeTimestamp = Date.now();
        }
        if (this.nativePlayerPlay) {
          this.player.play = this.nativePlayerPlay;
          this.nativePlayerPlay = null;
        }
        this.teardownVideoSwipeGestures();
        this.teardownDoubleTapSeek();
        this.clearRewindForwardFeedback();
        this.cleanupAudioVisualizer();
        this.clearMediaSession();
        this.cleanupAlbumArt();
        this.player.off();
        if (leavingInPip && media) {
          registerPipSession(media, {
            ...resolvePipMediaKey(this.req.source, this.req.path),
            wasInlineFullscreen: Boolean(this.player?.fullscreen?.active),
            wasPlaying: !media.paused,
          });
          this.player = null;
        } else {
          this.stopMediaElement(media);
          this.player.destroy();
          this.player = null;
        }
      } else if (leavingInPip && media) {
        registerPipSession(media, {
          ...resolvePipMediaKey(this.req.source, this.req.path),
          wasInlineFullscreen: Boolean(this.player?.fullscreen?.active),
          wasPlaying: !media.paused,
        });
      } else if (!inPip) {
        this.stopMediaElement(media);
      }

      this.playbackMenuInitialized = false;
      this.captionSizeMenuInitialized = false;
      this.loopMenuInitialized = false;
      this.lastAppliedMode = null;
      this.playbackButtons = null;
      this.playbackValueSpan = null;
      this.captionSizeButtons = null;
      this.captionSizeValueSpan = null;
      this.loopButtons = null;
      this.loopValueSpan = null;
    },
    cleanupAudioVisualizer() {
      if (this.audioSource) {
        try { this.audioSource.disconnect(); } catch (_) { /* ignore */ }
        this.audioSource = null;
      }
      if (this.audioContext) {
        const context = this.audioContext;
        try {
          const closed = context.close();
          if (closed?.catch) closed.catch(() => {});
        } catch (_) {
          /* ignore */
        }
        this.audioContext = null;
      }
      this.audioGraphInitialized = false;
    },
    togglePlayPause() {
      if (this.player) {
        this.player.togglePlay();
        return;
      }
      const media = this.mediaElement;
      if (!media) return;
      if (media.paused) media.play(); else media.pause();
    },
    handlePlay() {
      this.$emit('play');
      if (this.previewType === 'audio') {
        this.resumeAudioGraph();
      }
    },
    ensurePlaybackModeApplied() {
      if (!this.player) return;
      try {
        const settingsMenu = this.player.elements.settings?.menu;
        const playbackBtn = this.player.elements.settings?.buttons?.playback;
        const captionSizeBtn = this.player.elements.settings?.buttons?.captionSize;
        const loopBtn = this.player.elements.settings?.buttons?.loop;
        const menuOpen =
          settingsMenu
          && settingsMenu.style.display !== 'none'
          && settingsMenu.getAttribute('hidden') === null;
        const needPlayback = playbackBtn && !this.playbackMenuInitialized;
        const needCaptionSize = captionSizeBtn && !this.captionSizeMenuInitialized;
        const needLoop = loopBtn && !this.loopMenuInitialized;

        if (menuOpen || needPlayback || needCaptionSize || needLoop) {
          this.applyCustomSettings(this.player);
        }
      } catch (error) {
        console.error('Error ensuring playback mode applied:', error);
      }
    },
    getStoredCaptionSize() {
      try {
        const raw = localStorage.getItem(PLYR_LOCALSTORAGE_KEY);
        if (!raw) {
          return 'medium';
        }
        const data = JSON.parse(raw);
        const id = getObjectProperty(data, PLYR_CAPTION_SIZE_FIELD);
        if (id && PLYR_CAPTION_SIZE_IDS.includes(id)) {
          return id;
        }
      } catch {
        /* ignore */
      }
      return 'medium';
    },
    setStoredCaptionSize(id) {
      if (!PLYR_CAPTION_SIZE_IDS.includes(id)) {
        return;
      }
      this._mergePlyrStorage({ [PLYR_CAPTION_SIZE_FIELD]: id });
    },
    /** Merge into Plyr’s JSON store without clobbering `captions`, `language`, etc. */
    _mergePlyrStorage(partial) {
      try {
        let data = {};
        const raw = localStorage.getItem(PLYR_LOCALSTORAGE_KEY);
        if (raw) {
          data = JSON.parse(raw);
          if (typeof data !== 'object' || data === null) {
            data = {};
          }
        }
        Object.assign(data, partial);
        localStorage.setItem(PLYR_LOCALSTORAGE_KEY, JSON.stringify(data));
      } catch {
        /* ignore */
      }
    },
    captionSizeMenuLabels() {
      return {
        small: 'Small',
        medium: 'Medium',
        large: 'Large',
        xlarge: 'Extra large',
      };
    },
    applyCaptionSizeClass() {
      if (!this.player?.elements?.container) {
        return;
      }
      const el = this.player.elements.container;
      for (const id of PLYR_CAPTION_SIZE_IDS) {
        el.classList.remove(`plyr-caption-size--${id}`);
      }
      el.classList.add(`plyr-caption-size--${this.getStoredCaptionSize()}`);
    },
    syncCaptionSizeSettingsVisibility() {
      if (!this.player) {
        return;
      }
      const btn = this.player.elements.settings?.buttons?.captionSize;
      if (!btn) {
        return;
      }
      const visible = this.previewType === 'video' && this.hasSubtitles;
      if (visible) {
        btn.removeAttribute('hidden');
      } else {
        btn.setAttribute('hidden', '');
      }
    },
    getCaptionSizeLabel(size) {
      switch (size) {
        case 'small': return 'Small';
        case 'medium': return 'Medium';
        case 'large': return 'Large';
        case 'xlarge': return 'Extra large';
        default: return 'Medium';
      }
    },
    toggleSingleLoop() {
      const newLoop = toggleSingleLoop(this.loop);
      mutations.setPlaybackQueue({
        queue: this.playbackQueue,
        currentIndex: this.currentQueueIndex,
        mode: this.playbackMode,
        loop: newLoop
      });
    },
    handleKeydown(event) {
      if (this.handleMediaKey(event)) return;
      if (event.repeat) return;
      if (event.ctrlKey || event.metaKey || event.altKey) return;
      const key = event.key.toLowerCase();
      if (isTextEntryTarget(event.target)) {
        return;
      }
      // Handle 'P' and 'L' keys for loop and change playback
      if (key === 'p' || key === 'l') {
        event.stopPropagation();
        event.preventDefault();
        if (key === 'p') this.cyclePlaybackModes();
        if (key === 'l') this.toggleSingleLoop();
        return;
      }
      // "Q" key – open/close panel on desktop audio, queue prompt on vids
      if (key === 'q') {
        if (this.previewType === 'audio' && !this.isMobile) {
          event.stopPropagation();
          event.preventDefault();
          this.showDesktopPanel = !this.showDesktopPanel;
        } else if (state.prompts.length === 0) {
          event.stopPropagation();
          event.preventDefault();
          this.showQueuePrompt();
        }
      }
    },
    /** Runs a player control if the key is one; returns true when the key was taken. */
    handleMediaKey(event) {
      if (!this.player || isTextEntryTarget(event.target)) return false;
      if (this.previewType !== 'video' && this.previewType !== 'audio') return false;
      const action = mediaKeyAction(event);
      if (!action) return false;
      if ((action.type === 'frameStep' || action.type === 'theater') && this.previewType !== 'video') return false;
      event.preventDefault();
      event.stopPropagation();
      switch (action.type) {
        case 'seek':
          this.seekBy(action.seconds, event.repeat);
          break;
        case 'volume':
          this.player.increaseVolume(action.delta);
          this.showVolumeBar();
          break;
        case 'speed':
          this.setSpeed(steppedSpeed(this.player.speed, action.delta));
          break;
        case 'speedReset':
          this.setSpeed(1);
          break;
        case 'frameStep':
          this.stepFrame(action.direction);
          break;
        case 'abLoop':
          if (!event.repeat) this.cycleAbLoop();
          break;
        case 'stats':
          if (!event.repeat) this.toggleStats();
          break;
        case 'theater':
          if (!event.repeat) this.toggleTheater();
          break;
        case 'statsBrief':
          if (!event.repeat) this.showStatsBriefly();
          break;
        default:
          break;
      }
      return true;
    },
    seekBy(seconds, held) {
      const duration = this.player.duration;
      if (!(duration > 0)) return;
      this.player.currentTime = Math.min(duration, Math.max(0, this.player.currentTime + seconds));
      this.flashSkipFeedback(seconds < 0, Math.abs(seconds), held);
    },
    setSpeed(speed) {
      this.player.speed = speed;
      this.showOsd(this.$t('player.osdSpeed', { speed }));
    },
    setupFrameTracking() {
      if (this.previewType !== 'video' || !this.player?.media) return;
      this.frameTracker = createFrameTracker(this.player.media, this.onVideoFrame);
      if (!this.frameTracker) this.player.on('playing', releaseVideoFrame);
    },
    teardownFrameControls() {
      this.stopStats();
      this.frameTracker?.stop();
      this.frameTracker = null;
      if (this.frameStepTimer) clearTimeout(this.frameStepTimer);
      this.frameStepTimer = null;
      this.frameStepOrigin = null;
      if (this.abLoopPollTimer) clearInterval(this.abLoopPollTimer);
      this.abLoopPollTimer = null;
    },
    onVideoFrame(mediaTime) {
      releaseVideoFrame();
      if (this.frameStepOrigin !== null && mediaTime !== this.frameStepOrigin) this.finishFrameStep();
      this.checkAbLoop(mediaTime);
    },
    finishFrameStep() {
      if (this.frameStepTimer) clearTimeout(this.frameStepTimer);
      this.frameStepTimer = null;
      this.frameStepOrigin = null;
    },
    stepFrame(direction) {
      // One step at a time: the next starts from the frame this one lands on.
      if (this.frameStepOrigin !== null || !(this.player.duration > 0)) return;
      const video = this.player.media;
      this.player.pause();
      const origin = this.frameTracker?.mediaTime() ?? video.currentTime;
      const frameSeconds = this.frameTracker?.frameSeconds() ?? DEFAULT_FRAME_SECONDS;
      const offset = frameOffset(origin, this.frameStepLastTarget, video.currentTime);
      const target = frameStepTarget(origin, frameSeconds, direction, video.duration, offset);
      if (target === null) return;
      this.frameStepOrigin = origin;
      this.frameStepLastTarget = target;
      // A browser without the frame callback, or a seek that lands on the same frame, must not
      // lock the keys.
      this.frameStepTimer = setTimeout(this.finishFrameStep, this.frameTracker ? FRAME_STEP_TIMEOUT_MS : 150);
      video.currentTime = target;
    },
    cycleAbLoop() {
      if (!(this.player.duration > 0)) return;
      const next = nextAbLoop(this.abLoop, this.player.media.currentTime);
      if (next === this.abLoop) return;
      let message = this.$t('player.osdAbEnd');
      if (next.a === null) message = this.$t('player.osdAbCleared');
      else if (next.b === null) message = this.$t('player.osdAbStart');
      this.abLoop = next;
      this.abLoopArmed = true;
      this.drawAbLoopMarkers();
      this.syncAbLoopPolling();
      this.showOsd(message);
    },
    drawAbLoopMarkers() {
      const progress = this.player?.elements?.container?.querySelector('.plyr__progress');
      if (!progress) return;
      progress.querySelectorAll('.ab-loop-marker').forEach((el) => el.remove());
      for (const percent of abLoopMarkers(this.abLoop, this.player.duration)) {
        const marker = document.createElement('span');
        marker.className = 'ab-loop-marker';
        marker.style.left = `${percent}%`;
        progress.append(marker);
      }
    },
    /** Video jumps from the frame callback. Without one, as on audio, a short timer watches the time. */
    syncAbLoopPolling() {
      const needed = abLoopActive(this.abLoop) && !this.frameTracker;
      if (needed && !this.abLoopPollTimer) {
        this.abLoopPollTimer = setInterval(() => {
          this.checkAbLoop(this.player?.media?.currentTime ?? 0);
        }, AB_LOOP_POLL_MS);
      } else if (!needed && this.abLoopPollTimer) {
        clearInterval(this.abLoopPollTimer);
        this.abLoopPollTimer = null;
      }
    },
    checkAbLoop(time) {
      if (!abLoopActive(this.abLoop) || !this.abLoopArmed) return;
      const media = this.player?.media;
      if (!media || media.paused || media.seeking) return;
      if (time >= this.abLoop.b) media.currentTime = this.abLoop.a;
    },
    onAbLoopSeeking() {
      if (!abLoopActive(this.abLoop) || !this.player?.media) return;
      this.abLoopArmed = this.player.media.currentTime < this.abLoop.b;
    },
    setupTheater() {
      if (this.previewType !== 'video') return;
      claimTheaterLayout();
      this.theaterMode = readTheaterSetting();
      this.theaterHeader = document.querySelector('header.flexbar');
      this.theaterHeader?.addEventListener('mouseleave', this.onTheaterHeaderLeave);
      this.applyTheater();
    },
    teardownTheater() {
      if (this.theaterLeaveTimer) clearTimeout(this.theaterLeaveTimer);
      this.theaterLeaveTimer = null;
      this.theaterHeader?.removeEventListener('mouseleave', this.onTheaterHeaderLeave);
      this.theaterHeader = null;
      releaseTheaterLayout();
    },
    toggleTheater() {
      this.theaterMode = !this.theaterMode;
      writeTheaterSetting(this.theaterMode);
      this.applyTheater();
    },
    applyTheater() {
      applyTheaterLayout(this.theaterMode);
      this.syncTheaterBar();
    },
    /** The top bar follows Plyr's controls, but never disappears from under the mouse. */
    syncTheaterBar() {
      // Before the player exists, the bar stays as the previous file left it.
      if (!this.player?.elements?.container) return;
      const root = document.documentElement;
      const hidden = this.theaterMode && this.player.elements.container.classList.contains('plyr--hide-controls');
      if (hidden && this.theaterHeader?.matches(':hover')) return;
      root.classList.toggle(THEATER_BAR_HIDDEN_CLASS, hidden);
    },
    onTheaterHeaderLeave() {
      if (this.theaterLeaveTimer) clearTimeout(this.theaterLeaveTimer);
      // Moving onto the video brings the controls back; give that a moment to happen first.
      this.theaterLeaveTimer = setTimeout(this.syncTheaterBar, THEATER_BAR_LEAVE_MS);
    },
    toggleStats() {
      if (this.statsVisible) {
        statsPinned = false;
        this.stopStats();
        return;
      }
      statsPinned = true;
      this.startStats();
    },
    showStatsBriefly() {
      if (statsPinned) return;
      this.startStats();
      if (this.statsBriefTimer) clearTimeout(this.statsBriefTimer);
      this.statsBriefTimer = setTimeout(this.stopStats, STATS_BRIEF_MS);
    },
    startStats() {
      this.updateStats();
      this.statsVisible = true;
      if (!this.statsUpdateTimer) this.statsUpdateTimer = setInterval(this.updateStats, STATS_UPDATE_MS);
    },
    stopStats() {
      if (this.statsUpdateTimer) clearInterval(this.statsUpdateTimer);
      if (this.statsBriefTimer) clearTimeout(this.statsBriefTimer);
      this.statsUpdateTimer = null;
      this.statsBriefTimer = null;
      this.statsVisible = false;
    },
    updateStats() {
      if (!this.player?.media) return;
      this.statsRows = buildStatsRows({
        media: this.player.media,
        file: this.req,
        frameRate: this.frameTracker?.measuredFrameRate() ?? null,
        devicePixelRatio: window.devicePixelRatio || 1,
      });
    },
    handleWheelVolume(event) {
      if (!this.player || event.ctrlKey || event.metaKey || event.altKey) return;
      if (event.target.closest?.(WHEEL_SCROLLS_HERE)) return;
      if (event.shiftKey) {
        // Shift+wheel resizes the album art, and nothing else in the player.
        if (!event.target.closest?.('.album-art-container')) event.stopPropagation();
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      const { deltaMode, deltaY } = event;
      const { direction, accumulated } = wheelVolumeStep(this.wheelAccumulated, deltaY, deltaMode);
      this.wheelAccumulated = accumulated;
      if (direction === 0) return;
      this.player.increaseVolume(direction * WHEEL_VOLUME_STEP);
      this.showVolumeBar();
    },
    /** Kept current while hidden, so the first change doesn't animate up from empty. */
    syncVolumeBar() {
      if (!this.player?.media) return;
      this.volumeBarPercent = this.player.muted ? 0 : Math.round(this.player.volume * 100);
    },
    showVolumeBar() {
      this.syncVolumeBar();
      this.volumeBarVisible = true;
      if (this.volumeBarTimer) clearTimeout(this.volumeBarTimer);
      this.volumeBarTimer = setTimeout(() => {
        this.volumeBarVisible = false;
        this.volumeBarTimer = null;
      }, OSD_DURATION_MS);
    },
    showOsd(text) {
      if (this.osdTimer) clearTimeout(this.osdTimer);
      this.osdText = text;
      this.osdTimer = setTimeout(() => {
        this.osdText = '';
        this.osdTimer = null;
      }, OSD_DURATION_MS);
    },
    speedStorageKey() {
      return this.previewType === 'audio' ? AUDIO_SPEED_STORAGE_KEY : VIDEO_SPEED_STORAGE_KEY;
    },
    restoreSpeed() {
      const media = this.player?.media;
      if (!media) return;
      let stored = null;
      try {
        stored = parseStoredSpeed(localStorage.getItem(this.speedStorageKey()));
      } catch {
        /* storage unavailable: stay at normal speed */
      }
      if (stored === null) return;
      // Loading the stream resets playbackRate to defaultPlaybackRate, and the stream may only
      // attach on the first play, so both need the value.
      media.defaultPlaybackRate = stored;
      media.playbackRate = stored;
      if (stored !== 1) {
        this.showOsd(this.$t('player.osdSpeed', { speed: stored }));
      }
    },
    onRateChange() {
      const media = this.player?.media;
      if (!media) return;
      const rate = media.playbackRate;
      this.syncSpeedMenuLabel();
      // The touch long-press is a temporary 2x and restores the previous speed on release.
      if (this.longPressTriggered) return;
      if (media.defaultPlaybackRate !== rate) media.defaultPlaybackRate = rate;
      try {
        localStorage.setItem(this.speedStorageKey(), String(rate));
      } catch {
        /* storage unavailable: the speed just isn't remembered */
      }
    },
    /** Plyr leaves its label stale for a speed its menu doesn't list, such as 1.3×. */
    syncSpeedMenuLabel() {
      const rate = this.player?.media?.playbackRate;
      if (!rate || this.player.options?.speed?.includes(rate)) return;
      const settings = this.player.elements?.settings;
      const label = settings?.buttons?.speed?.querySelector('.plyr__menu__value');
      if (label) label.textContent = `${rate}×`;
      settings?.panels?.speed?.querySelectorAll('[role="menuitemradio"]').forEach((item) => {
        item.setAttribute('aria-checked', 'false');
      });
    },
    cyclePlaybackModes() {
      cyclePlaybackModes(this.playbackMode, {
        listing: this.listing,
        currentItem: this.req,
        isShare: getters.isShare()
      });
    },
    // Seek the player to the given timestamp (in milliseconds)
    seekToLyric(timestampMs) {
      if (!this.player) return;
      this.player.currentTime = timestampMs / 1000;
    },
    // Update active lyric line based on current player time.
    syncLyrics() {
      if (!this.lyrics.length || !this.syncedLyrics) return;
      const currentMs = this.player.currentTime * 1000;
      let idx = this.activeLyricIndex;
      if (idx > this.lyrics.length - 1) {
        idx = -1;
      }
      if (idx >= 0 && this.lyrics.at(idx)?.timestamp > currentMs) {
        idx = -1;
      }
      while (
        idx + 1 < this.lyrics.length &&
        this.lyrics.at(idx + 1).timestamp <= currentMs
      ) {
        idx++;
      }
      let first = idx;
      while (
        first > 0 &&
        this.lyrics.at(first - 1)?.timestamp === this.lyrics.at(idx)?.timestamp
      ) {
        first--;
      }
      if (first !== this.activeLyricIndex) {
        this.activeLyricIndex = first;
        this.activeWordIndex = -1;
      }
      this.syncActiveWord(currentMs);
    },
    // Update active word within the active lyric line (for elrc)
    syncActiveWord(currentMs) {
      const words = this.activeLyricIndex >= 0 ? this.lyrics.at(this.activeLyricIndex)?.words : null;
      if (!words?.length) {
        this.activeWordIndex = -1;
        return;
      }
      let idx = this.activeWordIndex;
      if (idx > words.length - 1) {
        idx = -1;
      }
      if (idx < 0 || words.at(idx)?.timestamp > currentMs) {
        idx = 0;
      }
      while (idx + 1 < words.length && words.at(idx + 1).timestamp <= currentMs) {
        idx++;
      }
      if (words.at(idx)?.timestamp > currentMs) {
        idx = -1;
      }
      if (idx !== this.activeWordIndex) {
        this.activeWordIndex = idx;
      }
    },
    scrollMobileLyrics() {
      if (this.mobileLyricsScrollLocked) return;
      const el = this.$refs.lyricsMobileScrollable;
      if (!el) return;
      const active = el.querySelector('.lyric-line.active');
      if (active) active.scrollIntoView({ behavior: 'smooth', block: 'center' });
    },
    showToast() {
      if (this.toastTimeout) {
        clearTimeout(this.toastTimeout);
      }
      this.toastVisible = true;
      this.toastTimeout = setTimeout(() => {
        this.toastVisible = false;
      }, 1500);
    },
    // Album art hover and scroll handlers
    onAlbumArtHover() {
      this.isHovering = true;
    },
    onAlbumArtLeave() {
      this.isHovering = false;
    },
    onAlbumArtScroll(event) {
      if (!this.isHovering || !event.shiftKey) return;
      event.preventDefault();
      // With Shift held, browsers may report the wheel as horizontal.
      const scrollDelta = (event.deltaY || event.deltaX) > 0 ? -5 : 5; // Scroll down decreases, scroll up increases
      const newSize = this.albumArtSize + scrollDelta;
      // Apply size constraints (minimum 10em, maximum 50em)
      this.albumArtSize = Math.max(10, Math.min(50, newSize));
    },
    // Load metadata from the backend response
    loadAudioMetadata() {
      if (this.previewType !== "audio") return;
      // Check if metadata is already provided by the backend
      if (this.req.metadata) {
        this.metadata = {
          title: this.req.metadata.title || this.fileName, // Fallback to filename
          artist: this.req.metadata.artist || null,
          album: this.req.metadata.album || null,
          year: this.req.metadata.year || null
        };
        // Handle base64 encoded album art
        if (this.req.metadata.albumArt) {
          try {
            const byteCharacters = atob(this.req.metadata.albumArt);
            const byteArray = Uint8Array.from(byteCharacters, (c) => c.charCodeAt(0));
            const blob = new Blob([byteArray], { type: 'image/jpeg' });
            this.albumArtUrl = URL.createObjectURL(blob);
          } catch (error) {
            console.error("Failed to decode album art:", error);
            this.albumArtUrl = null;
          }
        }
      } else {
        this.metadata = {
          title: this.fileName,
          artist: null,
          album: null,
          year: null,
        };
      }
    },
    cleanupAlbumArt() {
      if (this.previewType !== "audio") return;
      if (typeof this.albumArtUrl === 'string' && this.albumArtUrl.startsWith('blob:')) {
        URL.revokeObjectURL(this.albumArtUrl);
      }
      this.albumArtUrl = null;
      this.metadata = null;
    },
    hookEvents() {
      // For videos with subtitle metadata, wait for subtitles to load before initializing Plyr
      // This prevents Plyr from trying to access tracks before they have valid blob URLs
      const hasSubtitleMetadata = this.req?.subtitles?.length > 0;
      const subtitlesNotLoaded = !this.subtitlesList || this.subtitlesList.length === 0;

      if (this.previewType === 'video' && hasSubtitleMetadata && subtitlesNotLoaded) {
        // Wait for subtitles to be loaded (watcher will call initializePlyr)
        return;
      }

      void this.initializePlyr();
    },
    async initializePlyr() {
      if (this.viewerUnmounted || !this.mediaElement || this.player) {
        return;
      }
      await this.$nextTick();
      if (this.viewerUnmounted || this.player) {
        return;
      }
      void this.mountPlyrPlayer();
    },
    async mountPlyrPlayer() {
      if (this.viewerUnmounted || !this.mediaElement || this.player) {
        return;
      }
      this.plyrTeardownDone = false;
      this.mountedPreviewKey = resolvePipMediaKey(this.req?.source, this.req?.path);
      this.pipHandoffApplying = false;
      await this.reconcilePipSessionOnMount();
      if (this.viewerUnmounted || this.plyrTeardownDone || !this.mediaElement) return;
      this.player = new Plyr(this.mediaElement, this.plyrOptions);
      if (this.previewType === 'video') settleVideoFrame(VIDEO_SWITCH_SETTLE_MS);
      if (this.previewType === 'video' && !this.shouldAttachVideoStream) {
        this.nativePlayerPlay = this.player.play.bind(this.player);
        this.player.play = () => {
          if (!this.videoStreamAttached) {
            void this.attachVideoStreamAndPlay();
            return Promise.resolve();
          }
          return this.nativePlayerPlay();
        };
      }
      this.setupMediaSession();
      this.setupPlyrEvents();
      // Already fullscreen from the previous file: no change event will tell this player.
      if (this.previewType === 'video' && pageFullscreen.active) this.player.fullscreen?.onChange();
      if (pendingControlsHidden) {
        pendingControlsHidden = false;
        if (this.previewType === 'video') {
          this.player.elements?.container?.classList.add('plyr--hide-controls');
          // The line above arms a timer that shows the controls while this player is still paused.
          clearTimeout(this.player.timers?.controls);
        }
      }
      this.restoreSpeed();
      if (this.previewType === 'video') {
        this.setupPipControlGuard();
        this.pipPendingResumeCleanup = onPendingInlineResume((snapshot) => {
          if (!this.mountedPreviewMatchesPending(snapshot)) {
            return;
          }
          this.resumePlaybackAfterNavigation();
        });
      }
      this.seekOnReleaseCleanup = enablePlyrSeekOnRelease(this.player);
      this.setupScrubPreview();
      this.setupVideoLoadingIndicator();
      this.setupFrameTracking();
      if (statsPinned) this.startStats();
      this.resumePlaybackAfterNavigation();
      if (this.previewType === 'video') {
        if (!this.inlineResumeSnapshot && !this.pipHandoffSeekSeconds) {
          this.setupQueryPlaybackSeek();
        }
      } else if (this.previewType === 'audio') {
        this.setupQueryPlaybackSeek();
      }
    },
    applyInlineResumeSeek(el, snapshot, onDone) {
      if (!el || !snapshot) {
        onDone?.();
        return;
      }
      const targetTime = Number.isFinite(snapshot.currentTime) ? snapshot.currentTime : 0;
      let finished = false;
      let handoffCompleted = false;
      const completeHandoff = () => {
        if (handoffCompleted) {
          return;
        }
        handoffCompleted = true;
        if (snapshot.wasFullscreen) {
          this.player?.fullscreen?.enter();
        }
        if (snapshot.wasPlaying !== false) {
          this.attemptPipHandoffPlay(el, snapshot);
        }
        const boundSrc = el.currentSrc || el.getAttribute('src');
        if (boundSrc) {
          this.lockedNativeVideoSrc = boundSrc;
        }
        this.inlineResumeSnapshot = null;
        this.pipHandoffSeekSeconds = null;
        this.pipHandoffApplying = false;
        clearPendingInlineResumeFor(snapshot.source, snapshot.path);
        onDone?.();
      };
      const finish = () => {
        if (finished) {
          return;
        }
        finished = true;
        if (snapshot.wasPlaying !== false && el.readyState < HTMLMediaElement.HAVE_FUTURE_DATA) {
          el.addEventListener('canplay', () => completeHandoff(), { once: true });
          window.setTimeout(() => completeHandoff(), 8000);
        } else {
          completeHandoff();
        }
      };
      const readTime = () => this.player?.currentTime ?? el.currentTime;
      const writeTime = (time) => {
        if (this.player) {
          this.player.currentTime = time;
        } else {
          el.currentTime = time;
        }
      };
      const nearTarget = () => targetTime <= 0 || Math.abs(readTime() - targetTime) <= 1.5;
      const seek = () => {
        if (targetTime <= 0) {
          finish();
          return;
        }
        if (nearTarget()) {
          finish();
          return;
        }
        try {
          writeTime(targetTime);
        } catch (_) {
          finish();
          return;
        }
        const onSeeked = () => {
          if (nearTarget()) {
            finish();
            return;
          }
          el.addEventListener('canplay', () => {
            try {
              writeTime(targetTime);
            } catch (_) {
              finish();
              return;
            }
            el.addEventListener('seeked', () => {
              if (nearTarget()) {
                finish();
              }
            }, { once: true });
          }, { once: true });
        };
        el.addEventListener('seeked', onSeeked, { once: true });
        window.setTimeout(() => {
          if (!finished && nearTarget()) {
            finish();
          }
        }, 5000);
      };
      const whenSeekable = () => {
        if (nearTarget()) {
          finish();
          return;
        }
        if (el.readyState >= HTMLMediaElement.HAVE_FUTURE_DATA) {
          seek();
          return;
        }
        el.addEventListener('canplay', seek, { once: true });
      };
      if (el.readyState >= HTMLMediaElement.HAVE_METADATA) {
        whenSeekable();
      } else {
        el.addEventListener('loadedmetadata', whenSeekable, { once: true });
      }
    },
    async attachVideoStreamAndPlay() {
      if (this.videoStreamAttached || this.previewType !== 'video' || !this.raw) {
        if (this.inlineResumeSnapshot) {
          this.pipHandoffApplying = false;
        }
        return;
      }
      this.videoStreamAttached = true;
      this.videoLoadingCleanup?.expectPlayback?.();
      await this.$nextTick();
      await this.$nextTick();
      const el = this.mediaElement;
      if (!el) {
        return;
      }
      const snap = this.inlineResumeSnapshot;
      const handoffTime = snap?.currentTime ?? this.pipHandoffSeekSeconds;
      const streamUrl = appendMediaFragment(this.raw, handoffTime);
      if (streamUrl && !el.getAttribute('src')) {
        el.setAttribute('src', streamUrl);
        this.lockedNativeVideoSrc = streamUrl;
        if (snap?.wasPlaying !== false) {
          el.autoplay = true;
        }
      }
      if (snap?.wasPlaying !== false) {
        const onCanPlay = () => this.attemptPipHandoffPlay(el, snap);
        if (el.readyState >= HTMLMediaElement.HAVE_FUTURE_DATA) {
          onCanPlay();
        } else {
          el.addEventListener('canplay', onCanPlay, { once: true });
        }
      }

      const startDefault = () => {
        this.clearAttachVideoStreamWait();
        this.applyQueryPlaybackSeek();
        void el.play().catch(() => {});
      };

      const startWithResume = () => {
        this.clearAttachVideoStreamWait();
        const onMeta = () => {
          this.applyInlineResumeSeek(el, snap);
        };
        if (el.readyState >= HTMLMediaElement.HAVE_METADATA) {
          onMeta();
        } else {
          el.addEventListener('loadedmetadata', onMeta, { once: true });
        }
      };

      const start = snap ? startWithResume : startDefault;
      this.attachVideoStreamResume = start;
      if (snap) {
        if (el.readyState >= HTMLMediaElement.HAVE_METADATA) {
          start();
        } else {
          el.addEventListener('loadedmetadata', start, { once: true });
        }
      } else {
        start();
      }
    },
    clearAttachVideoStreamWait() {
      const el = this.mediaElement;
      if (el && this.attachVideoStreamResume) {
        el.removeEventListener('canplay', this.attachVideoStreamResume);
        el.removeEventListener('loadedmetadata', this.attachVideoStreamResume);
      }
      this.attachVideoStreamResume = null;
    },
    async reconcilePipSessionOnMount() {
      if (!this.req?.path) {
        return;
      }
      if (hasActivePipSession(this.req.source, this.req.path)) {
        const snap = await takeSessionSnapshot(this.req.source, this.req.path);
        if (snap) {
          setPendingInlineResume({
            source: snap.source,
            path: snap.path,
            currentTime: snap.currentTime,
            wasPlaying: snap.wasPlaying,
            wasFullscreen: Boolean(snap.wasFullscreen),
            timestamp: Date.now(),
          });
        }
      }
    },
    mountedPreviewMatchesPending(snapshot = getPendingInlineResume()) {
      if (!this.mountedPreviewKey || !snapshot) {
        return false;
      }
      return pendingMatchesPreview(this.mountedPreviewKey.source, this.mountedPreviewKey.path, snapshot);
    },
    isHandoffTargetPreview() {
      if (!this.req?.path || !this.mountedPreviewKey) {
        return false;
      }
      const current = resolvePipMediaKey(this.req.source, this.req.path);
      return current.source === this.mountedPreviewKey.source
        && current.path === this.mountedPreviewKey.path;
    },
    resumePlaybackAfterNavigation() {
      const pending = getPendingInlineResume();
      const inlineSnap = this.mountedPreviewMatchesPending(pending) ? pending : null;
      if (inlineSnap) {
        if (this.pipHandoffApplying) {
          return;
        }
        const isStale = Date.now() - inlineSnap.timestamp > 30000;
        if (!isStale) {
          this.applyInlineResume(inlineSnap);
        } else {
          clearPendingInlineResumeFor(this.req.source, this.req.path);
        }
        return;
      }

      const isStale = Date.now() - pendingResumeTimestamp > 3000;
      if (pendingFullscreenResume) {
        pendingFullscreenResume = false;
        if (!isStale && !this.player.fullscreen?.active) this.player.fullscreen?.enter();
      }
      if (pendingPipResume) {
        pendingPipResume = false;
        if (!isStale) {
          void this.requestPictureInPictureWhenReady();
        }
      }
    },
    waitForMediaReady(el, minReadyState = HTMLMediaElement.HAVE_METADATA) {
      if (!el) {
        return Promise.resolve();
      }
      if (el.readyState >= minReadyState) {
        return Promise.resolve();
      }
      const event = minReadyState >= HTMLMediaElement.HAVE_FUTURE_DATA ? 'canplay' : 'loadedmetadata';
      return new Promise((resolve) => {
        el.addEventListener(event, () => resolve(), { once: true });
      });
    },
    async requestPictureInPictureWhenReady() {
      if (this.isPipControlBlocked()) {
        return;
      }
      const el = this.mediaElement;
      if (!el?.requestPictureInPicture) {
        return;
      }
      if (!this.videoStreamAttached && this.previewType === 'video') {
        void this.attachVideoStreamAndPlay();
      }
      await this.waitForMediaReady(el, HTMLMediaElement.HAVE_METADATA);
      if (document.pictureInPictureElement === el) {
        return;
      }
      try {
        await el.requestPictureInPicture();
      } catch (_) {
        // ignore
      }
    },
    isPipControlBlocked() {
      return isPipBlockedForPreview(this.req?.source, this.req?.path, this.mediaElement);
    },
    syncPipControlAvailability() {
      const pipBtn = this.player?.elements?.buttons?.pip;
      if (!pipBtn || this.previewType !== 'video') {
        return;
      }
      const blocked = this.isPipControlBlocked();
      pipBtn.disabled = blocked;
      pipBtn.classList.toggle('plyr__control--disabled', blocked);
      pipBtn.setAttribute('aria-disabled', blocked ? 'true' : 'false');
    },
    setupPipControlGuard() {
      this.teardownPipControlGuard();
      const pipBtn = this.player?.elements?.buttons?.pip;
      if (!pipBtn) {
        return;
      }
      this.syncPipControlAvailability();
      this.pipAvailabilityCleanup = onPipAvailabilityChange(() => {
        this.syncPipControlAvailability();
      });
      this.pipReadinessHandler = (event) => {
        if (this.isPipControlBlocked()) {
          event.preventDefault();
          event.stopImmediatePropagation();
          return;
        }
        const el = this.mediaElement;
        if (!el || el.readyState >= HTMLMediaElement.HAVE_METADATA) {
          return;
        }
        event.preventDefault();
        event.stopImmediatePropagation();
        void this.requestPictureInPictureWhenReady();
      };
      pipBtn.addEventListener('click', this.pipReadinessHandler, true);
    },
    teardownPipControlGuard() {
      const pipBtn = this.player?.elements?.buttons?.pip;
      if (pipBtn && this.pipReadinessHandler) {
        pipBtn.removeEventListener('click', this.pipReadinessHandler, true);
      }
      this.pipReadinessHandler = null;
      this.pipAvailabilityCleanup?.();
      this.pipAvailabilityCleanup = null;
      if (pipBtn) {
        pipBtn.disabled = false;
        pipBtn.classList.remove('plyr__control--disabled');
        pipBtn.setAttribute('aria-disabled', 'false');
      }
    },
    attemptPipHandoffPlay(el, snapshot) {
      if (!el || snapshot?.wasPlaying === false) {
        return;
      }
      const tryPlay = () => {
        const playCall = this.nativePlayerPlay
          ?? (() => this.player?.play?.() ?? el.play());
        void Promise.resolve(playCall()).then(() => {
          mutations.setPlaybackState(true);
          this.$emit('play');
        }).catch(() => {});
      };
      if (el.readyState >= HTMLMediaElement.HAVE_FUTURE_DATA && !el.paused) {
        return;
      }
      if (el.readyState >= HTMLMediaElement.HAVE_FUTURE_DATA) {
        tryPlay();
        return;
      }
      el.addEventListener('canplay', tryPlay, { once: true });
    },
    applyInlineResume(snapshot) {
      const el = this.mediaElement;
      if (!el || !this.player) {
        return;
      }
      if (!this.mountedPreviewMatchesPending(snapshot)) {
        return;
      }
      if (!pendingMatchesPreview(this.mountedPreviewKey.source, this.mountedPreviewKey.path, snapshot)) {
        return;
      }
      if (this.pipHandoffApplying) {
        return;
      }
      this.pipHandoffApplying = true;
      if (this.previewType === 'video') {
        if (this.videoStreamAttached) {
          this.stopMediaElement(el);
          this.videoStreamAttached = false;
        }
        const seekSeconds = Number.isFinite(snapshot.currentTime) ? snapshot.currentTime : 0;
        this.pipHandoffSeekSeconds = seekSeconds > 0 ? seekSeconds : null;
        this.inlineResumeSnapshot = snapshot;
        void this.attachVideoStreamAndPlay();
        return;
      }
      this.applyInlineResumeSeek(el, snapshot);
    },
    getPlaybackDuration() {
      const fromPlayer = this.player?.duration;
      if (Number.isFinite(fromPlayer) && fromPlayer > 0) {
        return fromPlayer;
      }
      const meta = this.req?.metadata?.duration;
      if (Number.isFinite(meta) && meta > 0) {
        return meta;
      }
      return 0;
    },
    setupVideoLoadingIndicator() {
      if (this.previewType !== 'video' || !this.player) {
        return;
      }
      this.videoLoadingCleanup?.();
      this.videoLoadingCleanup = enablePlyrVideoLoadingIndicator(
        this.player,
        (loading) => {
          this.videoPlaybackLoading = loading;
        }
      );
    },
    teardownQueryPlaybackSeek() {
      const media = this.mediaElement;
      if (media && this.querySeekMetadataHandler) {
        media.removeEventListener('loadedmetadata', this.querySeekMetadataHandler);
      }
      this.querySeekMetadataHandler = null;
    },
    getQueryPlaybackSeekSeconds() {
      return parsePlaybackTimeFromQuery(this.$route?.query);
    },
    applyQueryPlaybackSeek() {
      if (this.previewType !== 'video' && this.previewType !== 'audio') {
        return;
      }
      const seconds = this.getQueryPlaybackSeekSeconds();
      if (seconds === null || seconds < 0) {
        return;
      }

      const media = this.mediaElement;
      if (!media) {
        return;
      }

      const duration = this.player?.duration
        ?? media.duration
        ?? this.req?.metadata?.duration
        ?? 0;
      const target = duration > 0 ? Math.min(seconds, duration) : seconds;

      const apply = () => {
        if (this.player) {
          this.player.currentTime = target;
        } else if (media) {
          media.currentTime = target;
        }
      };

      if (media.readyState >= HTMLMediaElement.HAVE_METADATA) {
        apply();
        return;
      }

      this.teardownQueryPlaybackSeek();
      this.querySeekMetadataHandler = apply;
      media.addEventListener('loadedmetadata', this.querySeekMetadataHandler, { once: true });
    },
    setupQueryPlaybackSeek() {
      this.teardownQueryPlaybackSeek();
      this.applyQueryPlaybackSeek();
    },
    setupScrubPreview() {
      if (!this.scrubPreviewEnabled || !this.player) {
        return;
      }
      this.scrubPreviewCleanup?.();
      this.scrubPreviewCleanup = enablePlyrScrubPreview(this.player, {
        buildPreviewUrl: (atPercentage) => {
          const base = getters.isShare()
            ? getPreviewURLPublic(this.req.path, 'large')
            : `${getPreviewURL(this.req.source, this.req.path, this.req.modified)}&size=large`;
          return `${base}&atPercentage=${atPercentage}`;
        },
        getDuration: () => this.getPlaybackDuration(),
      });
    },
    setupAudioVisualizer() {
      // This method gets called once only when in the visualizer tab of the audio panel
      if (this.audioGraphInitialized) return;
      if (this.previewType !== 'audio') return;
      if (this.audioContext) {
        this.cleanupAudioVisualizer();
      }
      const audio = this.mediaElement;
      if (!audio) return;
      try {
        const AudioContextCtor = window.AudioContext || window.webkitAudioContext;
        if (!AudioContextCtor) return;
        const ctx = new AudioContextCtor({ latencyHint: 'playback' });
        const source = ctx.createMediaElementSource(audio);
        // Connect source to destination
        source.connect(ctx.destination);
        this.audioContext = ctx;
        this.audioSource = source;
        this.audioGraphInitialized = true;
      } catch (err) {
        console.warn('Audio visualizer creation failed:', err);
      }
    },
    resumeAudioGraph() {
      const context = this.audioContext;
      if (context?.state !== 'suspended') return;
      try {
        const resumed = context.resume();
        if (resumed?.catch) resumed.catch(err => console.warn('Audio graph resume failed:', err));
      } catch (err) {
        console.warn('Audio visualizer resume failed:', err);
      }
    },
    setupPlyrEvents() {
      if (!this.player) return;
      mutations.setPlaybackState(this.player.playing);
      const eventMap = {
        ended: this.handleMediaEnd,
        play: () => {
          this.hasStartedPlayback = true;
          mutations.setPlaybackState(true);
          this.updateMediaSessionPlaybackState();
        },
        pause: () => {
          mutations.setPlaybackState(false);
          this.updateMediaSessionPlaybackState();
        },
        timeupdate: () => {
          this.updateMediaSessionPlaybackState();
          this.syncLyrics();
        },
        seeked: this.updateMediaSessionPlaybackState,
        loadedmetadata: this.updateMediaSessionPlaybackState,
        ratechange: this.updateMediaSessionPlaybackState,
        canplay: this.updateMediaSessionPlaybackState,
      };
      for (const [evt, fn] of Object.entries(eventMap)) {
        this.player.on(evt, fn);
      }
      if ((this.previewType === 'video' || this.previewType === 'audio')) {
        this.player.on('enterfullscreen', this.onFullscreenEnter);
        this.player.on('exitfullscreen', this.onFullscreenExit);
        this.player.on('ratechange', this.onRateChange);
        this.player.on('seeking', this.onAbLoopSeeking);
        this.player.on('volumechange', this.syncVolumeBar);
        this.player.on('ready', this.syncVolumeBar);
        this.syncVolumeBar();
        this.player.on('ready', this.syncSpeedMenuLabel);
      }
      if (this.previewType === 'video') {
        this.player.on('controlshidden', this.syncTheaterBar);
        this.player.on('controlsshown', this.syncTheaterBar);
        this.player.on('enterfullscreen', this.applyCaptionSizeClass);
        this.player.on('exitfullscreen', this.applyCaptionSizeClass);
      }
      this.ensurePlaybackModeApplied();
      if (this.previewType === 'video') {
        this.applyCaptionSizeClass();
        this.syncCaptionSizeSettingsVisibility();
      }
      if (this.previewType === 'video' || this.previewType === 'audio') {
        this.setupDoubleTapSeek();
        this.setupVideoSwipeGestures();
        this.setupRewindForwardFeedback();
      }
      if (this.previewType === 'video') {
        this.setupOverlaidHintController();
      }
    },
    getPlyrGestureSurface() {
      if (
        this.previewType === 'audio' &&
        this.player &&
        this.$refs.audioPlayerGestureRoot
      ) {
        return this.$refs.audioPlayerGestureRoot;
      }
      if (!this.player?.elements) {
        return null;
      }
      if (this.player.elements.wrapper) {
        return this.player.elements.wrapper;
      }
      return this.player.elements.container ?? null;
    },
    isPlyrControlOrMenuTarget(el) {
      if (!el || typeof el.closest !== 'function') return false;
      if (el.closest('.plyr__control--overlaid')) {
        return true;
      }
      return !!el.closest(
        '.plyr__controls, .plyr__control, .plyr__menu__container, .plyr__menu, ' +
        '[data-plyr="seek"], .plyr__progress, [data-plyr="volume"], .plyr__volume, ' +
        '.audio-side-panel .tab-btn, ' +
        '.audio-side-panel .fab-button, ' +
        '.audio-side-panel .lyric-line, ' +
        '.audio-side-panel input[type="radio"], ' +
        '.audio-side-panel label[for^="tab-"], ' +
        '.audio-side-panel .mode-btn, ' +
        '.audio-side-panel .repeat-one-btn, ' +
        '.audio-side-panel .queue-item, ' +
        '.audio-side-panel .clear-queue-btn, ' +
        '.lyrics-mobile .lyric-line'
      );
    },
    teardownDoubleTapSeek() {
      if (typeof this.doubleTapSeekCleanup === 'function') {
        this.doubleTapSeekCleanup();
        this.doubleTapSeekCleanup = null;
      }
    },
    setupRewindForwardFeedback() {
      this.clearRewindForwardFeedback();
      const container = this.player?.elements?.container;
      if (!container) {
        return;
      }
      const rewind = container.querySelector('[data-plyr="rewind"]');
      const forward = container.querySelector('[data-plyr="fast-forward"]');
      if (!rewind && !forward) {
        return;
      }
      const onRewind = () => this.flashSkipFeedback(true);
      const onForward = () => this.flashSkipFeedback(false);
      rewind?.addEventListener('click', onRewind);
      forward?.addEventListener('click', onForward);
      this.rewindForwardFeedback = () => {
        rewind?.removeEventListener('click', onRewind);
        forward?.removeEventListener('click', onForward);
      };
    },
    clearRewindForwardFeedback() {
      if (typeof this.rewindForwardFeedback === 'function') {
        this.rewindForwardFeedback();
      }
      this.rewindForwardFeedback = null;
    },
    setupOverlaidHintController() {
      this.teardownOverlaidHintController();
      if (!this.player) {
        return;
      }
      const api = enableOverlaidHintController({
        player: this.player,
        hasStartedPlayback: () => this.hasStartedPlayback,
        baseUrl: globalVars.baseURL,
        isPlaying: this.isPlaying,
      });
      this.overlaidHintApi = api;
      this.overlaidHintCleanup = api.cleanup;
    },
    teardownOverlaidHintController() {
      if (typeof this.overlaidHintCleanup === 'function') {
        this.overlaidHintCleanup();
      }
      this.overlaidHintCleanup = null;
      this.overlaidHintApi = null;
    },
    clearEdgeTapGestureState() {
      this.edgeTapLastTime = 0;
      this.edgeTapLastZone = null;
    },
    setupDoubleTapSeek() {
      if ((this.previewType !== 'video' && this.previewType !== 'audio') || !this.player) {
        return;
      }
      this.teardownDoubleTapSeek();
      const surface = this.getPlyrGestureSurface();
      if (!surface || !this.player) return;

      const EDGE_CLICK_MS = 200;

      const peekNavChromeForEdgeTap = (clientX, zone) => {
        if (!state.navigation.enabled) {
          return;
        }
        const moveWithSidebar = getters.isSidebarVisible() && getters.isStickySidebar();
        const sidebarWidthEm = state.sidebar?.width || 20;
        const opts = { moveWithSidebar, sidebarWidthEm };
        if (zone === 'left' && isInLeftNavTapZone(clientX, opts)) {
          mutations.peekNavigationChrome('left');
        } else if (zone === 'right' && isInRightNavTapZone(clientX)) {
          mutations.peekNavigationChrome('right');
        }
      };

      const zoneFromSurfaceX = (clientX) => {
        const rect = surface.getBoundingClientRect();
        return zoneFromClientX(clientX, rect);
      };
      const OVERLAID_BUTTON_TOUCH_PADDING = 8;
      const isOverlaidButtonHit = (clientX, clientY) => {
        const btn = this.player?.elements?.container?.querySelector('.plyr__control--overlaid');
        if (!btn) return false;
        const r = btn.getBoundingClientRect();
        if (!r.width || !r.height) return false;
        const cx = r.left + (r.width / 2);
        const cy = r.top + (r.height / 2);
        const radius = (Math.min(r.width, r.height) / 2) + OVERLAID_BUTTON_TOUCH_PADDING;
        const dx = clientX - cx;
        const dy = clientY - cy;
        return (dx * dx) + (dy * dy) <= radius * radius;
      };

      const applySeek = (rewind) => {
        this.clearEdgeTapGestureState();
        this.clearLongPressTimer();
        this.longPressPending = false;

        const step = this.player.config.seekTime || 10;
        const cur = this.player.currentTime;
        const dur = this.player.duration;
        const next = rewind ? cur - step : cur + step;
        const max = Number.isFinite(dur) && dur > 0 ? dur : next;
        this.player.currentTime = Math.max(0, Math.min(next, max));
        this.flashSkipFeedback(rewind);
      };

      const togglePlayPause = () => {
        if (this.player.playing) {
          this.player.pause();
        } else {
          this.player.play();
        }
      };
      const handleEdgeZoneTap = (zone, event) => {
        const now = Date.now();
        if (zone === this.edgeTapLastZone && now - this.edgeTapLastTime < 300) {
          applySeek(zone === 'left');
          if (event) {
            event.preventDefault();
            event.stopPropagation();
          }
          return;
        }
        this.edgeTapLastTime = now;
        this.edgeTapLastZone = zone;
        if (event) {
          event.preventDefault();
          event.stopPropagation();
        }
      };

      const handleCenterTap = (event) => {
        this.clearEdgeTapGestureState();
        if (this.previewType === 'video') {
          togglePlayPause();
        }
        if (event) {
          event.preventDefault();
          event.stopPropagation();
        }
      };

      const onTouchEnd = (event) => {
        if (this.longPressTriggered) {
          return;
        }
        if (this.skipNextTap) return;

        if (event.changedTouches.length !== 1) return;
        const t = event.changedTouches[0];
        const topEl = typeof document.elementFromPoint === 'function'
          ? document.elementFromPoint(t.clientX, t.clientY)
          : null;
        if (this.isPlyrControlOrMenuTarget(topEl)) {
          this.clearEdgeTapGestureState();
          return;
        }
        if (isOverlaidButtonHit(t.clientX, t.clientY)) {
          handleCenterTap(event);
          this.ignoreClickUntil = Date.now() + 500;
          return;
        }
        const zone = zoneFromSurfaceX(t.clientX);
        if (zone === 'left' || zone === 'right') {
          handleEdgeZoneTap(zone, event);
          peekNavChromeForEdgeTap(t.clientX, zone);
          this.ignoreClickUntil = Date.now() + 500;
          return;
        }
        this.clearEdgeTapGestureState();
        this.ignoreClickUntil = Date.now() + 500;
      };
      let edgeClickToggleTimer = null;
      const clearEdgeClickToggleTimer = () => {
        if (edgeClickToggleTimer) {
          clearTimeout(edgeClickToggleTimer);
          edgeClickToggleTimer = null;
        }
      };
      const onClick = (event) => {
        if (Date.now() < this.ignoreClickUntil) {
          event.preventDefault();
          event.stopPropagation();
          return;
        }
        if (this.isPlyrControlOrMenuTarget(event.target)) {
          return;
        }
        if (this.skipNextTap) return;
        this.clearEdgeTapGestureState();
        const zone = zoneFromSurfaceX(event.clientX);
        if (zone === 'left' || zone === 'right') {
          if (event.detail >= 2) {
            clearEdgeClickToggleTimer();
          } else {
            clearEdgeClickToggleTimer();
            edgeClickToggleTimer = setTimeout(() => {
              edgeClickToggleTimer = null;
              if (this.previewType === 'video') {
                togglePlayPause();
              }
            }, EDGE_CLICK_MS);
          }
          peekNavChromeForEdgeTap(event.clientX, zone);
        } else if (this.previewType === 'video') {
          togglePlayPause();
        }

        event.preventDefault();
        event.stopPropagation();
      };

      const onDblClick = (event) => {
        if (Date.now() < this.ignoreClickUntil) {
          return;
        }
        if (this.isPlyrControlOrMenuTarget(event.target)) {
          return;
        }
        const zone = zoneFromSurfaceX(event.clientX);
        if (zone === 'left' || zone === 'right') {
          clearEdgeClickToggleTimer();
          applySeek(zone === 'left');
          event.preventDefault();
          event.stopPropagation();
        }
      };

      surface.addEventListener('touchend', onTouchEnd, { passive: false });
      surface.addEventListener('click', onClick);
      surface.addEventListener('dblclick', onDblClick);

      this.doubleTapSeekCleanup = () => {
        surface.removeEventListener('touchend', onTouchEnd);
        surface.removeEventListener('click', onClick);
        surface.removeEventListener('dblclick', onDblClick);
        clearEdgeClickToggleTimer();
        this.clearEdgeTapGestureState();
      };
    },
    flashSkipFeedback(rewind, seconds = 10, held = false) {
      if (this.skipFeedbackTimer) {
        clearTimeout(this.skipFeedbackTimer);
      }
      this.skipFeedbackSide = rewind ? 'left' : 'right';
      this.skipFeedbackPath = seekIconPath(rewind ? -seconds : seconds);
      this.skipFeedbackHeld = Boolean(held);
      this.skipFeedbackKey += 1;
      this.skipFeedbackVisible = true;
      this.skipFeedbackTimer = setTimeout(() => {
        this.skipFeedbackVisible = false;
        this.skipFeedbackTimer = null;
      }, 700);
    },
    applyVideoSwipeTransform() {
      const el = this.getPlyrGestureSurface();
      if (!el) {
        return;
      }
      const transition = this.videoGestureSnapBack
        ? 'transform 0.22s cubic-bezier(0.32, 0.72, 0, 1)'
        : 'none';
      el.style.transition = transition;
      const x = this.videoDragOffsetX;
      const y = this.videoDragOffsetY;
      el.style.transform = x || y ? `translate(${x}px, ${y}px)` : '';
    },
    syncVideoNavigationGestureHintToStore() {
      const ax = Math.abs(this.videoEdgeDx);
      const ay = Math.abs(this.videoEdgeDy);
      const navPrevReady =
        this.hasVideoPreviousNav &&
        this.videoEdgeDx >= this.videoEdgeCommitX &&
        ax >= ay;
      const navNextReady =
        this.hasVideoNextNav &&
        this.videoEdgeDx <= -this.videoEdgeCommitX &&
        ax >= ay;
      const dismissReady =
        this.videoEdgeDy >= this.videoEdgeCommitY && ay >= ax;
      let kind = null;
      let commitReady = false;
      let flashClose = false;
      if (this.videoDismissFlashActive) {
        kind = 'close';
        commitReady = dismissReady;
        flashClose = true;
      } else if (this.videoShowDismissHint) {
        kind = 'close';
        commitReady = dismissReady;
      } else if (this.videoShowNavHint && this.videoNavHintDir === 'prev' && this.hasVideoPreviousNav) {
        kind = 'previous';
        commitReady = navPrevReady;
      } else if (this.videoShowNavHint && this.videoNavHintDir === 'next' && this.hasVideoNextNav) {
        kind = 'next';
        commitReady = navNextReady;
      }
      mutations.setNavigationGestureHint({ kind, commitReady, flashClose });
    },
    videoRubberband(value, max) {
      const sign = value < 0 ? -1 : 1;
      const a = Math.abs(value);
      if (a <= max) {
        return value;
      }
      return sign * (max + (a - max) * 0.32);
    },
    decideVideoEdgeKind() {
      if (this.videoEdgeKind) return;
      const ax = Math.abs(this.videoEdgeDx);
      const ay = Math.abs(this.videoEdgeDy);
      if (ax < 12 && ay < 12) return;
      if (this.previewType === 'audio') {
        if (ay > ax * 1.12 && ay > 14 && this.videoEdgeDy > 0) {
          this.videoEdgeKind = 'vertical-dismiss';
        } else if (ax > ay * 1.12 && ax > 14) {
          this.videoEdgeKind = 'horizontal';
        }
        return;
      }
      if (ay > ax * 1.12 && ay > 14) {
        this.videoEdgeKind = this.videoEdgeDy > 0 ? 'vertical-dismiss' : 'vertical-fullscreen';
      } else if (ax > ay * 1.12 && ax > 14) {
        this.videoEdgeKind = 'horizontal';
      }
    },
    applyVideoEdgeVisuals() {
      if (this.previewType === 'audio' && this.videoEdgeDy < 0 && Math.abs(this.videoEdgeDy) > Math.abs(this.videoEdgeDx)) {
        this.videoDragOffsetX = 0;
        this.videoDragOffsetY = 0;
        this.videoShowNavHint = false;
        this.videoShowDismissHint = false;
        this.applyVideoSwipeTransform();
        this.syncVideoNavigationGestureHintToStore();
        return;
      }

      const kind = this.videoEdgeKind;
      if (kind === 'horizontal') {
        this.videoDragOffsetX = this.videoRubberband(this.videoEdgeDx, this.videoEdgeRubberMax);
        this.videoDragOffsetY = 0;
        const adx = Math.abs(this.videoEdgeDx);
        this.videoShowNavHint = adx >= this.videoEdgeHintPx;
        this.videoNavHintDir = this.videoEdgeDx > 0 ? 'prev' : 'next';
        if (this.videoNavHintDir === 'prev' && !this.hasVideoPreviousNav) this.videoShowNavHint = false;
        if (this.videoNavHintDir === 'next' && !this.hasVideoNextNav) this.videoShowNavHint = false;
        this.videoShowDismissHint = false;
      } else if (kind === 'vertical-dismiss') {
        this.videoDragOffsetX = 0;
        this.videoDragOffsetY = this.videoEdgeDy;
        this.videoShowDismissHint = this.videoEdgeDy >= this.videoEdgeHintPx;
        this.videoShowNavHint = false;
      } else if (kind === 'vertical-fullscreen') {
        this.videoDragOffsetX = 0;
        this.videoDragOffsetY = this.videoEdgeDy;
        this.videoShowDismissHint = false;
        this.videoShowNavHint = false;
      } else {
        this.videoDragOffsetX = 0;
        this.videoDragOffsetY = 0;
        this.videoShowNavHint = false;
        this.videoShowDismissHint = false;
      }
      this.applyVideoSwipeTransform();
      this.syncVideoNavigationGestureHintToStore();
    },
    snapBackVideoEdgeGesture() {
      this.videoGestureSnapBack = true;
      this.videoDragOffsetX = 0;
      this.videoDragOffsetY = 0;
      this.videoShowNavHint = false;
      this.videoShowDismissHint = false;
      this.videoEdgeKind = null;
      this.videoEdgeDx = 0;
      this.videoEdgeDy = 0;
      this.setSkipNextTap(200);
      this.applyVideoSwipeTransform();
      mutations.setNavigationGestureHint({});
      setTimeout(() => {
        this.videoGestureSnapBack = false;
        this.applyVideoSwipeTransform();
      }, 240);
    },
    resetVideoSwipeVisualState() {
      this.clearVideoDismissAnimTimers();
      this.videoSwipeSuppressedTouchId = null;
      this.videoEdgeKind = null;
      this.videoEdgeDx = 0;
      this.videoEdgeDy = 0;
      this.videoDragOffsetX = 0;
      this.videoDragOffsetY = 0;
      this.videoShowNavHint = false;
      this.videoShowDismissHint = false;
      this.videoGestureSnapBack = false;
      this.videoDismissFlashActive = false;
      this.applyVideoSwipeTransform();
      mutations.setNavigationGestureHint({});
    },
    resetVideoEdgeGestureImmediate() {
      this.resetVideoSwipeVisualState();
      this.clearEdgeTapGestureState();
      this.skipNextTap = false;
      if (this.skipNextTapTimer) {
        clearTimeout(this.skipNextTapTimer);
        this.skipNextTapTimer = null;
      }
    },
    clearVideoDismissAnimTimers() {
      if (this.videoDismissCloseTimer) {
        clearTimeout(this.videoDismissCloseTimer);
        this.videoDismissCloseTimer = null;
      }
      if (this.videoDismissHintTimer) {
        clearTimeout(this.videoDismissHintTimer);
        this.videoDismissHintTimer = null;
      }
    },
    finishVideoEdgeGesture() {
      if (!this.videoSwipeGesturesActive) {
        this.resetVideoEdgeGestureImmediate();
        return;
      }
      const ax0 = Math.abs(this.videoEdgeDx);
      const ay0 = Math.abs(this.videoEdgeDy);
      if (!this.videoEdgeKind && ax0 < 5 && ay0 < 5) {
        this.resetVideoSwipeVisualState();
        return;
      }
      let kind = this.videoEdgeKind;
      if (!kind) {
        const ax = Math.abs(this.videoEdgeDx);
        const ay = Math.abs(this.videoEdgeDy);
        if (ax < this.videoEdgeHintPx && ay < this.videoEdgeHintPx) {
          this.snapBackVideoEdgeGesture();
          return;
        }
        kind = ax >= ay ? 'horizontal' : 'vertical-dismiss';
      }
      if (kind === 'horizontal') {
        if (this.videoEdgeDx >= this.videoEdgeCommitX && this.hasVideoPreviousNav) {
          this.$emit('navigate-previous');
          this.resetVideoEdgeGestureImmediate();
          return;
        }
        if (this.videoEdgeDx <= -this.videoEdgeCommitX && this.hasVideoNextNav) {
          this.$emit('navigate-next');
          this.resetVideoEdgeGestureImmediate();
          return;
        }
      } else if (kind === 'vertical-dismiss') {
        if (this.videoEdgeDy >= this.videoEdgeCommitY) {
          this.clearVideoDismissAnimTimers();
          this.videoDismissFlashActive = true;
          this.videoShowDismissHint = true;
          this.videoDragOffsetX = 0;
          this.videoDragOffsetY = 0;
          this.videoEdgeKind = null;
          this.applyVideoSwipeTransform();
          this.syncVideoNavigationGestureHintToStore();
          this.setSkipNextTap(200);
          // If we're in fullscreen, exit fullscreen instead of closing preview
          if (this.player?.fullscreen?.active) {
            this.player.fullscreen.exit();
            this.videoDismissHintTimer = setTimeout(() => {
              this.videoDismissHintTimer = null;
              this.videoDismissFlashActive = false;
              this.videoShowDismissHint = false;
              mutations.setNavigationGestureHint({});
            }, 420);
            return;
          }
          // Normal close preview
          this.videoDismissCloseTimer = setTimeout(() => {
            this.videoDismissCloseTimer = null;
            this.$emit('close-preview');
          }, 120);
          this.videoDismissHintTimer = setTimeout(() => {
            this.videoDismissHintTimer = null;
            this.videoDismissFlashActive = false;
            this.videoShowDismissHint = false;
            mutations.setNavigationGestureHint({});
          }, 420);
          return;
        }
      } else if (kind === 'vertical-fullscreen') {
        if (!this.player) {
          this.resetVideoEdgeGestureImmediate();
          return;
        }
        if (this.videoEdgeDy <= -this.videoEdgeCommitY) {
          this.player.fullscreen.toggle();
          this.resetVideoEdgeGestureImmediate();
          // Set skipNextTap to prevent play/pause toggle
          this.setSkipNextTap(300);
          return;
        }
      }
      this.snapBackVideoEdgeGesture();
    },
    teardownVideoSwipeMouseDocListeners() {
      document.removeEventListener('mousemove', this.onVideoSwipeMouseDocMove, true);
      document.removeEventListener('mouseup', this.onVideoSwipeMouseDocUp, true);
      this.videoEdgeMouseActive = false;
    },
    onVideoSwipeMouseDocMove(event) {
      if (!this.videoEdgeMouseActive || !this.videoSwipeGesturesActive) {
        return;
      }
      this.videoEdgeDx = event.clientX - this.videoEdgeStartX;
      this.videoEdgeDy = event.clientY - this.videoEdgeStartY;
      this.decideVideoEdgeKind();
      this.applyVideoEdgeVisuals();
      if (Math.abs(this.videoEdgeDx) > 3 || Math.abs(this.videoEdgeDy) > 3) {
        event.preventDefault();
      }
    },
    onVideoSwipeMouseDocUp(event) {
      if (!this.videoEdgeMouseActive) {
        this.teardownVideoSwipeMouseDocListeners();
        return;
      }
      this.videoEdgeDx = event.clientX - this.videoEdgeStartX;
      this.videoEdgeDy = event.clientY - this.videoEdgeStartY;
      this.finishVideoEdgeGesture();
      this.teardownVideoSwipeMouseDocListeners();
      if (Math.abs(this.videoEdgeDx) > 3 || Math.abs(this.videoEdgeDy) > 3) {
        event.preventDefault();
      }
    },
    onVideoSwipeMouseDown(event) {
      if (event.button !== 0 || !this.videoSwipeGesturesActive) return;
      // Don't start a gesture if we are selecting some text
      if (window.getSelection()?.toString().length > 0) return;
      if (this.isPlyrControlOrMenuTarget(event.target)) {
        return;
      }
      this.clearVideoDismissAnimTimers();
      this.teardownVideoSwipeMouseDocListeners();
      this.videoEdgeMouseActive = true;
      this.videoEdgeStartX = event.clientX;
      this.videoEdgeStartY = event.clientY;
      this.videoEdgeDx = 0;
      this.videoEdgeDy = 0;
      this.videoEdgeKind = null;
      document.addEventListener('mousemove', this.onVideoSwipeMouseDocMove, true);
      document.addEventListener('mouseup', this.onVideoSwipeMouseDocUp, true);
    },
    onVideoSwipeTouchStart(event) {
      if (!this.videoSwipeGesturesActive || event.targetTouches.length !== 1) return;
      // Don't start a gesture if we are selecting some text
      if (window.getSelection()?.toString().length > 0) return;
      if (this.isPlyrControlOrMenuTarget(event.target)) {
        this.videoSwipeSuppressedTouchId = event.targetTouches[0].identifier;
        return;
      }
      this.videoSwipeSuppressedTouchId = null;
      this.clearVideoDismissAnimTimers();

      // long-press timer
      this.clearLongPressTimer();
      this.longPressPending = true;
      this.longPressTriggered = false;
      this.longPressPreviousSpeed = this.player?.speed || 1;

      // only start timer if not already at 2x
      if (this.player && this.player.speed !== 2) {
        this.longPressTimer = setTimeout(() => {
          this.longPressTimer = null;
          if (this.player && this.longPressPending) {
            this.longPressPreviousSpeed = this.player.speed || 1;
            this.player.speed = 2;
            this.longPressTriggered = true;
            this.longPressPending = false;
            // Show toast
            this.speedToastVisible = true;
            this.speedToastMessage = '2x';
          }
        }, 500);
      } else {
        // If already at 2x don't change speed again
        if (this.player?.speed === 2) {
          this.longPressPending = false;
        }
      }

      const touch = event.targetTouches[0];
      this.videoEdgeStartX = touch.pageX;
      this.videoEdgeStartY = touch.pageY;
      this.videoEdgeDx = 0;
      this.videoEdgeDy = 0;
      this.videoEdgeKind = null;
      this.videoDragOffsetX = 0;
      this.videoDragOffsetY = 0;
    },
    onVideoSwipeTouchMove(event) {
      if (this.longPressTriggered) {
        event.preventDefault();
        return;
      }

      // If there's significant movement cancel any pending long press so if when using swipe gestures they don't change speed
      if (this.longPressPending) {
        const touch = event.targetTouches[0];
        const dx = Math.abs(touch.pageX - this.videoEdgeStartX);
        const dy = Math.abs(touch.pageY - this.videoEdgeStartY);
        if (dx > 20 || dy > 20) {
          this.clearLongPressTimer();
          this.longPressPending = false;
          this.clearEdgeTapGestureState();
        }
      }

      if (!this.videoSwipeGesturesActive || event.targetTouches.length !== 1) {
        if (this.videoEdgeKind || this.videoEdgeDx || this.videoEdgeDy) {
          this.resetVideoEdgeGestureImmediate();
        }
        return;
      }
      const touch = event.targetTouches[0];
      if (
        this.videoSwipeSuppressedTouchId !== null &&
        touch.identifier === this.videoSwipeSuppressedTouchId
      ) {
        return;
      }
      this.videoEdgeDx = touch.pageX - this.videoEdgeStartX;
      this.videoEdgeDy = touch.pageY - this.videoEdgeStartY;
      this.decideVideoEdgeKind();
      this.applyVideoEdgeVisuals();
      const ax = Math.abs(this.videoEdgeDx);
      const ay = Math.abs(this.videoEdgeDy);
      if (this.videoEdgeKind || ax > 14 || ay > 14) {
        event.preventDefault();
      }
    },
    onVideoSwipeTouchEnd(event) {
      if (!this.videoSwipeGesturesActive || event.changedTouches.length === 0) {
        return;
      }
      const t = event.changedTouches[0];
      if (
        this.videoSwipeSuppressedTouchId !== null &&
        t.identifier === this.videoSwipeSuppressedTouchId
      ) {
        this.videoSwipeSuppressedTouchId = null;
        return;
      }
      if (this.longPressPending) {
        this.clearLongPressTimer();
        this.longPressPending = false;
      }

      // Handle long-press release
      if (this.longPressTriggered) {
        this.clearLongPressTimer();
        this.speedToastVisible = false;
        if (this.player && this.longPressPreviousSpeed !== 2) {
          this.player.speed = this.longPressPreviousSpeed;
        }
        this.longPressTriggered = false;
        this.clearEdgeTapGestureState();
        this.resetVideoEdgeGestureImmediate();
        event.preventDefault();
        return;
      }

      // Normal swipe gesture
      this.videoEdgeDx = t.pageX - this.videoEdgeStartX;
      this.videoEdgeDy = t.pageY - this.videoEdgeStartY;
      const ax = Math.abs(this.videoEdgeDx);
      const ay = Math.abs(this.videoEdgeDy);
      const hadLockedKind = this.videoEdgeKind !== null;
      this.finishVideoEdgeGesture();
      if (hadLockedKind || ax > 14 || ay > 14) {
        this.clearEdgeTapGestureState();
        event.preventDefault();
      }
    },
    onVideoSwipeTouchCancel(event) {
      // If long-press was triggered, handle release
      if (this.longPressTriggered) {
        this.clearLongPressTimer();
        this.speedToastVisible = false;
        if (this.player && this.longPressPreviousSpeed !== 2) {
          this.player.speed = this.longPressPreviousSpeed;
        }
        this.longPressTriggered = false;
        this.longPressPending = false;
        this.resetVideoEdgeGestureImmediate();
        if (event) event.preventDefault();
        return;
      }

      // If long-press was pending clear it
      if (this.longPressPending) {
        this.clearLongPressTimer();
        this.longPressPending = false;
      }

      if (event?.changedTouches?.length) {
        const t = event.changedTouches[0];
        if (
          this.videoSwipeSuppressedTouchId !== null &&
          t.identifier === this.videoSwipeSuppressedTouchId
        ) {
          this.videoSwipeSuppressedTouchId = null;
          return;
        }
      }
      if (this.videoEdgeKind || this.videoEdgeDx || this.videoEdgeDy) {
        this.snapBackVideoEdgeGesture();
      }
    },
    setupVideoSwipeGestures() {
      this.teardownVideoSwipeGestures();
      if ((this.previewType !== 'video' && this.previewType !== 'audio') || !this.player) {
        return;
      }
      const surface = this.getPlyrGestureSurface();
      if (!surface) {
        return;
      }
      const touchOpts = { passive: false };
      surface.addEventListener('touchstart', this.onVideoSwipeTouchStart, touchOpts);
      surface.addEventListener('touchmove', this.onVideoSwipeTouchMove, touchOpts);
      surface.addEventListener('touchend', this.onVideoSwipeTouchEnd, touchOpts);
      surface.addEventListener('touchcancel', this.onVideoSwipeTouchCancel, touchOpts);
      surface.addEventListener('mousedown', this.onVideoSwipeMouseDown);

      this.videoSwipeCleanup = () => {
        surface.removeEventListener('touchstart', this.onVideoSwipeTouchStart, touchOpts);
        surface.removeEventListener('touchmove', this.onVideoSwipeTouchMove, touchOpts);
        surface.removeEventListener('touchend', this.onVideoSwipeTouchEnd, touchOpts);
        surface.removeEventListener('touchcancel', this.onVideoSwipeTouchCancel, touchOpts);
        surface.removeEventListener('mousedown', this.onVideoSwipeMouseDown);
        this.teardownVideoSwipeMouseDocListeners();
      };
    },
    teardownVideoSwipeGestures() {
      if (typeof this.videoSwipeCleanup === 'function') {
        this.videoSwipeCleanup();
        this.videoSwipeCleanup = null;
      }
      this.clearVideoDismissAnimTimers();
      this.resetVideoEdgeGestureImmediate();
      this.clearLongPressTimer();
      this.clearLongPressTimer();
      this.longPressPending = false;
      this.speedToastVisible = false;
      if (this.longPressTriggered && this.player && this.longPressPreviousSpeed !== 2) {
        this.player.speed = this.longPressPreviousSpeed;
      }
      this.longPressTriggered = false;
    },
    clearLongPressTimer() {
      if (this.longPressTimer) {
        clearTimeout(this.longPressTimer);
        this.longPressTimer = null;
      }
      if (this.longPressTimer) {
        clearTimeout(this.longPressTimer);
        this.longPressTimer = null;
      }
    },
    setSkipNextTap(delay) {
      this.clearEdgeTapGestureState();
      if (this.skipNextTapTimer) {
        clearTimeout(this.skipNextTapTimer);
        this.skipNextTapTimer = null;
      }
      this.skipNextTap = true;
      this.skipNextTapTimer = setTimeout(() => {
        this.skipNextTap = false;
        this.skipNextTapTimer = null;
      }, delay);
    },
    // Screen rotation in fullscreen is left to the browser (see utils/pageFullscreen.js).
    onFullscreenEnter() {
      this.isFullscreen = true;
      this.resetVideoEdgeGestureImmediate();
    },
    onFullscreenExit() {
      this.isFullscreen = false;
    },
    handleMediaEnd() {
      // B can be so close to the end that the file ends before a check sees it.
      if (abLoopActive(this.abLoop) && this.abLoopArmed) {
        this.player.currentTime = this.abLoop.a;
        this.player.play();
        return;
      }
      mutations.setPlaybackState(false);
      const queue = state.playbackQueue.queue;
      const currentIndex = state.playbackQueue.currentIndex;
      const loop = state.playbackQueue.loop;
      const action = getEndOfMediaAction(queue, currentIndex, loop);
      if (action === 'next') {
        navigatePlaybackQueue(1);
      } else if (action === 'restart') {
        this.restartCurrentFile();
      }
    },
    playPrevious() {
      navigatePlaybackQueue(-1);
    },

    playNext() {
      navigatePlaybackQueue(1);
    },
    restartCurrentFile() {
      if (this.player) {
        this.player.currentTime = 0;
        this.player.play();
      }
    },
    setupPlaybackQueue() {
      const listing = this.listing;
      if (!listing || !this.req) return;
      const isShare = getters.isShare();
      const currentItem = this.req;
      const mode = state.playbackQueue.mode || 'single';
      const { queue, currentIndex } = buildPlaybackQueue(listing, currentItem, mode, isShare);
      mutations.setPlaybackQueue({
        queue,
        currentIndex,
        mode,
        loop: this.loop
      });
    },
    // Builds playback and caption menus once, then updates state without rebuilding again.
    applyCustomSettings(player) {
      try {
        const playbackBtn = player.elements.settings?.buttons?.playback;
        const playbackPanel = player.elements.settings?.panels?.playback;
        const captionSizeBtn = player.elements.settings?.buttons?.captionSize;
        const captionSizePanel = player.elements.settings?.panels?.captionSize;

        // --- Playback menu ---
        if (playbackBtn && playbackPanel) {
          playbackBtn.removeAttribute('hidden');
          const title = player.config.i18n?.playback || 'Playback';
          const currentLabel = getModeLabel(this.playbackMode, this.$t, this.queueCount);

          if (!this.playbackMenuInitialized) {
            const menu = playbackPanel.querySelector('div[role="menu"]');
            menu.innerHTML = `
              <button data-plyr="playback" type="button" role="menuitemradio" class="plyr__control" value="single">
                <span>${getModeLabel('single', this.$t)}</span>
              </button>
              <button data-plyr="playback" type="button" role="menuitemradio" class="plyr__control" value="sequential">
                <span>${getModeLabel('sequential', this.$t)}</span>
              </button>
              <button data-plyr="playback" type="button" role="menuitemradio" class="plyr__control" value="shuffle">
                <span>${getModeLabel('shuffle', this.$t)}</span>
              </button>
            `;
            this.playbackButtons = menu.querySelectorAll('button[data-plyr="playback"]');
            // Set initial checked state
            for (const btn of this.playbackButtons) {
              btn.setAttribute('aria-checked', btn.getAttribute('value') === this.playbackMode);
            }
            // Add click listeners
            for (const btn of this.playbackButtons) {
              btn.addEventListener('click', (event) => {
                const value = event.currentTarget.getAttribute('value');
                if (value === 'single') {
                  clearPlaybackQueue();
                } else {
                  cyclePlaybackModes(this.playbackMode, {
                    listing: this.listing,
                    currentItem: this.req,
                    isShare: getters.isShare(),
                    targetMode: value
                  });
                }
                const newLabel = getModeLabel(value, this.$t);
                if (this.playbackValueSpan) this.playbackValueSpan.textContent = newLabel;
                for (const b of this.playbackButtons) {
                  b.setAttribute('aria-checked', b.getAttribute('value') === value);
                }
              });
            }
            const valueSpan = playbackBtn.querySelector('span .plyr__menu__value');
            if (valueSpan) {
              valueSpan.textContent = currentLabel;
              this.playbackValueSpan = valueSpan;
            } else {
              playbackBtn.querySelector('span').innerHTML = `${title}: <span class="plyr__menu__value">${currentLabel}</span>`;
              this.playbackValueSpan = playbackBtn.querySelector('span .plyr__menu__value');
            }
            this.lastAppliedMode = this.playbackMode;
            this.playbackMenuInitialized = true;
          } else {
            // Just update checked states and label
            if (this.playbackButtons) {
              for (const btn of this.playbackButtons) {
                btn.setAttribute('aria-checked', btn.getAttribute('value') === this.playbackMode);
              }
            }
            if (this.playbackValueSpan) {
              this.playbackValueSpan.textContent = currentLabel;
            }
          }
        }
        // --- Loop menu (off, loop all, loop single) ---
        const loopBtn = player.elements.settings?.buttons?.loop;
        const loopPanel = player.elements.settings?.panels?.loop;
        if (loopBtn && loopPanel) {
          loopBtn.removeAttribute('hidden');
          const loopTitle = this.$t('player.loop');
          const currentLoopLabel = getLoopLabel(this.loop, this.$t);

          if (!this.loopMenuInitialized) {
            const menu = loopPanel.querySelector('div[role="menu"]');
            menu.innerHTML = `
              <button data-plyr="loop" type="button" role="menuitemradio" class="plyr__control" value="off">
                <span>${getLoopLabel('off', this.$t)}</span>
              </button>
              <button data-plyr="loop" type="button" role="menuitemradio" class="plyr__control" value="all">
                <span>${getLoopLabel('all', this.$t)}</span>
              </button>
              <button data-plyr="loop" type="button" role="menuitemradio" class="plyr__control" value="single">
                <span>${getLoopLabel('single', this.$t)}</span>
              </button>
            `;
            this.loopButtons = menu.querySelectorAll('button[data-plyr="loop"]');
            // Set initial checked state
            for (const btn of this.loopButtons) {
              btn.setAttribute('aria-checked', btn.getAttribute('value') === this.loop);
            }
            // Add click listeners
            for (const btn of this.loopButtons) {
              btn.addEventListener('click', (event) => {
                const value = event.currentTarget.getAttribute('value');
                if (value !== this.loop) {
                  mutations.setPlaybackQueue({
                    queue: this.playbackQueue,
                    currentIndex: this.currentQueueIndex,
                    mode: this.playbackMode,
                    loop: value
                  });
                }
                const newLabel = getLoopLabel(value, this.$t);
                if (this.loopValueSpan) this.loopValueSpan.textContent = newLabel;
                for (const b of this.loopButtons) {
                  b.setAttribute('aria-checked', b.getAttribute('value') === value);
                }
              });
            }
            const valueSpan = loopBtn.querySelector('span .plyr__menu__value');
            if (valueSpan) {
              valueSpan.textContent = currentLoopLabel;
              this.loopValueSpan = valueSpan;
            } else {
              loopBtn.querySelector('span').innerHTML = `${loopTitle}: <span class="plyr__menu__value">${currentLoopLabel}</span>`;
              this.loopValueSpan = loopBtn.querySelector('span .plyr__menu__value');
            }
            this.loopMenuInitialized = true;
          } else {
            // Update checked states and label
            if (this.loopButtons) {
              for (const btn of this.loopButtons) {
                btn.setAttribute('aria-checked', btn.getAttribute('value') === this.loop);
              }
            }
            if (this.loopValueSpan) {
              this.loopValueSpan.textContent = currentLoopLabel;
            }
          }
        }
        // --- Caption size menu ---
        if (captionSizeBtn && captionSizePanel) {
          const visible = this.previewType === 'video' && this.hasSubtitles;
          if (!visible) {
            captionSizeBtn.setAttribute('hidden', '');
            this.captionSizeMenuInitialized = true;
            return;
          }
          captionSizeBtn.removeAttribute('hidden');
          const title = player.config.i18n?.captionSize || 'Caption size';
          const currentSize = this.getStoredCaptionSize();
          const currentSizeLabel = this.getCaptionSizeLabel(currentSize);

          if (!this.captionSizeMenuInitialized) {
            const menu = captionSizePanel.querySelector('div[role="menu"]');
            menu.innerHTML = PLYR_CAPTION_SIZE_IDS.map(
              (id) => `<button type="button" data-plyr="caption-size" role="menuitemradio" class="plyr__control" value="${id}">
                        <span>${this.getCaptionSizeLabel(id)}</span>
                      </button>`
            ).join('');

            this.captionSizeButtons = menu.querySelectorAll('button[data-plyr="caption-size"]');
            // Set initial checked state
            for (const btn of this.captionSizeButtons) {
              btn.setAttribute('aria-checked', btn.getAttribute('value') === currentSize);
            }
            // Add click listeners
            for (const btn of this.captionSizeButtons) {
              btn.addEventListener('click', (event) => {
                const value = event.currentTarget.getAttribute('value');
                if (!PLYR_CAPTION_SIZE_IDS.includes(value)) return;
                this.setStoredCaptionSize(value);
                this.applyCaptionSizeClass();
                // Update checked states and label
                for (const b of this.captionSizeButtons) {
                  b.setAttribute('aria-checked', b.getAttribute('value') === value);
                }
                // Update label in button
                const label = this.getCaptionSizeLabel(value);
                if (this.captionSizeValueSpan) this.captionSizeValueSpan.textContent = label;
              });
            }
            const valueSpan = captionSizeBtn.querySelector('span .plyr__menu__value');
            if (valueSpan) {
              valueSpan.textContent = currentSizeLabel;
              this.captionSizeValueSpan = valueSpan;
            } else {
              captionSizeBtn.querySelector('span').innerHTML = `${title}: <span class="plyr__menu__value">${currentSizeLabel}</span>`;
              this.captionSizeValueSpan = captionSizeBtn.querySelector('span .plyr__menu__value');
            }
            this.captionSizeMenuInitialized = true;
          } else {
            // Update checked states and label
            if (this.captionSizeButtons) {
              for (const btn of this.captionSizeButtons) {
                btn.setAttribute('aria-checked', btn.getAttribute('value') === currentSize);
              }
            }
            if (this.captionSizeValueSpan) {
              this.captionSizeValueSpan.textContent = currentSizeLabel;
            }
          }
          this.applyCaptionSizeClass();
        }
      } catch (error) {
        console.error('Error applying custom settings:', error);
      }
    },
  },
};
</script>

<style >
@import url("plyr/dist/plyr.css");

/* Remove blue overlay when tapping on mobile */
.plyr,
.plyr__video-wrapper,
.plyr video {
  -webkit-tap-highlight-color: transparent;
}

.plyr__video-wrapper:focus,
.plyr video:focus {
  outline: none;
}

/* Background styles for the audio player */
.plyr-background-dark {
  background: radial-gradient(#3b3b3b, black);
}

.plyr-background-light {
  background: radial-gradient(#262626, #e2e2e2);
}

/**********************************
*** STYLES FOR THE MEDIA PLAYER ***
**********************************/

.plyr-video-container {
  width: 100%;
  height: 100%;
}

.plyr-viewer {
  position: relative;
  height: 100%;
  width: 100%;
}

.plyr {
  --plyr-color-main: var(--primaryColor);
  --plyr-video-background: rgb(0 0 0 / 100%);
  --plyr-focus-visible-color: var(--primaryColor);
  --plyr-audio-control-color: #fff;
  --plyr-menu-background: rgb(0 0 0 / 70%);
  --plyr-menu-color: #fff;
  --plyr-menu-border-shadow-color: rgb(0 0 0 / 50%);
  --plyr-menu-radius: 12px;
  --plyr-menu-shadow: 0 1px 2px rgb(0 0 0 / 50%);
  --plyr-control-radius: 12px;
  --plyr-control-icon-size: 16px;
  --plyr-control-spacing: 8px;
  --plyr-control-padding: 6px;
  --plyr-tooltip-background: rgb(0 0 0 / 80%);
  --plyr-tooltip-color: #fff;
  --plyr-video-controls-background: linear-gradient(transparent,
          rgb(0 0 0 / 70%));

  overflow: visible;
  background-color: rgb(216 216 216);
  box-shadow: 0 2px 6px rgb(88 88 88 / 45%);
}

.plyr__controls {
  color: black;
}

/* Position/space of the buttons */
.plyr .plyr__controls {
  display: flex;
  flex-direction: row;
  gap: 8px;
  background-color: transparent;
}

.plyr .plyr__controls__items {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: nowrap;
}

/* Transitions (e.g. how much time take to hide the player UI) */
.plyr .plyr__control {
  transition: background 0.2s ease, color 0.2s ease, box-shadow 0.2s ease;
  flex-shrink: 0;
  display: flex;
  min-width: 2em;
  justify-content: center;
  align-items: center;
}

/* Progress bar with full width (audio and video) */
.plyr .plyr__progress__container {
  flex: 100%;
  margin: 0;
}

/* Scrub preview popup (mounted on Plyr container / fullscreen root while scrubbing) */
.fb-scrub-preview {
  position: fixed;
  z-index: 2147483646;
  pointer-events: none;
  transform: translate(-50%, calc(-100% - 14px));
  opacity: 0;
  visibility: hidden;
  transition: opacity 0.12s ease;
}

.fb-scrub-preview--visible {
  opacity: 1;
  visibility: visible;
}

.fb-scrub-preview__frame {
  overflow: hidden;
  position: relative;
  max-height: 600px;
  max-width: 600px;
  border-radius: 14px;
  background: #000;
  border: 2px solid var(--primaryColor);
  box-shadow:
    0 0 0 1px rgb(0 0 0 / 35%),
    0 6px 20px rgb(0 0 0 / 55%),
    0 0 12px color-mix(in srgb, var(--primaryColor) 35%, transparent);
}

.fb-scrub-preview__frame::after {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 44%;
  background: linear-gradient(to top, rgb(0 0 0 / 60%), rgb(0 0 0 / 0%));
  pointer-events: none;
}

.fb-scrub-preview__frame img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: contain;
}

.fb-scrub-preview__loading {
  position: absolute;
  inset: 0;
  z-index: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgb(0 0 0 / 45%);
}

.fb-scrub-preview__loading[hidden] {
  display: none;
}

/* Spinner sits above the previous frame while the next preview loads. */
.fb-scrub-preview__frame--loading:not(.fb-scrub-preview__frame--empty) .fb-scrub-preview__loading {
  background: rgb(0 0 0 / 50%);
}

.fb-scrub-preview__frame--loading:not(.fb-scrub-preview__frame--empty) img {
  opacity: 1;
}

.fb-scrub-preview__loading .loader {
  position: relative;
  z-index: 1;
}

.fb-scrub-preview__time {
  position: absolute;
  left: 50%;
  bottom: 8px;
  transform: translateX(-50%);
  z-index: 1;
  font-size: 13px;
  font-weight: 600;
  line-height: 1;
  color: #fff;
  white-space: nowrap;
  text-shadow:
    0 1px 3px rgb(0 0 0 / 90%),
    0 0 8px rgb(0 0 0 / 60%);
  pointer-events: none;
}

.fb-scrub-preview__arrow {
  position: absolute;
  top: calc(100% - 2px);
  transform: translateX(-50%);
  width: 14px;
  height: 8px;
  pointer-events: none;
}

.fb-scrub-preview__arrow::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  width: 0;
  height: 0;
  border-left: 7px solid transparent;
  border-right: 7px solid transparent;
  border-top: 8px solid var(--primaryColor);
}

.plyr__control--overlaid {
  background: var(--plyr-video-control-background-hover, var(--primaryColor));
  border: 0;
  display: none;
  position: absolute;
  transition:
    opacity 0.2s ease-out,
    visibility 0.2s ease-out,
    transform 0.3s ease,
    box-shadow 0.3s ease !important;
  z-index: 5;
  height: 4em;
  inset: 50% auto auto 50%;
  transform: translate(-50%, -50%) !important;
  width: 4em !important;
  margin: 0 !important;
  border-radius: 5em !important;
  pointer-events: auto;
  cursor: pointer;
  outline: none;
  box-shadow: 0 2px 10px rgb(0 0 0 / 30%);
}

.plyr--fullscreen-active .plyr__control--overlaid {
  top: 50% !important;
  left: 50% !important;
  transform: translate(-50%, -50%) !important;
}

/* Hide the overlaid play button in audio mode */
.plyr--audio .plyr__control--overlaid {
  display: none !important;
}

/* Big play button when pause/start the video */
.plyr--full-ui.plyr--video .plyr__control--overlaid {
  display: flex;
  justify-content: center;
  align-items: center;
}

.plyr--video .plyr__control--overlaid:hover,
.plyr--video .plyr__control--overlaid:focus-visible,
.plyr--fullscreen-active .plyr__control--overlaid:hover,
.plyr--fullscreen-active .plyr__control--overlaid:focus-visible {
  transform: translate(-50%, -50%) scale(1.05) !important;
  opacity: 1 !important;
  outline: none;
  box-shadow:
    0 0 0 2px rgb(255 255 255 / 90%),
    0 8px 25px color-mix(in srgb, var(--primaryColor) 30%, transparent),
    0 4px 12px rgb(0 0 0 / 20%);
}

.plyr.fb-overlaid--fade-in .plyr__control--overlaid {
  transition:
    opacity 0.4s ease-in-out,
    transform 0.3s ease,
    box-shadow 0.3s ease !important;
}

.plyr.fb-overlaid--fade-out .plyr__control--overlaid {
  opacity: 0 !important;
  visibility: visible !important;
  pointer-events: none !important;
  transition:
    opacity 0.4s ease-in-out,
    transform 0.3s ease,
    box-shadow 0.3s ease !important;
}

.plyr--playing:not(.plyr--hide-controls) .plyr__control--overlaid,
.plyr--playing.fb-overlaid--shown:not(.fb-overlaid--fade-out) .plyr__control--overlaid {
  opacity: 1 !important;
  visibility: visible !important;
  pointer-events: auto !important;
  transition:
    opacity 0.4s ease-in-out,
    transform 0.3s ease,
    visibility 0.2s ease-out,
    box-shadow 0.3s ease !important;
}

/* Hide center button while playing unless shown or fading out */
.plyr--playing.plyr--hide-controls:not(.fb-overlaid--shown, .fb-overlaid--fade-out)
  .plyr__control--overlaid {
  opacity: 0 !important;
  visibility: hidden !important;
  pointer-events: none !important;
}

/************
*** VIDEO ***
************/

/* Video container size */
.video-player-container {
  position: relative;
  width: 100%;
  height: 100%;
  background-color: #000;
}

.video-loading-overlay {
  position: absolute;
  inset: 0;
  z-index: 6;
  display: flex;
  align-items: center;
  justify-content: center;
  pointer-events: none;
  background: rgb(0 0 0 / 35%);
}

/* Letterboxing and Plyr chrome: match cinema-style black (audio uses .audio-controls-container .plyr) */
.video-player-container .plyr {
  background-color: #000;
  box-shadow: none;
}

.video-player-container .plyr .plyr__controls {
  color: #fff;
}

/* Video size in the container */
.plyr.plyr--video {
  width: 100%;
  height: 100%;
}

.plyr .plyr__video-wrapper {
  touch-action: manipulation;
  cursor: pointer;
}

/* Double-tap / double-click seek feedback (left/right third of video) */
.ab-loop-marker {
  position: absolute;
  top: 50%;
  z-index: 3;
  width: 3px;
  height: 14px;
  border-radius: 1px;
  background: #fff;
  box-shadow: 0 0 0 1px rgb(0 0 0 / 60%);
  pointer-events: none;
  transform: translate(-50%, -50%);
}

.media-osd {
  position: absolute;
  top: 1em;
  left: 1em;
  z-index: 9;
  padding: 0.25em 0.6em;
  border-radius: 0.3em;
  background: rgb(0 0 0 / 60%);
  color: #fff;
  font-size: 1.1rem;
  font-variant-numeric: tabular-nums;
  pointer-events: none;
}

.media-stats {
  position: absolute;
  top: 3.4rem;
  left: 1rem;
  z-index: 9;
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 0.1rem 0.9rem;
  max-width: min(34rem, calc(100% - 2rem));
  max-height: calc(100% - 9rem);
  margin: 0;
  padding: 0.6rem 0.8rem;
  overflow: hidden;
  border: 1px solid var(--divider);
  border-radius: 0.5rem;
  background: color-mix(in srgb, var(--background) 88%, transparent);
  color: var(--textPrimary);
  font-size: 0.8rem;
  text-align: left;
  font-variant-numeric: tabular-nums;
  line-height: 1.45;
  pointer-events: none;
}

.media-stats dt {
  color: var(--textSecondary);
}

.media-stats dd {
  margin: 0;
  overflow-wrap: anywhere;
}

/* --header-height is in em, which would resolve against these elements' own font sizes. */
html.fb-theater:not(.fb-page-fullscreen) .media-osd {
  top: calc(5.1rem + var(--safe-area-top));
}

html.fb-theater:not(.fb-page-fullscreen) .media-stats {
  top: calc(7.4rem + var(--safe-area-top));
  max-height: calc(100% - 13rem - var(--safe-area-top));
}

.media-volume-bar {
  position: absolute;
  top: 50%;
  right: calc(var(--fab-size) + 2rem); /* clear of the next-file button at the edge */
  z-index: 9;
  box-sizing: border-box;
  width: 2.5rem;
  height: min(45%, 18rem);
  overflow: hidden;
  border: 2px solid var(--primaryColor);
  border-radius: 1rem;
  background: var(--background);
  opacity: 0;
  pointer-events: none;
  transform: translateY(-50%);
  transition: opacity 0.25s ease;
}

.media-volume-bar--visible {
  opacity: 1;
  transition-duration: 0.05s;
}

.media-volume-bar__fill {
  position: absolute;
  right: 0;
  bottom: 0;
  left: 0;
  background: var(--primaryColor);
  transition: height 0.08s linear;
}

.video-skip-feedback-layer {
  position: absolute;
  inset: 0;
  z-index: 8;
  display: flex;
  align-items: center;
  justify-content: center;
  pointer-events: none;
}

.video-skip-feedback-layer--left {
  justify-content: flex-start;
  padding-left: min(22%, 7rem);
}

.video-skip-feedback-layer--right {
  justify-content: flex-end;
  padding-right: min(22%, 7rem);
}

.video-skip-feedback-layer__icon {
  flex-shrink: 0;
  font-size: clamp(2.5rem, 7vmin, 6rem);
  line-height: 1;
  color: rgb(255 255 255 / 96%);
  filter: drop-shadow(0 2px 16px rgb(0 0 0 / 85%));
  opacity: 0;
  transform: scale(0.55);
}

.video-skip-feedback-layer__icon svg {
  display: block;
  width: 1em;
  height: 1em;
  fill: currentcolor;
}

.video-skip-feedback-layer--visible .video-skip-feedback-layer__icon {
  animation: video-skip-feedback-pop 0.7s cubic-bezier(0.34, 1.56, 0.64, 1) forwards;
}

/* Key repeat restarts the animation; holding must keep the icon on screen, not stuck at its start. */
.video-skip-feedback-layer--visible .video-skip-feedback-layer__icon--held {
  animation: video-skip-feedback-hold 0.7s linear forwards;
}

@keyframes video-skip-feedback-hold {
  0%,
  40% {
    opacity: 1;
    transform: scale(1);
  }
  100% {
    opacity: 0;
    transform: scale(1);
  }
}

@keyframes video-skip-feedback-pop {
  0% {
    opacity: 0;
    transform: scale(0.55);
  }
  28% {
    opacity: 1;
    transform: scale(1.12);
  }
  100% {
    opacity: 0;
    transform: scale(1);
  }
}

/* Hide the captions button in audio mode */
.plyr--audio .plyr__control[data-plyr="captions"] {
  display: none !important;
}

/* Hide captions button when there are no subtitle tracks */
.video-player-container.no-captions .plyr__control[data-plyr="captions"] {
  display: none !important;
}

/*
 * Caption size: --fb-captions-font-size on .plyr (plyr-caption-size--* from JS; inherits to .plyr__captions).
 * Video: cqmin replaces vmin when supported so small players don’t use the full viewport scale.
 */
.plyr__captions {
  pointer-events: none;
  font-size: var(--fb-captions-font-size, max(20px, 4vmin));
  line-height: 150%;
  font-weight: 700;
  -webkit-font-smoothing: antialiased;
  color: #fff;
  -webkit-text-stroke: 0.1em #000;
  paint-order: stroke fill;
  text-shadow:
    0.0625em 0.0625em 0 #000,
    -0.0625em 0.0625em 0 #000,
    -0.0625em -0.0625em 0 #000,
    0.0625em -0.0625em 0 #000,
    0 0.08em 0.2em rgb(0 0 0 / 55%);
}

.plyr.plyr-caption-size--small {
  --fb-captions-font-size: max(1em, 2.5vmin);
}

.plyr.plyr-caption-size--medium {
  --fb-captions-font-size: max(1.5em, 4vmin);
}

.plyr.plyr-caption-size--large {
  --fb-captions-font-size: max(2em, 5vmin);
}

.plyr.plyr-caption-size--xlarge {
  --fb-captions-font-size: max(2.5em, 5.5vmin);
}

.video-player-container .plyr--fullscreen-fallback .plyr__captions,
.video-player-container .plyr:fullscreen .plyr__captions {
  font-size: var(--fb-captions-font-size);
}

.plyr__caption {
  background: transparent;
  line-height: 150%;
}

/************
*** AUDIO ***
************/

.plyr.plyr--audio {
  border-radius: var(--borderRadius);
  backdrop-filter: var(--panel-blur);
  background-color: var(--alt-background);
}

.audio-controls-container .plyr .plyr__controls {
  color: var(--textPrimary);
}

/* Hide some unnesary buttons on the audio player */
.plyr--audio .plyr__control[data-plyr="fullscreen"],
.plyr--audio .plyr__control[data-plyr="pip"] {
  display: none !important;
}

/* Style for audio player on mobile */
@media (width <= 768px) {
  /* Buttons container more "big" for easy touch */
  .plyr--audio .plyr__control {
    min-width: 44px;
    min-height: 44px;
  }
  .plyr--audio .plyr__progress__container {
    margin: 10px 0;
  }
  .plyr--audio .plyr__controls__items {
    justify-content: center;
    gap: 12px;
  }

  /* Play button a bit more big */
  .plyr--audio .plyr__control--play {
    transform: scale(1.25);
  }
}

/*****************************
*** ALBUM ART AND METADATA ***
*****************************/

.audio-player-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 1em;
  margin: 0 auto;
  padding: 1em;
  width: 100%;
  box-sizing: border-box;
  height: 100%;
  justify-content: center;
}

/* When mobile lyrics are open: push art+meta to top */
.audio-player-container--lyrics-open {
  justify-content: flex-start;
  gap: 0;
}

/* Full-area swipe / double-tap seek (album art + metadata + Plyr); skip overlay uses position absolute. */
.audio-player-container--plyr-gestures {
  position: relative;
  touch-action: manipulation;
}

.audio-player-content {
  width: 100%;
  max-width: 1500px;
  margin: 0 auto;
  padding: 0 3.5em;
  padding-bottom: 0;
  box-sizing: border-box;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: flex-start;
  overflow: hidden;
  position: relative;
}

.audio-player-container--lyrics-open .audio-player-content {
  height: auto;
  flex: none;
}

/* Left column (album art + metadata) */
.audio-left-column {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  width: 100%;
  padding: 0 2em;
  box-sizing: border-box;
  transition: width 0.4s cubic-bezier(0.25, 0.8, 0.25, 1);
  min-width: 0;
  flex-shrink: 0;
}

.panel-open .audio-left-column {
  width: 50%;
}

/* Right panel (lyrics / queue) */
.lyrics-panel {
  width: 50%;
  flex-shrink: 0;
  height: 100%;
  overflow-y: auto;
  padding: 0.5em 2em;
  color: var(--textPrimary);
  scroll-behavior: smooth;
  text-align: center;
  background: transparent;
  border-radius: var(--borderRadius);
  box-sizing: border-box;
}

/* --- panel transition --- */
.panel-slide-enter-active {
  transition: opacity 0.4s cubic-bezier(0.25, 0.8, 0.25, 1);
}

.panel-slide-leave-active {
  transition: none;
}

.panel-slide-enter-from,
.panel-slide-leave-to {
  opacity: 0;
}

/* Lyrics */
.lyric-line {
  padding: 0.2em 0;
  opacity: 0.5;
  transition: opacity 0.25s ease, color 0.25s ease, font-size 0.25s ease, transform 0.25s ease;
  transform: scale(1);
  transform-origin: center;
  overflow-wrap: break-word;
  cursor: pointer;
  font-size: 1.15rem;
}

.lyric-line:hover {
  opacity: 0.85;
}

.lyric-line.active {
  opacity: 1;
  font-weight: bold;
  color: var(--primaryColor);
  font-size: 1.35rem;
  animation: lyric-line-in 0.3s ease;
}

@keyframes lyric-line-in {
  0% { transform: scale(0.98); }
  100% { transform: scale(1); }
}

.lyric-word {
  display: inline-block;
  opacity: 0.4;
  transform: scale(1);
  transition: opacity 0.35s ease, transform 0.35s cubic-bezier(0.25, 0.8, 0.25, 1);
}

.lyric-word.sung {
  opacity: 1;
}

.lyric-word.current {
  transform: scale(1.06);
}

.lyrics-mobile {
  position: relative;
  display: flex;
  flex-direction: column;
  max-height: 30vh;
  margin-top: -0.5em;
  padding-top: 0;
}

.audio-player-container--lyrics-open .lyrics-mobile {
  flex: 1 1 0%;
  min-height: 0;
  max-height: none;
  margin-top: 0;
  display: flex;
  flex-direction: column;
}

.lyrics-mobile-scrollable {
  flex: 1;
  overflow-y: auto;
  padding: 0 1em;
  text-align: center;
  color: var(--textPrimary);
}

/* Hide scrollbars in lyrics */
.lyrics-scrollable,
.lyrics-mobile-scrollable,
.lyrics-panel {
  scrollbar-width: none;
}

.lyrics-scrollable::-webkit-scrollbar,
.lyrics-mobile-scrollable::-webkit-scrollbar,
.lyrics-panel::-webkit-scrollbar {
  display: none;
}

.audio-player-container--lyrics-open .lyrics-mobile-scrollable {
  flex: 1;
  min-height: 0;
}

.lyrics-mobile-scrollable .lyric-line:first-child {
  padding-top: 0;
}

.lyrics-meta-header {
  padding: 0.2em 0 0.6em;
  margin: 0;
  opacity: 0.55;
  font-size: 0.85rem;
  font-weight: 600;
  letter-spacing: 0.02em;
  text-transform: uppercase;
  user-select: none;
}

.album-art-container {
  flex-shrink: 0;
  border-radius: 1em;
  overflow: hidden;
  box-shadow: 0 6px 20px rgb(0 0 0 / 20%);
  transition: width 0.3s ease;
  will-change: transform;
}

.audio-player-container--lyrics-open .album-art-container {
  width: 5em;
  height: 5em;
}

.album-art {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.album-art-fallback {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  height: 100%;
  border-radius: 18px;
  background: linear-gradient(115deg, var(--primaryColor), rgb(2 0 36 / 90%));
  filter: brightness(0.85);
}

.album-art-fallback i.material-symbols {
  font-size: 5rem;
  color: white;
  opacity: 0.8;
  user-select: none;
}

.album-art-container.no-artwork {
  box-shadow: 0 4px 12px rgb(0 0 0 / 15%);
  height: auto;
  aspect-ratio: 1 / 1;
}

/* Metadata */
.audio-title {
  font-size: max(1.4rem, 3.1vmin);
  font-weight: bold;
  margin-bottom: 8px;
  overflow-wrap: break-word;
}

.audio-metadata {
   text-align: center;
   color: whitesmoke;
   box-sizing: border-box;
   padding-bottom: 0;
   margin-bottom: 0;
   padding-top: 1.2em;
   overflow-wrap: break-word;
}

.audio-artist,
.audio-album,
.audio-year {
  font-size: max(1.2rem, 2.5vmin);
  opacity: 0.8;
  margin-bottom: 5px;
  overflow-wrap: break-word;
}

.filetype-badge {
  display: inline-block;
  background: var(--primaryColor);
  color: white;
  padding: 2px 8px;
  border-radius: var(--borderRadius);
  font-size: 0.8em;
  margin-left: 0.5em;
  vertical-align: middle;
}

.audio-controls-container {
  width: 100%;
  border-radius: var(--borderRadius);
  margin: -2px;
}

/* For small tablets and phones with big screen */
@media (width <= 740px) {
  .audio-player-container {
    padding: 0;
    padding-top: 1em;
  }
  .plyr.plyr--audio {
    padding: 1em;
    border-radius: 0;
  }
  .plyr--audio .plyr__controls {
    padding: 0;
    gap: 5px;
  }
  .album-art-container {
    margin-top: 1em;
    max-width: min(71vw);
  }
  .audio-player-container--lyrics-open .album-art-container {
    transition: none !important;
  }
}

@media (width <= 550px) {
  /* Hide volume buttons to made more space */
  .plyr__volume {
    display: none;
  }

  /* Time playing */
  .plyr--audio .plyr__time {
    font-size: 14px;
    margin: 0 5px;
  }
  .audio-left-column {
    padding: 0;
    margin: 0;
  }
}

/* For small screens in landscape orientation (Like a phone) */
@media (height <= 600px) and (orientation: landscape) {
  .album-art-container {
    width: min(100px, 30vh);
    height: min(100px, 30vh);
    margin: 0;
    flex-shrink: 0;
  }
}

/*********************
*** PLAYBACK TOAST ***
**********************/

.playback-toast {
  position: fixed;
  bottom: 50px;
  left: 50%;
  transform: translateX(-50%);
  background: rgb(0 0 0 / 80%);
  color: white;
  padding: 15px 25px;
  border-radius: 8px;
  font-size: 1.1rem;
  display: flex;
  align-items: center;
  gap: 10px;
  z-index: 10000;
  pointer-events: none;
  user-select: none;
  opacity: 0;
  transition: opacity 0.3s ease;
  box-shadow: 0 4px 12px rgb(0 0 0 / 30%);
}

.playback-toast.visible {
  opacity: 1;
}

.playback-toast .material-symbols {
  font-size: 24px;
  color: white;
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.status-indicator {
  display: inline-block;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  margin-left: 10px;
}

.status-on {
  background: #4caf50;
}

.status-off {
  background: #f44336;
}

</style>
