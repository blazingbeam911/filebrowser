// fufu: thumbnail jobs (backend: internal/web/thumbjob.go). Admins and users with API access.
import { getApiPath } from "@/utils/url.js";
import { fetchJSON } from "./utils.ts";

/**
 * Previewable files under a folder, for the start dialog.
 * @param {string} source
 * @param {string} path
 * @returns {Promise<{count: number, sizes: string[], secondsPerFile: number}>}
 */
export function count(source, path) {
  return fetchJSON(getApiPath("thumbnails/jobs/count", { source, path }));
}

/**
 * Queues one job per folder.
 * @param {string} source
 * @param {string[]} paths
 * @returns {Promise<{jobs: object[], secondsPerFile: number}>}
 */
export function start(source, paths) {
  return fetchJSON(getApiPath("thumbnails/jobs"), {
    method: "POST",
    body: JSON.stringify({ source, paths }),
  });
}

/**
 * Running, queued and finished jobs.
 * @returns {Promise<{jobs: object[], secondsPerFile: number}>}
 */
export function list() {
  return fetchJSON(getApiPath("thumbnails/jobs"));
}

/**
 * Cancels a job: a queued one is removed, a running one stops after its current file.
 * @param {string} id
 */
export function cancel(id) {
  return fetchJSON(getApiPath(`thumbnails/jobs/${encodeURIComponent(id)}`), { method: "DELETE" });
}
