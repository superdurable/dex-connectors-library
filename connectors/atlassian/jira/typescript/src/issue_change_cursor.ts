// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Cursor } from '@superdurable/connectors-sdk/pollingtrigger';

/** Re-read window before the cursor, in milliseconds: JQL dates have minute precision and Jira indexes updates late. */
export const issueChangeLookbackMs = 120_000;
/** Keeps the cursor far below the poll kit's 64 KiB token bound. */
export const maximumSeenIssueVersions = 1000;

/**
 * The committed issueChanged position. updatedThrough is the latest updated instant read, in Unix
 * milliseconds; seenVersions lists "issueID@updatedMillis" versions within the lookback before it. pageToken
 * and the search fields are set only while one search spans pages, because Jira requires the next page to
 * repeat the same JQL; searchFloor is the restart floor, in Unix milliseconds, the search began from.
 */
export interface IssueChangeCursor {
  cloudId: string;
  project: string;
  updatedThrough: number;
  seenVersions: string[];
  pageToken: string;
  searchMinutes: number;
  searchStartedAt: number;
  searchFloor: number;
}

const stringFields = { cloudid: 'cloudId', project: 'project', pagetoken: 'pageToken' } as const;
const integerFields = { updatedthrough: 'updatedThrough', searchminutes: 'searchMinutes', searchstartedat: 'searchStartedAt', searchfloor: 'searchFloor' } as const;
const integerLiteral = /^-?(?:0|[1-9][0-9]*)$/;

/** Return an empty position for cloudId and project. */
export function newIssueChangeCursor(cloudId: string, project: string, updatedThrough: number): IssueChangeCursor {
  return { cloudId, project, updatedThrough, seenVersions: [], pageToken: '', searchMinutes: 0, searchStartedAt: 0, searchFloor: 0 };
}

/** Encode position as Go's json.Marshal does: the same field order, omitted empty fields, and HTML escapes. */
export function encodeIssueChangeCursor(position: IssueChangeCursor): Cursor {
  const document: Record<string, unknown> = { cloudId: position.cloudId };
  if (position.project) document.project = position.project;
  document.updatedThrough = position.updatedThrough;
  if (position.seenVersions.length) document.seenVersions = position.seenVersions;
  if (position.pageToken) document.pageToken = position.pageToken;
  if (position.searchMinutes) document.searchMinutes = position.searchMinutes;
  if (position.searchStartedAt) document.searchStartedAt = position.searchStartedAt;
  if (position.searchFloor) document.searchFloor = position.searchFloor;
  // Go's encoding/json escapes these inside strings, so stored tokens match a Go replica's bytes.
  const token = JSON.stringify(document).replace(/[<>&\u2028\u2029]/g, character => `\\u${character.charCodeAt(0).toString(16).padStart(4, '0')}`);
  return { token };
}

/**
 * Decode a stored token as Go's json.Unmarshal does and require a complete position: keys match
 * case-insensitively, unknown keys are ignored, null leaves a field empty, and a number that is not an integer
 * literal fails. An integer beyond 2^53, which Go's int64 holds, also fails, as the SDK's poll cursor
 * documents do. Throws TypeError for anything undecodable.
 */
export function decodeIssueChangeCursor(cursor: Cursor): IssueChangeCursor {
  const invalid = () => new TypeError('Jira issue cursor is not decodable');
  let document: unknown;
  try {
    document = JSON.parse(cursor.token, (_key, value: unknown, context?: { source?: string }) => {
      if (typeof value === 'number' && (!context?.source || !integerLiteral.test(context.source) || !Number.isSafeInteger(value))) throw invalid();
      return value;
    });
  } catch { throw invalid(); }
  if (!document || typeof document !== 'object' || Array.isArray(document)) throw invalid();
  const position = newIssueChangeCursor('', '', 0);
  for (const [key, value] of Object.entries(document as Record<string, unknown>)) {
    const folded = /^[\x20-\x7e]*$/.test(key) ? key.toLowerCase() : key;
    if (folded in stringFields) {
      if (value !== null && typeof value !== 'string') throw invalid();
      position[stringFields[folded as keyof typeof stringFields]] = value ?? '';
    } else if (folded in integerFields) {
      if (value !== null && typeof value !== 'number') throw invalid();
      position[integerFields[folded as keyof typeof integerFields]] = value === null ? 0 : value + 0;
    } else if (folded === 'seenversions') {
      if (value !== null && (!Array.isArray(value) || value.some(item => item !== null && typeof item !== 'string'))) throw invalid();
      position.seenVersions = ((value ?? []) as (string | null)[]).map(item => item ?? '');
    }
  }
  if (!position.cloudId || position.updatedThrough <= 0) throw invalid();
  if (position.pageToken && (position.searchMinutes < 1 || position.searchStartedAt <= 0 || position.searchFloor <= 0)) throw new TypeError('Jira issue cursor has a page token without its search');
  return position;
}

/** Return the "issueID@updatedMillis" key of one issue version. */
export function issueVersionKey(issueId: string, updatedMillis: number): string { return `${issueId}@${updatedMillis}`; }

/**
 * Merge [issueID, updatedMillis] versions into previous, keep those within the lookback before
 * updatedThrough, and keep only the newest when they exceed the bound; a dropped entry can only cause a
 * redelivery with the same event ID. Malformed previous entries are dropped.
 */
export function recentSeenVersions(previous: readonly string[], versions: readonly (readonly [string, number])[], updatedThrough: number): string[] {
  const threshold = updatedThrough - issueChangeLookbackMs;
  const byKey = new Map<string, number>();
  for (const entry of previous) {
    const separator = entry.lastIndexOf('@');
    if (separator < 1) continue;
    const millisText = entry.slice(separator + 1);
    if (!/^[+-]?[0-9]+$/.test(millisText)) continue;
    const millis = Number(millisText);
    // Go parses int64; a magnitude beyond 2^53 cannot be compared exactly here and is dropped.
    if (Number.isSafeInteger(millis)) byKey.set(entry, millis + 0);
  }
  for (const [issueId, updatedMillis] of versions) byKey.set(issueVersionKey(issueId, updatedMillis), updatedMillis);
  const kept = [...byKey.keys()].filter(key => byKey.get(key)! >= threshold);
  kept.sort((left, right) => byKey.get(right)! - byKey.get(left)! || compareBytes(left, right));
  return kept.slice(0, maximumSeenIssueVersions).sort(compareBytes);
}

function compareBytes(left: string, right: string): number { return Buffer.compare(Buffer.from(left), Buffer.from(right)); }
