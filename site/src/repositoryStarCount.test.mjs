// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import test from "node:test";

import {
  failedFetchCacheMilliseconds,
  formatStarCount,
  formatStarredLinkLabel,
  isValidStarCount,
  readCachedStarCount,
  starCountCacheMilliseconds,
  writeCachedStarCount,
  writeFailedFetchUnlessCountCached,
} from "./repositoryStarCount.mjs";

function createMemoryStorage() {
  const entries = new Map();
  return {
    getItem: (key) => (entries.has(key) ? entries.get(key) : null),
    setItem: (key, value) => entries.set(key, String(value)),
  };
}

const blockedStorage = {
  getItem: () => {
    throw new Error("blocked");
  },
  setItem: () => {
    throw new Error("blocked");
  },
};

test("cached counts expire after one hour and stay separate per cache key", () => {
  const storage = createMemoryStorage();
  writeCachedStarCount(storage, "first-repository", 12, 1000);
  assert.equal(readCachedStarCount(storage, "first-repository", 1000 + starCountCacheMilliseconds - 1), 12);
  assert.equal(readCachedStarCount(storage, "first-repository", 1000 + starCountCacheMilliseconds), undefined);
  assert.equal(readCachedStarCount(storage, "second-repository", 1000), undefined);
});

test("failed fetches back off for ten minutes without replacing a fresh count", () => {
  const storage = createMemoryStorage();
  writeFailedFetchUnlessCountCached(storage, "repository", 0);
  assert.equal(readCachedStarCount(storage, "repository", failedFetchCacheMilliseconds - 1), null);
  assert.equal(readCachedStarCount(storage, "repository", failedFetchCacheMilliseconds), undefined);

  writeCachedStarCount(storage, "repository", 7, 0);
  writeFailedFetchUnlessCountCached(storage, "repository", 1);
  assert.equal(readCachedStarCount(storage, "repository", 1), 7);
});

test("malformed, future-dated, and blocked cache entries read as misses", () => {
  const storage = createMemoryStorage();
  storage.setItem("malformed", "{");
  storage.setItem("invalid-count", JSON.stringify({ starCount: -1, fetchedAt: 0 }));
  writeCachedStarCount(storage, "future", 5, 1000);
  assert.equal(readCachedStarCount(storage, "malformed", 0), undefined);
  assert.equal(readCachedStarCount(storage, "invalid-count", 0), undefined);
  assert.equal(readCachedStarCount(storage, "future", 0), undefined);
  assert.equal(readCachedStarCount(blockedStorage, "repository", 0), undefined);
  assert.doesNotThrow(() => writeFailedFetchUnlessCountCached(blockedStorage, "repository", 0));
});

test("star counts validate, format, and label like the shared header", () => {
  assert.equal(isValidStarCount(0), true);
  assert.equal(isValidStarCount(1.5), false);
  assert.equal(isValidStarCount("12"), false);
  assert.equal(formatStarCount(999), "999");
  assert.equal(formatStarCount(1250), "1.3k");
  assert.equal(formatStarredLinkLabel("GitHub Repo", 1), "GitHub Repo, 1 star");
  assert.equal(formatStarredLinkLabel("GitHub", 1250), "GitHub, 1.3k stars");
  assert.equal(formatStarredLinkLabel("GitHub Repo", null), undefined);
});
