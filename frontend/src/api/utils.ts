import i18n from "@/i18n/index.ts";
import { state } from "@/store/index.ts";
import type { FileListItem } from "@/store/types.ts";

const defaultRequestTimeoutMs = 5000;

export class HttpError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}

export type FetchOptions = Omit<RequestInit, "headers"> & {
  headers?: Record<string, string>;
};

export type RawListingItem = Partial<FileListItem> & { name: string };

export interface ListingResponse {
  type?: string;
  source?: string;
  path?: string;
  pinnedItems?: string[];
  folders?: RawListingItem[];
  files?: RawListingItem[];
  items?: RawListingItem[];
}

export function requestTimeoutSignal(ms = defaultRequestTimeoutMs): AbortSignal {
  if (typeof AbortSignal !== "undefined" && typeof AbortSignal.timeout === "function") {
    return AbortSignal.timeout(ms);
  }
  const controller = new AbortController();
  setTimeout(() => {
    controller.abort(new DOMException("The operation timed out.", "TimeoutError"));
  }, ms);
  return controller.signal;
}

export async function fetchURL(
  url: string,
  opts?: FetchOptions,
  _auth = true,
): Promise<Response> {
  opts = opts || {};
  opts.headers = opts.headers || {};

  const { headers, ...rest } = opts;

  let res: Response;
  try {
    res = await fetch(url, {
      credentials: 'same-origin', // Ensure cookies are sent with all API requests
      headers: {
        "sessionId": state.sessionId,
        ...headers,
      },
      ...rest,
    });
  } catch (e) {
    // The thrown value can be a DOMException (timeout/abort), a TypeError (network) or anything else.
    const err = e as { name?: string; message?: string } | null;
    let message = err?.message;
    if (err?.name === "TimeoutError" || err?.name === "AbortError") {
      message = i18n.global.t("errors.requestTimedOut");
    } else if (e instanceof TypeError && e.message === "Failed to fetch") {
      message = i18n.global.t("errors.failedToConnectToServer");
    }
    const error = new Error(message);
    error.name = err?.name || error.name;
    throw error;
  }

  // Session JWT renew is handled by utils/auth session keep-alive (not per-request headers).
  if (res.status < 200 || res.status > 299) {
    throw new HttpError(readableErrorBody(await res.text(), res.status, res.headers.get("Content-Type")), res.status);
  }
  return res;
}

// An error answered with an HTML page, typically by a reverse proxy in front of the server
// (e.g. 502 or 504), becomes a short message naming the page's title instead of its markup.
// Other error bodies (the server's own messages) are returned unchanged.
export function readableErrorBody(body: string, status: number, contentType: string | null): string {
  const isHtml = (contentType || "").toLowerCase().includes("text/html") || /^\s*<(!doctype|html)/i.test(body);
  if (!isHtml) {
    return body;
  }
  const title = body.match(/<title>([^<]*)<\/title>/i)?.[1]?.trim() || `HTTP ${status}`;
  if (status === 504 || status === 408) {
    return i18n.global.t("errors.proxyTimeout", { error: title });
  }
  return i18n.global.t("errors.errorPage", { error: title });
}

export async function fetchJSON<T = unknown>(url: string, opts?: FetchOptions): Promise<T> {
  opts = opts || {};
  if (opts.body && !opts.headers?.["Content-Type"] && !opts.headers?.["content-type"]) {
    opts.headers = {
      "Content-Type": "application/json",
      ...opts.headers,
    };
  }
  const res = await fetchURL(url, opts);
  if (res.status < 300) {
    return res.json() as Promise<T>;
  } else {
    throw new Error(String(res.status));
  }
}

export function adjustedData<T extends ListingResponse>(data: T): T {
  const listing: ListingResponse = data;
  if (listing.type === "directory") {
    const pinnedNames = new Set(listing.pinnedItems || []);
    // Combine folders and files into items
    const combined = [...(listing.folders || []), ...(listing.files || [])];
    listing.items = combined.map((item) => {
      item.source = listing.source
      if (item.isShared === undefined) {
        item.isShared = false;
      }
      item.pinned = pinnedNames.has(item.name);
      if (listing.path === "/") {
        if (item.type === "directory") {
          item.path = `/${item.name}/`
        } else {
          item.path = `/${item.name}`
        }
      } else {
        if (item.type === "directory") {
          item.path = `${listing.path}${item.name}/`
        } else {
          item.path = `${listing.path}${item.name}`
        }
      }
      return item;
    });
    delete listing.pinnedItems;
  }
  if (listing.files) {
    listing.files = []
  }
  if (listing.folders) {
    listing.folders = []
  }
  return data;
}
