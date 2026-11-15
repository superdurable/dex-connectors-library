// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { GoInstant, parseGoTime } from './go_time.js';

/** A scan that spans several search pages; Confluence's search cursor continues only its own CQL. */
export interface PageChangeScan { startedAt: GoInstant; cql: string; searchCursor: string }

/** The committed position: when the last complete scan began, and any scan in progress. */
export interface PageChangeCursor { cloudId: string; spaceKey: string; scannedThrough: GoInstant; scan: PageChangeScan | undefined }

/** The snapshot value of one delivered or baseline page version. */
export interface SeenPageVersion { versionNumber: number; lastModifiedAt: GoInstant }

const int64Minimum = -(2n ** 63n), int64Maximum = 2n ** 63n - 1n;

class UndecodableError extends Error {}

/** Replace lone surrogates with U+FFFD, as Go does when it encodes a string as UTF-8. */
function wellFormed(value: string): string {
  return value.replace(/[\ud800-\udbff](?![\udc00-\udfff])|(?<![\ud800-\udbff])[\udc00-\udfff]/g, '\ufffd');
}

/** Encode a string as Go's encoding/json does, escaping HTML characters and replacing lone surrogates. */
export function goJSONString(value: string): string {
  return JSON.stringify(wellFormed(value)).replace(/[<>&\u2028\u2029]/g, character => `\\u${character.charCodeAt(0).toString(16).padStart(4, '0')}`);
}

/** Encode position as Go's json.Marshal does, with the same field order and omitted empty fields. */
export function encodePageChangeCursor(position: PageChangeCursor): string {
  const fields = [`"cloudId":${goJSONString(position.cloudId)}`];
  if (position.spaceKey) fields.push(`"spaceKey":${goJSONString(position.spaceKey)}`);
  fields.push(`"scannedThrough":"${position.scannedThrough.format()}"`);
  if (position.scan) {
    const scan = [`"startedAt":"${position.scan.startedAt.format()}"`, `"cql":${goJSONString(position.scan.cql)}`];
    if (position.scan.searchCursor) scan.push(`"searchCursor":${goJSONString(position.scan.searchCursor)}`);
    fields.push(`"scan":{${scan.join(',')}}`);
  }
  return `{${fields.join(',')}}`;
}

/**
 * Decode a stored token as Go's json.Unmarshal does: keys match case-insensitively, unknown keys are ignored,
 * and null leaves a field empty. Throws TypeError for a token Go cannot decode.
 */
export function decodePageChangeCursor(token: string): PageChangeCursor {
  try {
    const document = parseStrictJSON(token);
    const position: PageChangeCursor = { cloudId: '', spaceKey: '', scannedThrough: GoInstant.zero, scan: undefined };
    if (document === null) return position;
    const values = objectFields(document, ['cloudid', 'spacekey', 'scannedthrough', 'scan']);
    position.cloudId = stringField(values.get('cloudid'));
    position.spaceKey = stringField(values.get('spacekey'));
    position.scannedThrough = timeField(values.get('scannedthrough'));
    const scan = values.get('scan');
    if (scan !== undefined && scan !== null) {
      const scanValues = objectFields(scan, ['startedat', 'cql', 'searchcursor']);
      position.scan = { startedAt: timeField(scanValues.get('startedat')), cql: stringField(scanValues.get('cql')), searchCursor: stringField(scanValues.get('searchcursor')) };
    }
    return position;
  } catch {
    throw new TypeError('Confluence pageChanged cursor is not decodable');
  }
}

/** Encode the snapshot value as Go marshals seenPageVersion. */
export function encodeSeenPageVersion(seen: SeenPageVersion): Uint8Array {
  return new TextEncoder().encode(`{"versionNumber":${seen.versionNumber},"lastModifiedAt":"${seen.lastModifiedAt.format()}"}`);
}

/** Decode a snapshot value as Go's json.Unmarshal does; throws TypeError for one Go cannot decode. */
export function decodeSeenPageVersion(value: Uint8Array | undefined): SeenPageVersion {
  try {
    const document = parseStrictJSON(new TextDecoder('utf-8', { fatal: true }).decode(value ?? new Uint8Array()));
    if (document === null) return { versionNumber: 0, lastModifiedAt: GoInstant.zero };
    const values = objectFields(document, ['versionnumber', 'lastmodifiedat']);
    return { versionNumber: integerField(values.get('versionnumber')), lastModifiedAt: timeField(values.get('lastmodifiedat')) };
  } catch {
    throw new TypeError('Confluence seen page version is not decodable');
  }
}

/** Hash the page version as Go's seenPageItem.SnapshotHash does. */
export function seenPageHash(pageId: string, versionNumber: number): string {
  return createHash('sha256').update(`${wellFormed(pageId)}\u0000${versionNumber}`, 'utf8').digest().subarray(0, 8).toString('hex');
}

/** Return the pageChanged Trigger event ID: a hash of site, page ID, and version, as the Go connector computes it. */
export function pageChangeEventId(cloudId: string, pageId: string, versionNumber: number): string {
  const joined = [wellFormed(cloudId), wellFormed(pageId), String(versionNumber)].join('\u0000');
  return 'confluence-page-' + createHash('sha256').update(joined, 'utf8').digest().subarray(0, 16).toString('hex');
}

/** An integer literal kept with its source text, so int64 bounds are checked exactly. */
class IntegerLiteral { constructor(readonly text: string) {} }
/** A non-integer number literal, which Go cannot decode into an int field. */
class FractionLiteral {}

function parseStrictJSON(text: string): unknown {
  return JSON.parse(text, (_key, value: unknown, context?: { source?: string }) => {
    if (typeof value !== 'number') return value;
    const source = context?.source ?? String(value);
    return /^-?\d+$/.test(source) ? new IntegerLiteral(source) : new FractionLiteral();
  });
}

/** Return the known fields of a JSON object by folded name; the last spelling wins, as in Go. */
function objectFields(document: unknown, names: readonly string[]): Map<string, unknown> {
  if (!document || typeof document !== 'object' || Array.isArray(document) || document instanceof IntegerLiteral || document instanceof FractionLiteral) throw new UndecodableError();
  const values = new Map<string, unknown>();
  for (const [key, value] of Object.entries(document)) {
    const folded = [...key].every(character => character.charCodeAt(0) < 0x80) ? key.toLowerCase() : key;
    if (names.includes(folded)) values.set(folded, value);
  }
  return values;
}

function stringField(value: unknown): string {
  if (value === undefined || value === null) return '';
  if (typeof value !== 'string') throw new UndecodableError();
  return value;
}

function integerField(value: unknown): number {
  if (value === undefined || value === null) return 0;
  if (!(value instanceof IntegerLiteral)) throw new UndecodableError();
  const integer = BigInt(value.text);
  if (integer < int64Minimum || integer > int64Maximum) throw new UndecodableError();
  return Number(integer);
}

function timeField(value: unknown): GoInstant {
  if (value === undefined || value === null) return GoInstant.zero;
  if (typeof value !== 'string') throw new UndecodableError();
  const parsed = parseGoTime(value);
  if (!parsed) throw new UndecodableError();
  return parsed;
}
