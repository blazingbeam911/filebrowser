import { describe, expect, it } from 'vitest';
import {
  DEFAULT_FRAME_SECONDS,
  createFrameTracker,
  frameOffset,
  frameStepTarget,
  medianFrameLength,
} from '@/plyr/plyrFrameTracker.js';

function fakeVideo() {
  let callback = null;
  return {
    paused: false,
    seeking: false,
    playbackRate: 1,
    requestVideoFrameCallback(fn) { callback = fn; return 1; },
    cancelVideoFrameCallback() { callback = null; },
    present(mediaTime, presentedFrames) { callback?.(0, { mediaTime, presentedFrames }); },
  };
}

describe('medianFrameLength', () => {
  it('assumes film rate with nothing measured', () => {
    expect(medianFrameLength([])).toBe(DEFAULT_FRAME_SECONDS);
  });

  it('ignores an outlier', () => {
    expect(medianFrameLength([0.04, 0.04, 0.5, 0.04, 0.04])).toBe(0.04);
  });
});

describe('frameStepTarget', () => {
  it('aims for the middle of the neighboring frame from a frame start', () => {
    expect(frameStepTarget(10, 0.04, 1, 100)).toBeCloseTo(10.06);
    expect(frameStepTarget(10, 0.04, -1, 100)).toBeCloseTo(9.98);
  });

  it('moves exactly one frame from the middle of a frame', () => {
    expect(frameStepTarget(10, 0.04, 1, 100, 'middle')).toBeCloseTo(10.04);
    expect(frameStepTarget(10, 0.04, -1, 100, 'middle')).toBeCloseTo(9.96);
  });

  it('stays close to one frame from an unknown spot in a frame', () => {
    expect(frameStepTarget(10, 0.04, 1, 100, 'unknown')).toBeCloseTo(10.042);
    expect(frameStepTarget(10, 0.04, -1, 100, 'unknown')).toBeCloseTo(9.962);
  });

  it('has nowhere to go at either end of the file', () => {
    expect(frameStepTarget(0, 0.04, -1, 100)).toBeNull();
    expect(frameStepTarget(99.98, 0.04, 1, 100)).toBeNull();
  });
});

describe('frameOffset', () => {
  it('recognizes the landing spot of a step', () => {
    expect(frameOffset(10.06, 10.06, 10.06)).toBe('middle');
  });

  it('cannot place a time that only echoes the element', () => {
    expect(frameOffset(10.51, 10.06, 10.51)).toBe('unknown');
    expect(frameOffset(10.51, null, 10.51)).toBe('unknown');
  });

  it('trusts a frame time that differs from the element', () => {
    expect(frameOffset(10.5, null, 10.52)).toBe('start');
  });
});

describe('createFrameTracker', () => {
  it('returns null without a frame callback', () => {
    expect(createFrameTracker({}, () => {})).toBeNull();
  });

  it('measures frame length and reports each frame', () => {
    const video = fakeVideo();
    const seen = [];
    const tracker = createFrameTracker(video, (t) => seen.push(t));
    video.present(1, 10);
    video.present(1.04, 11);
    video.present(1.12, 13);
    expect(seen).toEqual([1, 1.04, 1.12]);
    expect(tracker.mediaTime()).toBe(1.12);
    expect(tracker.frameSeconds()).toBeCloseTo(0.04);
  });

  it('reports a frame rate only after enough frames', () => {
    const video = fakeVideo();
    const tracker = createFrameTracker(video, () => {});
    expect(tracker.measuredFrameRate()).toBeNull();
    for (let i = 0; i < 6; i++) video.present(1 + i * 0.04, 10 + i);
    expect(tracker.measuredFrameRate()).toBeCloseTo(25);
  });

  it('does not measure while paused or at high speed', () => {
    const video = fakeVideo();
    const tracker = createFrameTracker(video, () => {});
    video.present(1, 10);
    video.paused = true;
    video.present(5, 11);
    video.paused = false;
    video.playbackRate = 4;
    video.present(5.1, 12);
    expect(tracker.frameSeconds()).toBe(DEFAULT_FRAME_SECONDS);
  });

  it('stops reporting once stopped', () => {
    const video = fakeVideo();
    const seen = [];
    const tracker = createFrameTracker(video, (t) => seen.push(t));
    video.present(1, 10);
    tracker.stop();
    video.present(1.04, 11);
    expect(seen).toEqual([1]);
  });
});
