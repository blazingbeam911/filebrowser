/**
 * A seek into buffered video finishes within a few frames. Dimming the picture for that long reads
 * as a black flash, so the indicator only appears once loading has lasted this long.
 */
const SHOW_DELAY_MS = 300;

/**
 * Show a loading indicator while the native video element is fetching or buffering.
 */
export function enablePlyrVideoLoadingIndicator(player, onLoadingChange) {
  if (!player?.media || typeof onLoadingChange !== 'function') {
    return () => {};
  }

  const media = player.media;
  let waiting = false;
  let seeking = false;
  let loading = false;
  /** True after play is requested; cleared when paused and not loading/buffering. */
  let playbackExpected = false;
  let shown = false;
  let showTimer = null;

  const cancelShow = () => {
    if (showTimer !== null) clearTimeout(showTimer);
    showTimer = null;
  };

  const isLoadingOrBuffering = () => (
    waiting
    || seeking
    || loading
    || (
      media.networkState === HTMLMediaElement.NETWORK_LOADING
      && media.readyState < HTMLMediaElement.HAVE_FUTURE_DATA
    )
  );

  const sync = () => {
    const hasSource = Boolean(media.currentSrc || media.src);
    const show = playbackExpected && hasSource && isLoadingOrBuffering();
    if (!show) {
      cancelShow();
      shown = false;
      onLoadingChange(false);
      return;
    }
    if (shown || showTimer !== null) return;
    showTimer = setTimeout(() => {
      showTimer = null;
      shown = true;
      onLoadingChange(true);
    }, SHOW_DELAY_MS);
  };

  const onPlay = () => {
    playbackExpected = true;
    sync();
  };

  const onPause = () => {
    const stillLoadingForPlayback = media.readyState < HTMLMediaElement.HAVE_FUTURE_DATA
      || isLoadingOrBuffering();
    if (!stillLoadingForPlayback) {
      playbackExpected = false;
    }
    sync();
  };

  const onWaiting = () => {
    waiting = true;
    sync();
  };
  const onPlaying = () => {
    waiting = false;
    loading = false;
    sync();
  };
  const onLoadStart = () => {
    loading = true;
    sync();
  };
  const onCanPlay = () => {
    loading = false;
    sync();
  };
  const onSeeking = () => {
    seeking = true;
    sync();
  };
  const onSeeked = () => {
    seeking = false;
    sync();
  };
  const onError = () => {
    waiting = false;
    seeking = false;
    loading = false;
    playbackExpected = false;
    sync();
  };
  const onEmptied = () => {
    waiting = false;
    seeking = false;
    loading = false;
    playbackExpected = false;
    sync();
  };

  const handlers = {
    play: onPlay,
    pause: onPause,
    loadstart: onLoadStart,
    waiting: onWaiting,
    playing: onPlaying,
    canplay: onCanPlay,
    seeking: onSeeking,
    seeked: onSeeked,
    error: onError,
    emptied: onEmptied,
  };

  for (const [evt, fn] of Object.entries(handlers)) {
    player.on(evt, fn);
  }

  return Object.assign(() => {
    for (const [evt, fn] of Object.entries(handlers)) {
      player.off(evt, fn);
    }
    cancelShow();
    onLoadingChange(false);
  }, {
    expectPlayback: () => {
      playbackExpected = true;
      sync();
    },
  });
}
