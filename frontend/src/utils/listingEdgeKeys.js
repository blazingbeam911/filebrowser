import { isTextEntryTarget } from "@/plyr/plyrKeyTargets.js";

/**
 * Home / End in the listing: "first" or "last" item, or null when the key isn't one of them, has
 * Shift, Alt or Meta held, or goes to a text field (where it moves the caret). Ctrl+Home and
 * Ctrl+End count as Home and End.
 *
 * The listing handles these itself: left to the browser, they scrolled the listing only when
 * the last click had landed somewhere the browser treats as inside its scroll area, which clicks
 * on empty space (rectangle selection cancels their default) and on rows often aren't.
 */
export function listingEdgeKey(event) {
  if (!event || event.shiftKey || event.altKey || event.metaKey) return null;
  if (event.key !== "Home" && event.key !== "End") return null;
  if (isTextEntryTarget(event.target)) return null;
  return event.key === "Home" ? "first" : "last";
}
