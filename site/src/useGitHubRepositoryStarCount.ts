// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { useEffect, useState } from "react";

import {
  isValidStarCount,
  readCachedStarCount,
  starCountFetchTimeoutMilliseconds,
  writeCachedStarCount,
  writeFailedFetchUnlessCountCached,
  type StarCountStorage,
} from "./repositoryStarCount.mjs";

/**
 * Returns a repository's GitHub star count, cached in localStorage under cacheKey.
 * Returns null while loading and when GitHub is unreachable or rate limited.
 */
export function useGitHubRepositoryStarCount(repositoryApiUrl: string, cacheKey: string): number | null {
  const [starCount, setStarCount] = useState<number | null>(null);

  useEffect(() => {
    const storage = browserLocalStorage();
    const cachedStarCount = readCachedStarCount(storage, cacheKey, Date.now());
    if (cachedStarCount !== undefined) {
      setStarCount(cachedStarCount);
      return;
    }
    const controller = new AbortController();
    const fetchTimeout = window.setTimeout(() => controller.abort(), starCountFetchTimeoutMilliseconds);
    fetch(repositoryApiUrl, { signal: controller.signal, headers: { Accept: "application/vnd.github+json" } })
      .then((response) => (response.ok ? response.json() : null))
      .then((repository: unknown) => {
        const fetchedStarCount = (repository as { stargazers_count?: unknown } | null)?.stargazers_count;
        if (isValidStarCount(fetchedStarCount)) {
          setStarCount(fetchedStarCount);
          writeCachedStarCount(storage, cacheKey, fetchedStarCount, Date.now());
        } else {
          writeFailedFetchUnlessCountCached(storage, cacheKey, Date.now());
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          writeFailedFetchUnlessCountCached(storage, cacheKey, Date.now());
        }
      })
      .finally(() => window.clearTimeout(fetchTimeout));
    return () => {
      window.clearTimeout(fetchTimeout);
      controller.abort();
    };
  }, [repositoryApiUrl, cacheKey]);

  return starCount;
}

const unavailableStorage: StarCountStorage = {
  getItem: () => null,
  setItem: () => undefined,
};

// Reading window.localStorage throws when site data is blocked.
function browserLocalStorage(): StarCountStorage {
  try {
    return window.localStorage;
  } catch {
    return unavailableStorage;
  }
}
