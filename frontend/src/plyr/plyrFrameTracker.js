/**
 * Which frame a video is showing and how long its frames last.
 *
 * Browsers can only seek to a time, so stepping one frame needs the length of a frame, and none of
 * them report it. It is measured from the frames presented during normal playback.
 */

export const DEFAULT_FRAME_SECONDS = 1 / 24;
const MAX_SAMPLES = 31;
const MAX_FRAME_SECONDS = 1;

/** The median of the measured frame lengths: one late frame or seek shouldn't move it. */
export function medianFrameLength(gaps) {
  if (gaps.length === 0) return DEFAULT_FRAME_SECONDS;
  const sorted = [...gaps].sort((a, b) => a - b);
  return sorted[Math.floor(sorted.length / 2)];
}

/**
 * How far to move, in frames, to land inside the neighboring frame. A frame's real start is known
 * during playback; a step of our own lands mid-frame. After any other seek some browsers report only
 * the requested time, which can be anywhere in the frame, so that case stays close to one frame.
 */
const STEP_FRAMES = {
  start: { forward: 1.5, back: 0.5 },
  middle: { forward: 1, back: 1 },
  unknown: { forward: 1.05, back: 0.95 },
};

/** Where `mediaTime` sits within its frame, given the last step's target and the element's time. */
export function frameOffset(mediaTime, lastStepTarget, currentTime) {
  const same = (a, b) => a !== null && Math.abs(a - b) < 0.0005;
  if (same(lastStepTarget, mediaTime)) return 'middle';
  return same(currentTime, mediaTime) ? 'unknown' : 'start';
}

/** The time to seek to for a step of one frame, or null at either end of the file. */
export function frameStepTarget(mediaTime, frameSeconds, direction, duration, offset = 'start') {
  const frames = STEP_FRAMES[offset];
  const target = direction > 0
    ? mediaTime + frameSeconds * frames.forward
    : mediaTime - frameSeconds * frames.back;
  if (target < 0 || target >= duration) return null;
  return target;
}

/**
 * Follows the presented frames of `video`, calling `onFrame(mediaTime)` for each one. Returns null
 * when the browser has no frame callback.
 */
export function createFrameTracker(video, onFrame) {
  if (typeof video.requestVideoFrameCallback !== 'function') return null;
  const gaps = [];
  let previous = null;
  let currentTime = null;
  let handle = null;
  let stopped = false;

  const measure = (metadata) => {
    // Dropped frames at high speed would read as long frames.
    if (previous === null || video.paused || video.seeking || video.playbackRate > 1) return;
    const frames = metadata.presentedFrames - previous.presentedFrames;
    if (frames <= 0) return;
    const gap = (metadata.mediaTime - previous.mediaTime) / frames;
    if (gap <= 0 || gap > MAX_FRAME_SECONDS) return;
    gaps.push(gap);
    if (gaps.length > MAX_SAMPLES) gaps.shift();
  };

  const onVideoFrame = (_now, metadata) => {
    if (stopped) return;
    measure(metadata);
    previous = metadata;
    currentTime = metadata.mediaTime;
    handle = video.requestVideoFrameCallback(onVideoFrame);
    onFrame(metadata.mediaTime);
  };
  handle = video.requestVideoFrameCallback(onVideoFrame);

  return {
    /** The time of the frame on screen, or null before the first frame. */
    mediaTime: () => currentTime,
    frameSeconds: () => medianFrameLength(gaps),
    stop() {
      stopped = true;
      if (handle !== null) video.cancelVideoFrameCallback?.(handle);
    },
  };
}
