// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Unauthenticated GitHub API allows 60 requests per hour per IP.
export const starCountCacheMilliseconds = 60 * 60 * 1000;
export const failedFetchCacheMilliseconds = 10 * 60 * 1000;
export const starCountFetchTimeoutMilliseconds = 5000;

/**
 * Reads a cached star count for one repository cache key.
 * Returns undefined on a cache miss and null for a cached failure.
 */
export function readCachedStarCount(storage, cacheKey, nowMilliseconds) {
  try {
    const cached = JSON.parse(storage.getItem(cacheKey) ?? "null");
    if (!cached || typeof cached.fetchedAt !== "number") {
      return undefined;
    }
    const cacheAgeMilliseconds = nowMilliseconds - cached.fetchedAt;
    if (cached.starCount === null) {
      return cacheAgeMilliseconds >= 0 && cacheAgeMilliseconds < failedFetchCacheMilliseconds ? null : undefined;
    }
    if (isValidStarCount(cached.starCount) && cacheAgeMilliseconds >= 0 && cacheAgeMilliseconds < starCountCacheMilliseconds) {
      return cached.starCount;
    }
  } catch {
    // Storage may be blocked or hold malformed data; fetch instead.
  }
  return undefined;
}

/** Caches a fetched star count, or null to back off after a failed fetch. */
export function writeCachedStarCount(storage, cacheKey, starCount, nowMilliseconds) {
  try {
    storage.setItem(cacheKey, JSON.stringify({ starCount, fetchedAt: nowMilliseconds }));
  } catch {
    // Storage may be blocked; the next visit fetches again.
  }
}

// Another page on this origin may have cached a fresh count meanwhile; keep it.
export function writeFailedFetchUnlessCountCached(storage, cacheKey, nowMilliseconds) {
  if (typeof readCachedStarCount(storage, cacheKey, nowMilliseconds) !== "number") {
    writeCachedStarCount(storage, cacheKey, null, nowMilliseconds);
  }
}

/** Reports whether a value is a usable non-negative integer star count. */
export function isValidStarCount(value) {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
}

/** Formats a star count compactly, such as 999 or 1.2k. */
export function formatStarCount(starCount) {
  return starCount >= 1000 ? `${Math.round(starCount / 100) / 10}k` : String(starCount);
}

/** Builds a link label such as "GitHub, 12 stars", or undefined without a count. */
export function formatStarredLinkLabel(linkName, starCount) {
  if (starCount === null) {
    return undefined;
  }
  return `${linkName}, ${formatStarCount(starCount)} ${starCount === 1 ? "star" : "stars"}`;
}
