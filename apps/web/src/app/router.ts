import { useSyncExternalStore } from "react";

export interface Location {
  readonly path: string;
  readonly search: string;
  readonly hash: string;
}

export function readLocation(): Location {
  const raw = window.location.hash.replace(/^#/, "");
  if (!raw || raw === "/") return { path: "/overview", search: "", hash: "" };
  const queryIndex = raw.indexOf("?");
  const path = queryIndex >= 0 ? raw.slice(0, queryIndex) : raw;
  const search = queryIndex >= 0 ? raw.slice(queryIndex) : "";
  const normalised = path.startsWith("/") ? path : `/${path}`;
  return { path: normalised.length > 1 ? normalised.replace(/\/+$/, "") : normalised, search, hash: "" };
}

function subscribe(callback: () => void): () => void {
  window.addEventListener("hashchange", callback);
  window.addEventListener("popstate", callback);
  return () => {
    window.removeEventListener("hashchange", callback);
    window.removeEventListener("popstate", callback);
  };
}

let cached = readLocation();
function getSnapshot(): Location {
  const next = readLocation();
  if (next.path === cached.path && next.search === cached.search) return cached;
  cached = next;
  return cached;
}

export function useLocation(): Location {
  return useSyncExternalStore(subscribe, getSnapshot, () => cached);
}

export function navigate(to: string, options: { replace?: boolean } = {}): void {
  const target = to.startsWith("#") ? to : `#${to.startsWith("/") ? to : `/${to}`}`;
  if (options.replace) {
    window.history.replaceState(null, "", target);
    window.dispatchEvent(new HashChangeEvent("hashchange"));
    return;
  }
  if (window.location.hash === target) return;
  window.location.hash = target;
}

export function buildPath(path: string, query: Record<string, string | number | boolean | null | undefined>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value === null || value === undefined || value === "") continue;
    params.set(key, String(value));
  }
  const search = params.toString();
  return search ? `${path}?${search}` : path;
}

export interface Match {
  readonly params: Readonly<Record<string, string>>;
}

export function matchPath(pattern: string, path: string): Match | null {
  const patternParts = pattern.split("/").filter(Boolean);
  const pathParts = path.split("/").filter(Boolean);
  if (patternParts.length !== pathParts.length) return null;
  const params: Record<string, string> = {};
  for (let index = 0; index < patternParts.length; index += 1) {
    const expected = patternParts[index];
    const actual = pathParts[index];
    if (expected.startsWith(":")) {
      try {
        params[expected.slice(1)] = decodeURIComponent(actual);
      } catch {
        return null;
      }
    } else if (expected !== actual) {
      return null;
    }
  }
  return { params };
}
