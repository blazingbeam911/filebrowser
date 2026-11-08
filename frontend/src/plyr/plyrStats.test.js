import { describe, expect, it } from 'vitest';
import {
  buildStatsRows,
  bufferedAhead,
  containerName,
  displayedSize,
  formatBitrate,
  formatMediaTime,
} from '@/plyr/plyrStats.js';

const ranges = (...pairs) => ({
  length: pairs.length,
  start: (i) => pairs[i][0],
  end: (i) => pairs[i][1],
});

function fakeAudio(overrides = {}) {
  return {
    currentTime: 30,
    duration: 120,
    playbackRate: 1,
    volume: 0.5,
    muted: false,
    readyState: 4,
    networkState: 1,
    buffered: ranges([0, 42.5]),
    ...overrides,
  };
}

const value = (rows, label) => rows.find((r) => r.label === label)?.value;

describe('formatting', () => {
  it('formats minutes and hours', () => {
    expect(formatMediaTime(65)).toBe('1:05');
    expect(formatMediaTime(3661)).toBe('1:01:01');
    expect(formatMediaTime(NaN)).toBe('0:00');
  });

  it('formats bitrates', () => {
    expect(formatBitrate(4200000)).toBe('4.2 Mbps');
    expect(formatBitrate(192000)).toBe('192 kbps');
  });
});

describe('bufferedAhead', () => {
  it('measures from the current position', () => {
    expect(bufferedAhead({ currentTime: 30, buffered: ranges([0, 42.5]) })).toBe(12.5);
  });

  it('is null outside any buffered range', () => {
    expect(bufferedAhead({ currentTime: 50, buffered: ranges([0, 42.5]) })).toBeNull();
  });
});

describe('containerName', () => {
  it('uses a single format name as it is', () => {
    expect(containerName('flv', 'video/x-flv')).toBe('flv');
  });

  it('prefers the MIME type over a list of names', () => {
    expect(containerName('mov,mp4,m4a,3gp,3g2,mj2', 'video/mp4')).toBe('mp4');
    expect(containerName('matroska,webm', 'video/x-matroska')).toBe('matroska');
  });

  it('falls back to the MIME type with no metadata', () => {
    expect(containerName(undefined, 'audio/x-wav')).toBe('wav');
    expect(containerName(undefined, '')).toBeNull();
  });
});

describe('displayedSize', () => {
  it('allows for letterboxing and device pixels', () => {
    const media = { videoWidth: 1920, videoHeight: 1080, getBoundingClientRect: () => ({ width: 1000, height: 1000 }) };
    expect(displayedSize(media, 2)).toBe('2000×1125');
  });

  it('is null before the video has a size', () => {
    const media = { videoWidth: 0, videoHeight: 0, getBoundingClientRect: () => ({ width: 1000, height: 1000 }) };
    expect(displayedSize(media, 1)).toBeNull();
  });
});

describe('buildStatsRows', () => {
  it('shows only what applies to audio', () => {
    const rows = buildStatsRows({ media: fakeAudio(), file: { name: 'a.flac', type: 'audio/flac', size: 15000000 } });
    expect(value(rows, 'File')).toBe('a.flac');
    expect(value(rows, 'Container')).toBe('flac');
    expect(value(rows, 'Bitrate')).toBe('1.0 Mbps (average)');
    expect(value(rows, 'Position')).toBe('0:30 / 2:00');
    expect(value(rows, 'Volume')).toBe('50%');
    expect(value(rows, 'Buffered ahead')).toBe('12.5 s');
    expect(value(rows, 'Ready state')).toBe('4 (HAVE_ENOUGH_DATA)');
    for (const label of ['Video size', 'Displayed', 'Frame rate', 'Decoded frames', 'Video codec']) {
      expect(value(rows, label)).toBeUndefined();
    }
  });

  it('adds the video rows and browser counters that exist', () => {
    const media = fakeAudio({
      videoWidth: 640,
      videoHeight: 360,
      getBoundingClientRect: () => ({ width: 640, height: 360 }),
      getVideoPlaybackQuality: () => ({ totalVideoFrames: 100, droppedVideoFrames: 2 }),
      mozPaintedFrames: 98,
    });
    const rows = buildStatsRows({ media, file: { name: 'v.mp4', metadata: { videoCodec: 'h264' } }, frameRate: 29.97 });
    expect(value(rows, 'Video size')).toBe('640×360');
    expect(value(rows, 'Displayed')).toBe('640×360');
    expect(value(rows, 'Frame rate')).toBe('29.97 fps (measured)');
    expect(value(rows, 'Video codec')).toBe('h264');
    expect(value(rows, 'Decoded frames')).toBe('100');
    expect(value(rows, 'Dropped frames')).toBe('2');
    expect(value(rows, 'Firefox painted')).toBe('98');
    expect(value(rows, 'Firefox parsed')).toBeUndefined();
  });

  it('reports muted instead of a level', () => {
    expect(value(buildStatsRows({ media: fakeAudio({ muted: true }), file: {} }), 'Volume')).toBe('muted');
  });
});
