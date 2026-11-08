/**
 * The rows of the player's stats overlay, from what the browser and the file's metadata report.
 * Labels are technical terms and stay in English. A row with no value is left out.
 */

const READY_STATES = ['HAVE_NOTHING', 'HAVE_METADATA', 'HAVE_CURRENT_DATA', 'HAVE_FUTURE_DATA', 'HAVE_ENOUGH_DATA'];
const NETWORK_STATES = ['EMPTY', 'IDLE', 'LOADING', 'NO_SOURCE'];
const FIREFOX_COUNTERS = [
  ['Firefox parsed', 'mozParsedFrames'],
  ['Firefox decoded', 'mozDecodedFrames'],
  ['Firefox presented', 'mozPresentedFrames'],
  ['Firefox painted', 'mozPaintedFrames'],
];

/** `m:ss`, or `h:mm:ss` from one hour up. */
export function formatMediaTime(seconds) {
  const total = Math.max(0, Math.floor(seconds || 0));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const secs = String(total % 60).padStart(2, '0');
  return hours > 0 ? `${hours}:${String(minutes).padStart(2, '0')}:${secs}` : `${minutes}:${secs}`;
}

export function formatBitrate(bitsPerSecond) {
  if (bitsPerSecond >= 1e6) return `${(bitsPerSecond / 1e6).toFixed(1)} Mbps`;
  return `${Math.round(bitsPerSecond / 1e3)} kbps`;
}

/** Seconds buffered past the current position, or null when it isn't in a buffered range. */
export function bufferedAhead(media) {
  const { buffered, currentTime } = media;
  for (let i = 0; i < (buffered?.length ?? 0); i++) {
    if (buffered.start(i) <= currentTime && currentTime <= buffered.end(i)) {
      return buffered.end(i) - currentTime;
    }
  }
  return null;
}

/** ffprobe names some formats as a list, such as `mov,mp4,m4a`; the MIME type reads better then. */
export function containerName(container, mimeType) {
  if (container && !container.includes(',')) return container;
  const subtype = (mimeType || '').split('/')[1];
  return subtype ? subtype.replace(/^x-/, '') : container || null;
}

/** The size of the picture on screen in device pixels, allowing for letterboxing. */
export function displayedSize(media, devicePixelRatio) {
  const { width, height } = media.getBoundingClientRect();
  if (!media.videoWidth || !media.videoHeight || !width || !height) return null;
  const scale = Math.min(width / media.videoWidth, height / media.videoHeight) * devicePixelRatio;
  return `${Math.round(media.videoWidth * scale)}×${Math.round(media.videoHeight * scale)}`;
}

function videoRows(media, frameRate, devicePixelRatio) {
  if (media.videoWidth === undefined) return [];
  const quality = media.getVideoPlaybackQuality?.();
  const counters = FIREFOX_COUNTERS.map(([label, key]) => [label, media[key]]);
  return [
    ['Video size', media.videoWidth ? `${media.videoWidth}×${media.videoHeight}` : null],
    ['Displayed', displayedSize(media, devicePixelRatio)],
    ['Frame rate', frameRate ? `${frameRate.toFixed(2)} fps (measured)` : null],
    ['Decoded frames', quality?.totalVideoFrames],
    ['Dropped frames', quality?.droppedVideoFrames],
    ...counters,
  ];
}

export function buildStatsRows({ media, file, frameRate = null, devicePixelRatio = 1 }) {
  const metadata = file?.metadata;
  const duration = Number.isFinite(media.duration) ? media.duration : metadata?.duration;
  const ahead = bufferedAhead(media);
  const rows = [
    ['File', file?.name],
    ['Container', containerName(metadata?.container, file?.type)],
    ['Video codec', metadata?.videoCodec],
    ['Audio codec', metadata?.audioCodec],
    ...videoRows(media, frameRate, devicePixelRatio),
    ['Bitrate', file?.size > 0 && duration > 0 ? `${formatBitrate((file.size * 8) / duration)} (average)` : null],
    ['Position', `${formatMediaTime(media.currentTime)} / ${formatMediaTime(duration)}`],
    ['Speed', `${media.playbackRate}×`],
    ['Volume', media.muted ? 'muted' : `${Math.round(media.volume * 100)}%`],
    ['Buffered ahead', ahead === null ? null : `${ahead.toFixed(1)} s`],
    ['Ready state', `${media.readyState} (${READY_STATES[media.readyState]})`],
    ['Network state', `${media.networkState} (${NETWORK_STATES[media.networkState]})`],
  ];
  return rows
    .filter(([, value]) => value !== null && value !== undefined && value !== '')
    .map(([label, value]) => ({ label, value: String(value) }));
}
