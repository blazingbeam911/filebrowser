/**
 * Whether a listing shows the Duration column.
 *
 * Durations arrive a moment after the listing, from a separate media metadata request
 * that Files.vue makes when the folder holds audio or video files. Showing the column only
 * once the first duration had arrived made it pop in after the listing was on screen. It is
 * shown from the start instead, as soon as the folder has an audio or video file (the same
 * rule that starts the metadata request), with its cells filled in when the durations
 * arrive.
 *
 * @param {Array<{type?: string, metadata?: {duration?: number}}>|null|undefined} items
 * @returns {boolean}
 */
export function listingShowsDuration(items) {
  if (!Array.isArray(items)) return false;
  return items.some(
    (item) =>
      item &&
      item.type !== "directory" &&
      (Boolean(item.metadata?.duration) || item.type?.startsWith("audio") || item.type?.startsWith("video"))
  );
}
