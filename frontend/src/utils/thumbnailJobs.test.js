import { beforeEach, describe, expect, it } from "vitest";
import {
  applyList,
  applyStatus,
  dismiss,
  dismissFinished,
  errorText,
  formatDuration,
  loadMinimized,
  overallProgress,
  percent,
  progressJobs,
  saveMinimized,
  setMinimized,
  sortJobs,
  thumbnailJobs,
} from "@/utils/thumbnailJobs.js";

const t = (key, params) => `${key}${params ? JSON.stringify(params) : ""}`;

function job(id, state, extra = {}) {
  return { id, source: "s", path: `/f${id}`, state, total: 10, done: 0, generated: 0, cached: 0, skipped: 0, failed: 0, queuedAt: `q${id}`, ...extra };
}

beforeEach(() => {
  thumbnailJobs.byId = {};
  thumbnailJobs.watched = {};
  thumbnailJobs.dismissed = {};
  thumbnailJobs.secondsPerFile = 0;
});

describe("thumbnail job state", () => {
  it("shows active jobs, then a watched job's summary until dismissed", () => {
    applyStatus(job("1", "running", { done: 3 }));
    applyStatus(job("2", "queued"));
    expect(progressJobs().map((j) => j.id)).toEqual(["1", "2"]);
    applyStatus(job("1", "done", { done: 10, finishedAt: "2026-01-01T00:00:01Z" }));
    expect(progressJobs().map((j) => j.id)).toEqual(["2", "1"]);
    dismiss("1");
    expect(progressJobs().map((j) => j.id)).toEqual(["2"]);
  });

  it("doesn't show finished jobs it never saw running (e.g. after a page reload)", () => {
    applyList({ jobs: [job("5", "done"), job("6", "running")], secondsPerFile: 0.2 });
    expect(progressJobs().map((j) => j.id)).toEqual(["6"]);
    expect(thumbnailJobs.secondsPerFile).toBe(0.2);
  });

  it("keeps a known failure list when an event (which has none) updates the job", () => {
    applyList({ jobs: [job("1", "running", { failed: 1, failures: [{ path: "/a", reason: "x" }] })] });
    applyStatus(job("1", "running", { failed: 1, done: 4 }));
    expect(thumbnailJobs.byId["1"].failures).toHaveLength(1);
    expect(thumbnailJobs.byId["1"].done).toBe(4);
  });

  it("forgets jobs the server no longer has, and a reused id after a restart", () => {
    applyStatus(job("1", "running"));
    applyList({ jobs: [] });
    expect(progressJobs()).toEqual([]);
    applyStatus(job("1", "running"));
    applyStatus(job("1", "done"));
    // The server restarted: id 1 is now a different job, finished before this page saw it.
    applyList({ jobs: [job("1", "done", { queuedAt: "other" })] });
    expect(progressJobs()).toEqual([]);
  });

  it("sorts running, queued by id, then finished newest first", () => {
    const sorted = sortJobs([
      job("3", "done", { finishedAt: "2026-01-01T00:00:01Z" }),
      job("10", "queued"),
      job("4", "cancelled", { finishedAt: "2026-01-01T00:00:05Z" }),
      job("9", "queued"),
      job("8", "running"),
    ]);
    expect(sorted.map((j) => j.id)).toEqual(["8", "9", "10", "4", "3"]);
  });
});

describe("formatting", () => {
  it("percent", () => {
    expect(percent(job("1", "running", { done: 3, total: 7 }))).toBe(42);
    expect(percent(job("1", "running", { total: 0 }))).toBe(0);
    expect(percent(job("1", "done"))).toBe(100);
  });

  it("durations", () => {
    expect(formatDuration(30, t)).toBe("thumbnailJobs.underAMinute");
    expect(formatDuration(600, t)).toBe('thumbnailJobs.minutes{"m":10}');
    expect(formatDuration(3 * 3600 + 5 * 60, t)).toBe('thumbnailJobs.hoursMinutes{"h":3,"m":5}');
  });

  it("server error messages", () => {
    expect(errorText(new Error('{"status":404,"message":"folder /x is not in the index"}'))).toBe("folder /x is not in the index");
    expect(errorText(new Error("plain text"))).toBe("plain text");
  });
});

describe("progress box", () => {
  it("clears every finished job from the box at once, and only those", () => {
    applyStatus(job("1", "running", { done: 3 }));
    applyStatus(job("2", "running"));
    applyStatus(job("3", "running"));
    applyStatus(job("4", "queued"));
    applyStatus(job("1", "done", { done: 10, finishedAt: "2026-01-01T00:00:01Z" }));
    applyStatus(job("2", "cancelled", { finishedAt: "2026-01-01T00:00:02Z" }));
    applyStatus(job("3", "failed", { finishedAt: "2026-01-01T00:00:03Z" }));
    dismissFinished();
    expect(progressJobs().map((j) => j.id)).toEqual(["4"]);
    expect(Object.keys(thumbnailJobs.byId).sort((a, b) => Number(a) - Number(b))).toEqual(["1", "2", "3", "4"]);
  });

  it("sums progress across the active jobs whose size is known", () => {
    const jobs = [
      job("1", "running", { done: 30, total: 100 }),
      job("2", "running", { done: 10, total: 100 }),
      job("3", "queued", { total: 0 }),
      job("4", "done", { done: 10 }),
    ];
    expect(overallProgress(jobs)).toEqual({ active: 3, finished: 1, percent: 20, paused: false });
    expect(overallProgress([job("5", "queued", { total: 0 })])).toEqual({ active: 1, finished: 0, percent: 0, paused: false });
    expect(overallProgress([])).toEqual({ active: 0, finished: 0, percent: 0, paused: false });
  });

  it("reports a running job paused for browsing", () => {
    expect(overallProgress([job("1", "running", { done: 1, total: 4, paused: true }), job("2", "queued")]).paused).toBe(true);
    // a finished job's last status may still say paused: it doesn't count
    expect(overallProgress([job("3", "done", { paused: true })]).paused).toBe(false);
  });

  it("shares the minimized state and remembers it", () => {
    setMinimized(true);
    expect(thumbnailJobs.minimized).toBe(true);
    expect(loadMinimized()).toBe(true);
    setMinimized(false);
    expect(thumbnailJobs.minimized).toBe(false);
    expect(loadMinimized()).toBe(false);
  });

  it("remembers the minimized box, and copes without storage", () => {
    saveMinimized(true);
    expect(loadMinimized()).toBe(true);
    saveMinimized(false);
    expect(loadMinimized()).toBe(false);
    const original = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
    Object.defineProperty(globalThis, "localStorage", { configurable: true, get() { throw new Error("blocked"); } });
    try {
      expect(loadMinimized()).toBe(false);
      expect(() => saveMinimized(true)).not.toThrow();
    } finally {
      Object.defineProperty(globalThis, "localStorage", original);
    }
  });
});
