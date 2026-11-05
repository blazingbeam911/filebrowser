import { describe, expect, it } from 'vitest';
import { sortedItems } from './sort.ts';

describe('testSort', () => {

  it('sort items by name correctly', () => {
    const input = [
      { name: "zebra" },
      { name: "10 something.txt" },
      { name: "1 something.txt" },
      { name: "2 something.txt" },
      { name: "Apple" },
      { name: "bee 20" },
      { name: "cave 10.txt" },
      { name: "cave 1.txt" },
      { name: "cave 2.txt" },
      { name: "2" },
      { name: "bee 2" },
    ]
    const expected = [
      { name: "1 something.txt" },
      { name: "2" },
      { name: "2 something.txt" },
      { name: "10 something.txt" },
      { name: "Apple" },
      { name: "bee 2" },
      { name: "bee 20" },
      { name: "cave 1.txt" },
      { name: "cave 2.txt" },
      { name: "cave 10.txt" },
      { name: "zebra" }
    ]
    expect(sortedItems(input, "name")).toEqual(expected);
  });

  it('sort items with extensions by name correctly', () => {
    const input = [
      { name: "zebra.txt" },
      { name: "1.txt" },
      { name: "10.txt" },
      { name: "Apple.txt" },
      { name: "2.txt" },
      { name: "0" }
    ]
    const expected = [
      { name: "0" },
      { name: "1.txt" },
      { name: "2.txt" },
      { name: "10.txt" },
      { name: "Apple.txt" },
      { name: "zebra.txt" }
    ]
    expect(sortedItems(input, "name")).toEqual(expected);
  });

  it('sort items by size correctly', () => {
    const input = [
      { size: "10" },
      { size: "0" },
      { size: "5000" },
    ]
    const expected = [
      { size: "0" },
      { size: "10" },
      { size: "5000" }
    ]
    expect(sortedItems(input, "size")).toEqual(expected);
  });

  it('sort items by date correctly', () => {
    const now = new Date();
    const tenMinutesAgo = new Date(now.getTime() - 10 * 60 * 1000);
    const tenMinutesFromNow = new Date(now.getTime() + 10 * 60 * 1000);

    const input = [
      { date: now },
      { date: tenMinutesAgo },
      { date: tenMinutesFromNow },
    ]
    const expected = [
      { date: tenMinutesAgo },
      { date: now },
      { date: tenMinutesFromNow }
    ]
    expect(sortedItems(input, "date")).toEqual(expected);
  });

  it('sort items by duration correctly (ascending)', () => {
    const input = [
      { name: "video3.mp4", metadata: { duration: 300.5 } },
      { name: "video1.mp4", metadata: { duration: 120.0 } },
      { name: "video2.mp4", metadata: { duration: 240.75 } },
      { name: "video4.mp4", metadata: { duration: 60.25 } },
      { name: "audio.mp3", metadata: { duration: 180.5 } },
    ]
    const expected = [
      { name: "video4.mp4", metadata: { duration: 60.25 } },
      { name: "video1.mp4", metadata: { duration: 120.0 } },
      { name: "audio.mp3", metadata: { duration: 180.5 } },
      { name: "video2.mp4", metadata: { duration: 240.75 } },
      { name: "video3.mp4", metadata: { duration: 300.5 } },
    ]
    expect(sortedItems(input, "duration", true)).toEqual(expected);
  });

  it('sort items by duration correctly (descending)', () => {
    const input = [
      { name: "video3.mp4", metadata: { duration: 300.5 } },
      { name: "video1.mp4", metadata: { duration: 120.0 } },
      { name: "video2.mp4", metadata: { duration: 240.75 } },
      { name: "video4.mp4", metadata: { duration: 60.25 } },
      { name: "audio.mp3", metadata: { duration: 180.5 } },
    ]
    const expected = [
      { name: "video3.mp4", metadata: { duration: 300.5 } },
      { name: "video2.mp4", metadata: { duration: 240.75 } },
      { name: "audio.mp3", metadata: { duration: 180.5 } },
      { name: "video1.mp4", metadata: { duration: 120.0 } },
      { name: "video4.mp4", metadata: { duration: 60.25 } },
    ]
    expect(sortedItems(input, "duration", false)).toEqual(expected);
  });

  it('sort items by duration with missing metadata', () => {
    const input = [
      { name: "video2.mp4", metadata: { duration: 240.75 } },
      { name: "image.jpg" }, // no metadata
      { name: "video1.mp4", metadata: { duration: 120.0 } },
      { name: "document.pdf", metadata: {} }, // metadata but no duration
    ]
    const result = sortedItems(input, "duration", true);

    // Items with no duration (treated as 0) should come first
    expect(result[0].metadata?.duration ?? 0).toBe(0);
    expect(result[1].metadata?.duration ?? 0).toBe(0);
    // Then items with actual duration values in ascending order
    expect(result[2]).toEqual({ name: "video1.mp4", metadata: { duration: 120.0 } });
    expect(result[3]).toEqual({ name: "video2.mp4", metadata: { duration: 240.75 } });
  });

  it('keeps pinned items at the top while preserving sort order among them', () => {
    const input = [
      { name: "beta.txt", path: "/beta.txt" },
      { name: "delta.txt", path: "/delta.txt", pinned: true },
      { name: "alpha.txt", path: "/alpha.txt", pinned: true },
      { name: "gamma.txt", path: "/gamma.txt" },
    ];
    const expected = [
      { name: "alpha.txt", path: "/alpha.txt", pinned: true },
      { name: "delta.txt", path: "/delta.txt", pinned: true },
      { name: "beta.txt", path: "/beta.txt" },
      { name: "gamma.txt", path: "/gamma.txt" },
    ];

    expect(sortedItems(input, "name", true)).toEqual(expected);
  });

  it('sort items by date added, missing dates sort as oldest', () => {
    const input = [
      { name: "b", created: "2026-09-24T08:59:00Z" },
      { name: "none" },
      { name: "a", created: "2026-09-22T22:09:56Z" },
    ];
    expect(sortedItems([...input], "created", true).map((i) => i.name)).toEqual(["none", "a", "b"]);
    expect(sortedItems([...input], "created", false).map((i) => i.name)).toEqual(["b", "a", "none"]);
  });

  it('sort items by kind, then name', () => {
    const input = [
      { name: "b.mp4", type: "video/mp4" },
      { name: "a.zip", type: "application/zip" },
      { name: "a.mp4", type: "video/mp4" },
      { name: "song.mp3", type: "audio/mpeg" },
    ];
    expect(sortedItems([...input], "kind", true).map((i) => i.name)).toEqual(["a.zip", "song.mp3", "a.mp4", "b.mp4"]);
    expect(sortedItems([...input], "kind", false).map((i) => i.name)).toEqual(["a.mp4", "b.mp4", "song.mp3", "a.zip"]);
  });


  // fufu: name breaks ties, always A→Z.
  it('orders equal sizes by name A→Z in both directions', () => {
    const input = [
      { name: "c.png", size: 10 }, { name: "a.png", size: 10 }, { name: "big", size: 99 }, { name: "B.png", size: 10 },
    ];
    expect(sortedItems([...input], "size", true).map((i) => i.name)).toEqual(["a.png", "B.png", "c.png", "big"]);
    expect(sortedItems([...input], "size", false).map((i) => i.name)).toEqual(["big", "a.png", "B.png", "c.png"]);
  });

  it('orders equal modified times by name A→Z in both directions', () => {
    const input = [
      { name: "z", modified: "2026-01-02T00:00:00Z" }, { name: "y", modified: "2026-01-01T00:00:00Z" },
      { name: "x", modified: "2026-01-02T00:00:00Z" },
    ];
    expect(sortedItems([...input], "modified", true).map((i) => i.name)).toEqual(["y", "x", "z"]);
    expect(sortedItems([...input], "modified", false).map((i) => i.name)).toEqual(["x", "z", "y"]);
  });

  it('orders equal creation times by name A→Z in both directions', () => {
    const input = [
      { name: "b", created: "2026-01-01T00:00:00Z" }, { name: "a", created: "2026-01-01T00:00:00Z" },
      { name: "c", created: "2025-01-01T00:00:00Z" },
    ];
    expect(sortedItems([...input], "created", true).map((i) => i.name)).toEqual(["c", "a", "b"]);
    expect(sortedItems([...input], "created", false).map((i) => i.name)).toEqual(["a", "b", "c"]);
  });

  it('orders equal and missing durations by name A→Z in both directions', () => {
    const input = [
      { name: "z.jpg" }, { name: "clip.mp4", metadata: { duration: 5 } }, { name: "a.pdf", metadata: {} },
      { name: "m.mp4", metadata: { duration: 5 } },
    ];
    expect(sortedItems([...input], "duration", true).map((i) => i.name)).toEqual(["a.pdf", "z.jpg", "clip.mp4", "m.mp4"]);
    expect(sortedItems([...input], "duration", false).map((i) => i.name)).toEqual(["clip.mp4", "m.mp4", "a.pdf", "z.jpg"]);
  });

  it('keeps Type ties in name order A→Z in both directions', () => {
    const input = [{ name: "b.mp4", type: "video/mp4" }, { name: "a.mp4", type: "video/mp4" }];
    expect(sortedItems([...input], "kind", true).map((i) => i.name)).toEqual(["a.mp4", "b.mp4"]);
    expect(sortedItems([...input], "kind", false).map((i) => i.name)).toEqual(["a.mp4", "b.mp4"]);
  });
});
