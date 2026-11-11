// fufu: client-side state of the thumbnail jobs (patch 0036). Filled from the server's list
// whenever the event stream connects (page load, reconnect after a restart), then kept current
// by "thumbnailJob" events. Read by ThumbnailJobProgress.vue and prompts/ThumbnailJobs.vue.
import { reactive } from "vue";
import * as api from "@/api/thumbnailJobs.js";
import { getters } from "@/store/index.ts";

export const thumbnailJobs = reactive({
  /** @type {Record<string, any>} id → latest status */
  byId: {},
  /** Latest measured seconds per file that needed thumbnails; 0 = unknown. */
  secondsPerFile: 0,
  /** @type {Record<string, boolean>} jobs seen queued or running in this page: their end shows in the progress box */
  watched: {},
  /** @type {Record<string, boolean>} finished jobs whose summary was dismissed */
  dismissed: {},
  /** The progress box is minimized into the status bar (remembered per device, set below). */
  minimized: false,
});

const ACTIVE = new Set(["queued", "running"]);

export function isActive(job) {
  return ACTIVE.has(job?.state);
}

/** Orders jobs as the server lists them: running, then queued by id, then finished, newest first. */
export function sortJobs(jobs) {
  const rank = (j) => (j.state === "running" ? 0 : j.state === "queued" ? 1 : 2);
  return [...jobs].sort((a, b) => {
    if (rank(a) !== rank(b)) return rank(a) - rank(b);
    if (rank(a) === 2) return String(b.finishedAt || "").localeCompare(String(a.finishedAt || ""));
    return Number(a.id) - Number(b.id);
  });
}

/** All known jobs, in list order. */
export function allJobs() {
  return sortJobs(Object.values(thumbnailJobs.byId));
}

/** Jobs for the progress box: active ones, and finished ones watched here and not dismissed. */
export function progressJobs() {
  return allJobs().filter((j) => isActive(j) || (thumbnailJobs.watched[j.id] && !thumbnailJobs.dismissed[j.id]));
}

/**
 * Merges one job's status (from an event or a response). Events carry no failure list, so a
 * list already known is kept.
 */
export function applyStatus(status) {
  if (!status || status.id === undefined) return;
  const id = String(status.id);
  const prev = thumbnailJobs.byId[id];
  const next = { ...status, id };
  if (!next.failures && prev?.failures) next.failures = prev.failures;
  thumbnailJobs.byId[id] = next;
  if (isActive(next)) thumbnailJobs.watched[id] = true;
  if (next.secondsPerGenerated > 0) thumbnailJobs.secondsPerFile = next.secondsPerGenerated;
}

/** Replaces the known jobs with the server's list (job ids restart with the server). */
export function applyList(resp) {
  const fresh = {};
  for (const job of resp?.jobs || []) {
    fresh[String(job.id)] = { ...job, id: String(job.id) };
  }
  // A job watched here keeps its watched mark only while the server still has the same job.
  for (const id of Object.keys(thumbnailJobs.watched)) {
    const before = thumbnailJobs.byId[id];
    if (!fresh[id] || (before && before.queuedAt !== fresh[id].queuedAt)) {
      delete thumbnailJobs.watched[id];
      delete thumbnailJobs.dismissed[id];
    }
  }
  thumbnailJobs.byId = fresh;
  for (const job of Object.values(fresh)) {
    if (isActive(job)) thumbnailJobs.watched[job.id] = true;
  }
  thumbnailJobs.secondsPerFile = resp?.secondsPerFile || 0;
}

/** Reloads the jobs from the server (admins only; quietly does nothing otherwise). */
export async function refreshThumbnailJobs() {
  if (!getters.isLoggedIn() || !getters.isAdmin() || getters.isShare()) return;
  try {
    applyList(await api.list());
  } catch (err) {
    console.warn("thumbnail jobs: could not load", err);
  }
}

/** Handles a "thumbnailJob" event (its message is the status as a JSON string). */
export function onThumbnailJobEvent(message) {
  try {
    applyStatus(typeof message === "string" ? JSON.parse(message) : message);
  } catch (err) {
    console.error("thumbnail jobs: bad event", err, message);
  }
}

export function dismiss(id) {
  thumbnailJobs.dismissed[String(id)] = true;
}

/** Dismisses every finished job in the progress box (the panel still lists them). */
export function dismissFinished() {
  for (const job of progressJobs()) {
    if (!isActive(job)) dismiss(job.id);
  }
}

/**
 * The one line of a minimized progress box: how many jobs are queued or running, how many
 * finished ones it holds, and the percentage done across the active jobs whose size is known.
 */
export function overallProgress(jobs) {
  const active = jobs.filter(isActive);
  let done = 0;
  let total = 0;
  for (const job of active) {
    if (job.total > 0) {
      done += Math.min(job.done || 0, job.total);
      total += job.total;
    }
  }
  return {
    active: active.length,
    finished: jobs.length - active.length,
    percent: total ? Math.floor((done * 100) / total) : 0,
    paused: active.some((j) => j.state === "running" && j.paused),
  };
}

const MINIMIZED_KEY = "fbThumbnailJobsMinimized";

/** Whether the progress box is minimized on this device. */
export function loadMinimized() {
  try {
    return localStorage.getItem(MINIMIZED_KEY) === "1";
  } catch {
    return false;
  }
}

export function saveMinimized(minimized) {
  try {
    localStorage.setItem(MINIMIZED_KEY, minimized ? "1" : "0");
  } catch {
    // storage blocked: the box just forgets
  }
}

thumbnailJobs.minimized = loadMinimized();

/** Minimizes the progress box into the status bar, or expands it again, and remembers it. */
export function setMinimized(minimized) {
  thumbnailJobs.minimized = minimized;
  saveMinimized(minimized);
}

/** Percentage done, 0–100 (100 for a finished job). */
export function percent(job) {
  if (!job) return 0;
  if (!isActive(job)) return 100;
  if (!job.total) return 0;
  return Math.min(100, Math.floor((job.done * 100) / job.total));
}

/**
 * A rough duration for people, e.g. "under a minute", "12 min", "3 h 5 min".
 * @param {number} seconds
 * @param {(key: string, params?: object) => string} t
 */
export function formatDuration(seconds, t) {
  const s = Math.max(0, Math.round(seconds || 0));
  if (s < 60) return t("thumbnailJobs.underAMinute");
  const totalMin = Math.round(s / 60);
  if (totalMin < 60) return t("thumbnailJobs.minutes", { m: totalMin });
  const h = Math.floor(totalMin / 60);
  const m = totalMin % 60;
  return t("thumbnailJobs.hoursMinutes", { h, m });
}

/** The server's message from a failed request (its body is {"status", "message"} JSON or text). */
export function errorText(err) {
  const raw = err?.message || String(err);
  try {
    const parsed = JSON.parse(raw);
    if (parsed && typeof parsed.message === "string" && parsed.message) return parsed.message;
  } catch {
    // not JSON: the text itself
  }
  return raw;
}
