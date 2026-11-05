// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export type StarCountStorage = Pick<Storage, "getItem" | "setItem">;

export const starCountCacheMilliseconds: number;
export const failedFetchCacheMilliseconds: number;
export const starCountFetchTimeoutMilliseconds: number;

export function readCachedStarCount(
  storage: StarCountStorage,
  cacheKey: string,
  nowMilliseconds: number,
): number | null | undefined;
export function writeCachedStarCount(
  storage: StarCountStorage,
  cacheKey: string,
  starCount: number | null,
  nowMilliseconds: number,
): void;
export function writeFailedFetchUnlessCountCached(
  storage: StarCountStorage,
  cacheKey: string,
  nowMilliseconds: number,
): void;
export function isValidStarCount(value: unknown): value is number;
export function formatStarCount(starCount: number): string;
export function formatStarredLinkLabel(linkName: string, starCount: number | null): string | undefined;
