// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectionRef, TriggerContext, TriggerEvent, TriggerRef } from '@superdurable/connectors-sdk';
import { MaxChangesPerPage, markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest, type ChangeFetcher, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import type { KeyedSnapshotEntry } from '@superdurable/connectors-sdk/projectconfig';
import { BodyTooLargeError, readErrorTokens, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { goRFC3339, isDriveId, isRateLimited, type DriveClient } from './client.js';
import { ConnectorID } from './generated/definitions.js';
import type { Credentials } from './generated/models.js';

/** The fileCreated Trigger name. */ export const FileCreatedTriggerName = 'fileCreated';
/** The fileUpdated Trigger name. */ export const FileUpdatedTriggerName = 'fileUpdated';
/** The fileTrashed Trigger name. */ export const FileTrashedTriggerName = 'fileTrashed';
/** The stable storage identity of the shared changes.list cursor. */ export const FileChangesFeedName = 'fileChanges';

const nanosecondsPerMillisecond = 1_000_000n;
// Drive does not promise change records in timestamp order; stable event IDs absorb the repeats.
const classificationLookbackNanoseconds = 5n * 60n * 1_000_000_000n;
// Separates an edit from the write that created the file.
const modifiedAfterCreationThresholdNanoseconds = 1_000_000_000n;
/** Go's zero time.Time as encoding/json writes it. */
export const ZeroGoTime = '0001-01-01T00:00:00Z';
// mime.ParseMediaType token characters, lowercase because the parsed type must equal the value.
const plainMediaTypePattern = /^[a-z0-9!#$%&'*+\-.^_`{|}~]+\/[a-z0-9!#$%&'*+\-.^_`{|}~]+$/;

/**
 * The metadata of one Drive file that a file Trigger delivers; field names and JSON shape match the Go
 * connector's FileEvent. It never carries content. Times are Go RFC 3339 strings that keep the provider's
 * offset and nanoseconds.
 */
export interface FileEvent {
  /** The Drive file ID, usable with getFile and readFileText. */ fileId: string;
  /** The file name, which is not unique within a folder. */ name: string;
  /** The file's MIME type. */ mimeType: string;
  /** The file's direct parent folder ID; Drive allows at most one. Omitted when Drive reports none. */ parents?: string[];
  /** The shared drive ID; omitted for a My Drive file. */ driveId?: string;
  /** When the file was created. */ createdTime: string;
  /** When anyone last modified the file. */ modifiedTime: string;
  /** When the file was trashed; Drive reports it only for shared drive files, so it is omitted in My Drive. */ trashedTime?: string;
  /** The time of the Drive change record that reported the file. */ changeTime: string;
  /** Whether the file is in the trash, explicitly or through a trashed parent folder. */ isTrashed: boolean;
  /** The size of blob and Google Workspace editor files; omitted when Drive omits it. */ sizeBytes?: number;
  /** The content checksum of blob files; omitted for Google Workspace files. */ md5Checksum?: string;
  /** Opens the file in a browser. */ webViewLink?: string;
}

/**
 * Filters the files a fileCreated, fileUpdated, or fileTrashed binding receives; every non-blank field must
 * match, and the empty configuration matches every file.
 */
export interface FileTriggerConfiguration {
  /**
   * Limits events to files whose direct parent is this Drive folder or shared drive ID, as the folderPicker
   * unit saves it. Blank matches every folder; subfolders are not searched, because Drive reports only a
   * file's direct parent.
   */
  folderId?: string;
  /** The display name the folderPicker unit saves beside folderId; informational, never matched. */ folderName?: string;
  /**
   * Limits events to files of exactly this MIME type, such as application/pdf or
   * application/vnd.google-apps.document. Blank matches every type, including folders.
   */
  mimeType?: string;
}

/**
 * Check that folderId is a Drive ID and mimeType a plain lowercase type/subtype when set, as the Go
 * connector's FileTriggerConfiguration.Validate does; throws TypeError otherwise.
 */
export function validateFileTriggerConfiguration(configuration: FileTriggerConfiguration): void {
  if (!configuration) throw new TypeError('Google Drive file Trigger configuration is required');
  if (configuration.folderId && !isDriveId(configuration.folderId)) throw new TypeError('Google Drive file Trigger folderId must be a Drive folder ID');
  if (configuration.mimeType && !plainMediaTypePattern.test(configuration.mimeType)) {
    throw new TypeError('Google Drive file Trigger mimeType must be one lowercase type/subtype without parameters');
  }
}

function configurationMatches(configuration: FileTriggerConfiguration, file: FileEvent): boolean {
  if (configuration.folderId && !(file.parents ?? []).includes(configuration.folderId)) return false;
  return !configuration.mimeType || configuration.mimeType === file.mimeType;
}

/** One usable changes.list entry with the Trigger events it produces, classified against the cursor checkpoint. */
export interface FileChange {
  /** The reported file. */ file: FileEvent;
  /** The fileCreated event, if any. */ created?: TriggerEvent<FileEvent>;
  /** The fileUpdated event, if any. */ updated?: TriggerEvent<FileEvent>;
  /** The fileTrashed event, if any. */ trashed?: TriggerEvent<FileEvent>;
}

function eventFor(change: FileChange, triggerName: string): TriggerEvent<FileEvent> | undefined {
  if (triggerName === FileCreatedTriggerName) return change.created;
  if (triggerName === FileUpdatedTriggerName) return change.updated;
  if (triggerName === FileTrashedTriggerName) return change.trashed;
  return undefined;
}

/** Return the instant of a canonical Go RFC 3339 time in nanoseconds since the Unix epoch. */
export function goTimeNanoseconds(text: string): bigint {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?(Z|([+-])(\d{2}):(\d{2}))$/.exec(text);
  if (!match) throw new TypeError('invalid RFC 3339 time');
  const [, year, month, day, hour, minute, second, fraction, , sign, offsetHour, offsetMinute] = match;
  const date = new Date(Date.UTC(2000, 0, 1));
  date.setUTCFullYear(Number(year), Number(month) - 1, Number(day));
  date.setUTCHours(Number(hour), Number(minute), Number(second), 0);
  const offsetMinutes = sign ? (sign === '+' ? 1 : -1) * (Number(offsetHour) * 60 + Number(offsetMinute)) : 0;
  return BigInt(date.getTime()) * nanosecondsPerMillisecond + BigInt((fraction ?? '').padEnd(9, '0')) - BigInt(offsetMinutes) * 60_000_000_000n;
}

function isZeroGoTime(text: string): boolean { return goTimeNanoseconds(text) === goTimeNanoseconds(ZeroGoTime); }

function floorDivide(dividend: bigint, divisor: bigint): bigint {
  const quotient = dividend / divisor;
  return dividend % divisor !== 0n && (dividend < 0n) !== (divisor < 0n) ? quotient - 1n : quotient;
}

function occurredAt(text: string): Date { return new Date(Number(floorDivide(goTimeNanoseconds(text), nanosecondsPerMillisecond))); }

// One ID per file, kind, and provider time, so each edit or trash is one event however often refetched.
function fileTriggerEvent(file: FileEvent, kind: string, at: string): TriggerEvent<FileEvent> {
  const unixMilliseconds = floorDivide(goTimeNanoseconds(at), nanosecondsPerMillisecond);
  return { id: `${file.fileId}@${kind}@${unixMilliseconds}`, occurredAt: occurredAt(at), payload: file };
}

/**
 * Derive events from file state alone, because Drive changes carry no change kind. A trashed file produces
 * only fileTrashed. Otherwise a file created after the lookback produces fileCreated, and one modified after
 * the lookback and after its creation write produces fileUpdated.
 */
export function classifyFileChange(file: FileEvent, checkpoint: string): FileChange {
  const lookback = goTimeNanoseconds(checkpoint) - classificationLookbackNanoseconds;
  if (file.isTrashed) {
    const trashedAt = file.trashedTime && !isZeroGoTime(file.trashedTime) ? file.trashedTime : file.changeTime;
    return goTimeNanoseconds(trashedAt) > lookback ? { file, trashed: fileTriggerEvent(file, 'trashed', trashedAt) } : { file };
  }
  const change: FileChange = { file };
  const created = goTimeNanoseconds(file.createdTime), modified = goTimeNanoseconds(file.modifiedTime);
  if (created > lookback) change.created = { id: `${file.fileId}@created`, occurredAt: occurredAt(file.createdTime), payload: file };
  if (modified > lookback && modified - created >= modifiedAfterCreationThresholdNanoseconds) change.updated = fileTriggerEvent(file, 'modified', file.modifiedTime);
  return change;
}

/** Selects one binding's events from a shared feed change. */
export type FileChangeSelector = (change: FileChange, previous: KeyedSnapshotEntry | undefined) => TriggerEvent<FileEvent>[];

/** Return the feed selector of one binding of triggerName. */
export function fileChangeSelector(triggerName: string, configuration: FileTriggerConfiguration): FileChangeSelector {
  return change => {
    const event = eventFor(change, triggerName);
    return event && configurationMatches(configuration, change.file) ? [event] : [];
  };
}

/** Parse a provider time with Go's time.Time JSON rules and return its canonical Go text. */
export function canonicalGoTime(text: string): string { return goRFC3339(text); }

/** Encode a string as Go's encoding/json with SetEscapeHTML(false): lone surrogates become U+FFFD. */
export function goJSONString(text: string): string {
  return JSON.stringify(text.replace(/[\ud800-\udbff](?![\udc00-\udfff])|(?<![\ud800-\udbff])[\udc00-\udfff]/g, '\ufffd'))
    .replaceAll('\u2028', '\\u2028').replaceAll('\u2029', '\\u2029');
}

/** Format a UTC instant as Go's time.Time MarshalJSON does, trimming trailing fraction zeros. */
export function goUTCTime(date: Date): string {
  const iso = date.toISOString();
  const fraction = iso.slice(20, 23).replace(/0+$/, '');
  return `${iso.slice(0, 19)}${fraction ? `.${fraction}` : ''}Z`;
}

/** The stored feed position, the Go connector's fileChangeCursor; times are canonical Go text. */
interface FileChangeCursor { pageToken: string; checkpoint: string; roundLatest: string }

function encodeFileChangeCursor(position: FileChangeCursor): { token: string } {
  let token = `{"pageToken":${goJSONString(position.pageToken)},"checkpoint":${goJSONString(position.checkpoint)}`;
  if (!isZeroGoTime(position.roundLatest)) token += `,"roundLatest":${goJSONString(position.roundLatest)}`;
  return { token: `${token}}` };
}

/** Select an object's members as encoding/json matches struct fields: exact or case-folded, last wins. */
function goFields(value: unknown, names: readonly string[], isUnknownRejected = false): Record<string, unknown> {
  if (value === null || value === undefined) return {};
  if (typeof value !== 'object' || Array.isArray(value)) throw new TypeError('JSON value is not an object');
  const selected: Record<string, unknown> = {};
  for (const [key, member] of Object.entries(value)) {
    const name = names.find(candidate => key === candidate || key.toLowerCase() === candidate.toLowerCase());
    if (name !== undefined) selected[name] = member;
    else if (isUnknownRejected) throw new TypeError(`JSON object has unknown field ${JSON.stringify(key)}`);
  }
  return selected;
}

function goString(value: unknown): string {
  if (value === null || value === undefined) return '';
  if (typeof value !== 'string') throw new TypeError('JSON value is not a string');
  return value;
}

function goBool(value: unknown): boolean {
  if (value === null || value === undefined) return false;
  if (typeof value !== 'boolean') throw new TypeError('JSON value is not a boolean');
  return value;
}

function goTime(value: unknown): string {
  if (value === null || value === undefined) return ZeroGoTime;
  if (typeof value !== 'string') throw new TypeError('JSON value is not a time');
  return canonicalGoTime(value);
}

function goStrings(value: unknown): string[] {
  if (value === null || value === undefined) return [];
  if (!Array.isArray(value)) throw new TypeError('JSON value is not an array');
  return value.map(goString);
}

/** Return the first JSON value of text, ignoring what follows it, as Go's json.Decoder.Decode does. */
function firstJSONValue(text: string): unknown {
  try { return JSON.parse(text); } catch (error) {
    const start = text.search(/\S/);
    if (start < 0 || text[start] !== '{') throw error;
    let depth = 0;
    for (let index = start, isInString = false; index < text.length; index++) {
      const character = text[index];
      if (isInString) { if (character === '\\') index++; else if (character === '"') isInString = false; continue; }
      if (character === '"') isInString = true;
      else if (character === '{' || character === '[') depth++;
      else if ((character === '}' || character === ']') && --depth === 0) return JSON.parse(text.slice(start, index + 1));
    }
    throw error;
  }
}

/** Decode a stored cursor as Go's json.Decoder with DisallowUnknownFields does, or throw. */
function decodeFileChangeCursor(token: string): FileChangeCursor {
  let position: FileChangeCursor;
  try {
    const fields = goFields(firstJSONValue(token), ['pageToken', 'checkpoint', 'roundLatest'], true);
    position = { pageToken: goString(fields.pageToken), checkpoint: goTime(fields.checkpoint), roundLatest: goTime(fields.roundLatest) };
  } catch (error) { throw new Error('stored Google Drive change cursor is invalid', { cause: error }); }
  if (!position.pageToken || isZeroGoTime(position.checkpoint)) throw new Error('stored Google Drive change cursor is invalid');
  return position;
}

function goInt64(value: unknown): number {
  if (value === null || value === undefined) return 0;
  if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('JSON value is not an int64');
  return value;
}

const fileEventFields = ['fileId', 'name', 'mimeType', 'parents', 'driveId', 'createdTime', 'modifiedTime', 'trashedTime', 'changeTime', 'isTrashed', 'sizeBytes', 'md5Checksum', 'webViewLink'] as const;

/** Build a FileEvent in Go field order, omitting the members Go's omitempty and omitzero omit. */
export function newFileEvent(file: Omit<FileEvent, 'parents' | 'driveId' | 'trashedTime' | 'sizeBytes' | 'md5Checksum' | 'webViewLink'> & {
  parents: string[]; driveId: string; trashedTime: string; sizeBytes: number; md5Checksum: string; webViewLink: string;
}): FileEvent {
  return {
    fileId: file.fileId, name: file.name, mimeType: file.mimeType, ...(file.parents.length ? { parents: file.parents } : {}),
    ...(file.driveId ? { driveId: file.driveId } : {}), createdTime: file.createdTime, modifiedTime: file.modifiedTime,
    ...(!isZeroGoTime(file.trashedTime) ? { trashedTime: file.trashedTime } : {}), changeTime: file.changeTime, isTrashed: file.isTrashed,
    ...(file.sizeBytes ? { sizeBytes: file.sizeBytes } : {}), ...(file.md5Checksum ? { md5Checksum: file.md5Checksum } : {}),
    ...(file.webViewLink ? { webViewLink: file.webViewLink } : {}),
  };
}

/**
 * Decode a pending inbox payload that the Go, Python, or TypeScript connector stored, as Go's json.Unmarshal
 * does: unknown members are ignored and times keep their offset and nanoseconds. Throws for a payload that is
 * not a FileEvent object.
 */
export function decodeFileEventPayload(_payloadJSON: string, payloadBytes: Uint8Array): FileEvent {
  try {
    const fields = goFields(JSON.parse(Buffer.from(payloadBytes).toString('utf8')), fileEventFields);
    return newFileEvent({
      fileId: goString(fields.fileId), name: goString(fields.name), mimeType: goString(fields.mimeType), parents: goStrings(fields.parents),
      driveId: goString(fields.driveId), createdTime: goTime(fields.createdTime), modifiedTime: goTime(fields.modifiedTime),
      trashedTime: goTime(fields.trashedTime), changeTime: goTime(fields.changeTime), isTrashed: goBool(fields.isTrashed),
      sizeBytes: goInt64(fields.sizeBytes), md5Checksum: goString(fields.md5Checksum), webViewLink: goString(fields.webViewLink),
    });
  } catch (error) { throw new Error('pending Google Drive file event is invalid', { cause: error }); }
}

const changeFileFields = 'nextPageToken,newStartPageToken,changes(changeType,removed,fileId,time,' +
  'file(id,name,mimeType,parents,driveId,createdTime,modifiedTime,trashed,trashedTime,size,md5Checksum,webViewLink))';
const changeFields = ['changeType', 'removed', 'fileId', 'time', 'file'] as const;
const changeFileResourceFields = ['id', 'name', 'mimeType', 'parents', 'driveId', 'createdTime', 'modifiedTime', 'trashed', 'trashedTime', 'size', 'md5Checksum', 'webViewLink'] as const;

interface DriveChangeList { nextPageToken: string; newStartPageToken: string; changeTimes: string[]; files: FileEvent[] }

class ChangePageTooLargeError extends Error {
  constructor(count: number) { super(`Google Drive returned ${count} changes, more than the requested page size`); }
}

/** Decode a changes.list response as Go decodes driveChangeList, keeping only usable file changes. */
function decodeDriveChangeList(document: unknown): DriveChangeList {
  const listed = goFields(document, ['nextPageToken', 'newStartPageToken', 'changes']);
  const resources = listed.changes;
  if (resources !== undefined && resources !== null && !Array.isArray(resources)) throw new TypeError('changes is not an array');
  const changeTimes: string[] = [];
  const files: FileEvent[] = [];
  for (const resource of (resources as unknown[] | null | undefined) ?? []) {
    const change = goFields(resource, changeFields);
    const changeTime = goTime(change.time);
    changeTimes.push(changeTime);
    const file = change.file === null || change.file === undefined ? undefined : goFields(change.file, changeFileResourceFields);
    const decoded = fileEventFromChange(goString(change.changeType), goBool(change.removed), goString(change.fileId), changeTime, file);
    if (decoded) files.push(decoded);
  }
  if (changeTimes.length > MaxChangesPerPage) throw new ChangePageTooLargeError(changeTimes.length);
  return { nextPageToken: goString(listed.nextPageToken), newStartPageToken: goString(listed.newStartPageToken), changeTimes, files };
}

/** Skip drive changes, removals, and entries without valid file metadata. */
function fileEventFromChange(changeType: string, isRemoved: boolean, fileId: string, changeTime: string, file: Record<string, unknown> | undefined): FileEvent | undefined {
  if (!file) return undefined;
  // Every member decodes first, so a mistyped member fails the page as Go's json.Unmarshal does.
  const identifier = goString(file.id), name = goString(file.name), mimeType = goString(file.mimeType);
  const parents = goStrings(file.parents), driveId = goString(file.driveId);
  const createdTime = goTime(file.createdTime), modifiedTime = goTime(file.modifiedTime);
  const isTrashed = goBool(file.trashed), trashedTime = goTime(file.trashedTime);
  const size = goString(file.size), md5Checksum = goString(file.md5Checksum), webViewLink = goString(file.webViewLink);
  if (changeType !== 'file' || isRemoved || identifier !== fileId || !isDriveId(identifier) || !name || !mimeType || isZeroGoTime(createdTime) || isZeroGoTime(modifiedTime)) return undefined;
  const sizeBytes = parseOptionalInt64(size);
  if (sizeBytes === undefined) return undefined;
  return newFileEvent({ fileId: identifier, name, mimeType, parents, driveId, createdTime, modifiedTime, trashedTime, changeTime, isTrashed, sizeBytes, md5Checksum, webViewLink });
}

function parseOptionalInt64(value: string): number | undefined {
  if (value === '') return 0;
  if (!/^[+-]?[0-9]+$/.test(value)) return undefined;
  const parsed = BigInt(value);
  // Go accepts every int64; sizes beyond 2^53 are not exact JavaScript numbers.
  if (parsed < 0n || parsed >= 1n << 63n) return undefined;
  return Number(parsed);
}

/** Reads one connection's Drive change log for every file Trigger binding; a poll kit ChangeFetcher. */
export class FileChangeFetcher implements ChangeFetcher<FileChange> {
  readonly #client: DriveClient;
  readonly #connection: ConnectionRef;
  readonly #trigger: TriggerRef;

  /** Bind the client and the stable credential Call identity of triggerName, or of the shared feed name. */
  constructor(client: DriveClient, connection: ConnectionRef, triggerName: string) {
    if (!client) throw new TypeError('Google Drive client is required');
    this.#client = client;
    this.#connection = connection;
    this.#trigger = { connectorId: ConnectorID, triggerName };
  }

  /** Store Drive's current start page token, so existing files are never reported. */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    const document = await this.#getJSON(context, `/changes/startPageToken?${this.#client.query({ supportsAllDrives: 'true' })}`, false);
    let startPageToken: string;
    try { startPageToken = goString(goFields(document, ['startPageToken']).startPageToken); }
    catch (error) { throw new Error('Google Drive returned an invalid change response', { cause: error }); }
    if (!startPageToken) throw new Error('Google Drive returned no start page token');
    return encodeFileChangeCursor({ pageToken: startPageToken, checkpoint: goUTCTime(this.#client.now()), roundLatest: ZeroGoTime });
  }

  /** Read one changes.list page after cursor and classify every usable file change. */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<FileChange>> {
    let position: FileChangeCursor;
    try { position = decodeFileChangeCursor(cursor.token); }
    catch (error) { throw markCursorInvalid(error); }
    const query = this.#client.query({
      pageToken: position.pageToken, pageSize: String(this.#client.pollPageSize), fields: changeFileFields, spaces: 'drive',
      includeRemoved: 'false', includeItemsFromAllDrives: 'true', supportsAllDrives: 'true',
    });
    const document = await this.#getJSON(context, `/changes?${query}`, true);
    let listed: DriveChangeList;
    try { listed = decodeDriveChangeList(document); }
    catch (error) {
      if (error instanceof ChangePageTooLargeError) throw new Error(error.message);
      throw new Error('Google Drive returned an invalid change response', { cause: error });
    }
    let latest = position.roundLatest;
    for (const changeTime of listed.changeTimes) if (goTimeNanoseconds(changeTime) > goTimeNanoseconds(latest)) latest = changeTime;
    const changes = listed.files.map(file => classifyFileChange(file, position.checkpoint));
    if (listed.nextPageToken) {
      return { changes, next: encodeFileChangeCursor({ pageToken: listed.nextPageToken, checkpoint: position.checkpoint, roundLatest: latest }), hasMorePages: true };
    }
    if (listed.newStartPageToken) {
      const checkpoint = goTimeNanoseconds(latest) > goTimeNanoseconds(position.checkpoint) ? latest : position.checkpoint;
      return { changes, next: encodeFileChangeCursor({ pageToken: listed.newStartPageToken, checkpoint, roundLatest: ZeroGoTime }), hasMorePages: false };
    }
    throw new Error('Google Drive returned neither a next page token nor a new start page token');
  }

  /** Send one authenticated read and map failures to the poll kit's signals; only a stored page token is a cursor fault. */
  async #getJSON(context: TriggerContext, pathAndQuery: string, isCursorRequest: boolean): Promise<unknown> {
    await waitForPollRequest(context);
    const call = newTriggerCredentialCall(this.#connection, this.#trigger, context.signal);
    let credentials: Credentials;
    try { credentials = await this.#client.resolve(call); }
    catch (error) {
      if (context.signal.aborted) throw error;
      throw new Error('Google Drive Trigger credentials are unavailable');
    }
    const limit = this.#client.maxResponseBytes;
    let response: ProviderResponse;
    try { ({ response } = await this.#client.request(call, credentials, 'GET', `${this.#client.endpoint}/drive/v3${pathAndQuery}`, limit, undefined, undefined, context.signal)); }
    catch (error) {
      if (error instanceof BodyTooLargeError) throw new Error('Google Drive changes page exceeds maxResponseBytes; lower pollPageSize');
      throw new Error(`Google Drive change request failed: ${error instanceof Error ? error.name : 'unknown error'}`, { cause: error });
    }
    if (response.status >= 200 && response.status < 300) {
      if (response.body.length > limit) throw new Error('Google Drive changes page exceeds maxResponseBytes; lower pollPageSize');
      try { return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)); }
      catch (error) { throw new Error('Google Drive returned an invalid change response', { cause: error }); }
    }
    const reasons = readErrorTokens(response.body, ['/error/errors/0/reason', '/error/status']);
    const rejection = new Error(`Google Drive rejected the change request with HTTP ${response.status}`);
    if (isRateLimited(response.status, reasons)) throw markRateLimited(rejection, this.#client.retryDelay(response.headers));
    if (isCursorRequest && isRejectedPageToken(response.status, response.body)) throw markCursorInvalid(rejection);
    throw rejection;
  }
}

/** A 400 for another parameter, such as fields, would recur after every reseed, so it is retried. */
function isRejectedPageToken(status: number, body: Uint8Array): boolean {
  if (status === 404 || status === 410) return true;
  if (status !== 400) return false;
  const location = readErrorTokens(body, ['/error/errors/0/location']);
  return location.length === 0 || location[0] === 'pageToken';
}

/** Turns the shared change feed into one binding's event feed, for the per-Trigger factories. */
export class BindingFileEventFetcher implements ChangeFetcher<TriggerEvent<FileEvent>> {
  /** Bind the change fetcher and the binding's selector. */
  constructor(private readonly changes: FileChangeFetcher, private readonly selectEvents: FileChangeSelector) {}
  /** Return Drive's current change position. */
  seedCursor(context: TriggerContext): Promise<Cursor> { return this.changes.seedCursor(context); }
  /** Return the binding's events from one changes.list page. */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<FileEvent>>> {
    const page = await this.changes.fetchChanges(context, cursor);
    return { changes: page.changes.flatMap(change => this.selectEvents(change, undefined)), next: page.next, hasMorePages: page.hasMorePages };
  }
}
