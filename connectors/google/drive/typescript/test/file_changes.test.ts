// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, StaticCredentialProvider, newTriggerContext } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { describe, expect, it } from 'vitest';
import { FileChangeFetcher, FileChangesFeedName, ZeroGoTime, newFileEvent, type FileEvent } from '../src/file_changes.js';
import { DriveClient, classifyFileChange, decodeFileEventPayload, validateFileTriggerConfiguration, type Credentials } from '../src/index.js';

const checkpoint = '2026-11-01T12:00:00Z';
const checkpointMs = Date.parse(checkpoint);
const connection = { provider: 'google', name: 'drive-files' };
const at = (offsetMs: number) => new Date(checkpointMs + offsetMs).toISOString();
const millis = (offsetMs: number) => String(checkpointMs + offsetMs);
const minute = 60_000;

function fileEvent(fields: Partial<FileEvent>): FileEvent {
  return newFileEvent({ fileId: 'f1', name: 'n', mimeType: 'application/pdf', parents: [], driveId: '', createdTime: ZeroGoTime, modifiedTime: ZeroGoTime,
    trashedTime: ZeroGoTime, changeTime: ZeroGoTime, isTrashed: false, sizeBytes: 0, md5Checksum: '', webViewLink: '', ...fields });
}

describe('classifyFileChange', () => {
  const old = at(-60 * minute), recent = at(minute), withinLookback = at(-minute);
  it.each([
    ['new upload', { createdTime: recent, modifiedTime: recent }, 'f1@created', '', ''],
    ['upload keeps older local mtime', { createdTime: recent, modifiedTime: old }, 'f1@created', '', ''],
    ['created then edited', { createdTime: recent, modifiedTime: at(2 * minute) }, 'f1@created', `f1@modified@${millis(2 * minute)}`, ''],
    ['edit of an old file', { createdTime: old, modifiedTime: recent }, '', `f1@modified@${millis(minute)}`, ''],
    ['late change record inside the lookback', { createdTime: withinLookback, modifiedTime: withinLookback }, 'f1@created', '', ''],
    ['permission or view change', { createdTime: old, modifiedTime: old }, '', '', ''],
    ['trashed with trashed time', { createdTime: old, modifiedTime: recent, isTrashed: true, trashedTime: recent }, '', '', `f1@trashed@${millis(minute)}`],
    ['trashed without trashed time uses the change time', { createdTime: old, modifiedTime: old, isTrashed: true, changeTime: recent }, '', '', `f1@trashed@${millis(minute)}`],
    ['change to a long-trashed file', { createdTime: old, modifiedTime: old, isTrashed: true, trashedTime: old, changeTime: recent }, '', '', ''],
  ] as const)('%s', (_name, fields, created, updated, trashed) => {
    const change = classifyFileChange(fileEvent(fields), checkpoint);
    expect([change.created?.id ?? '', change.updated?.id ?? '', change.trashed?.id ?? '']).toEqual([created, updated, trashed]);
  });
});

it('validates configuration as Go does', () => {
  validateFileTriggerConfiguration({ folderId: 'folder_a', folderName: 'Invoices', mimeType: 'application/vnd.google-apps.document' });
  for (const invalid of [{ folderId: 'https://drive.google.com/drive/folders/x' }, { mimeType: 'Application/PDF' }, { mimeType: 'text/plain; charset=utf-8' }]) {
    expect(() => validateFileTriggerConfiguration(invalid)).toThrow();
  }
});

it('decodes Go inbox payloads and keeps their nanoseconds', () => {
  const payload = '{"fileId":"f1","name":"Invoice.pdf","mimeType":"application/pdf","parents":["folder_a"],"createdTime":"2026-11-01T12:00:00.123456789Z",'
    + '"modifiedTime":"2026-11-01T12:00:00Z","changeTime":"2026-11-01T12:00:01Z","isTrashed":false,"sizeBytes":42,"future":true}';
  const decoded = decodeFileEventPayload(payload, Buffer.from(payload));
  // The stored JSON is the Go connector's bytes once unknown members are dropped.
  expect(JSON.stringify(decoded)).toBe(payload.replace(',"future":true', ''));
  expect(() => decodeFileEventPayload('', Buffer.from('{"sizeBytes":"42"}'))).toThrow();
});

function newFetcher(transport: (request: ProviderRequest) => Promise<ProviderResponse>): FileChangeFetcher {
  const credentials: Credentials = { accessToken: new SecretString('token-SENTINEL') };
  const client = new DriveClient({ endpoint: 'https://drive.example' }, new StaticCredentialProvider([[connection, credentials]]), {
    transport, now: () => new Date(checkpointMs),
    refreshDriver: { refreshRequired: () => false, refresh: async () => { throw new Error('refresh was not expected'); } },
  });
  return new FileChangeFetcher(client, connection, FileChangesFeedName);
}

const respond = (status: number, body = '', headers: Record<string, string> = {}): ProviderResponse => ({ status, headers, body: Buffer.from(body) });

function changeJSON(fileId: string, created: string, changed: string): string {
  return JSON.stringify({ changeType: 'file', fileId, time: changed, file: { id: fileId, name: 'Invoice.pdf', mimeType: 'application/pdf', parents: ['folder_a'], createdTime: created, modifiedTime: created, size: '42' } });
}

it('seeds and keeps the checkpoint across the pages of one pass, with the Go cursor bytes', async () => {
  const first = '2026-11-01T12:01:00Z', second = '2026-11-01T12:02:00Z';
  const requests: URL[] = [];
  const fetcher = newFetcher(async request => {
    const url = new URL(request.url);
    requests.push(url);
    expect(request.headers?.Authorization).toBe('Bearer token-SENTINEL');
    const pageToken = url.searchParams.get('pageToken');
    if (!pageToken) { expect(url.pathname).toBe('/drive/v3/changes/startPageToken'); return respond(200, '{"startPageToken":"100"}'); }
    if (pageToken === '100') return respond(200, `{"nextPageToken":"101","changes":[${changeJSON('f1', first, first)},{"changeType":"drive","driveId":"d1"},{"changeType":"file","fileId":"gone","removed":true}]}`);
    return respond(200, `{"newStartPageToken":"102","changes":[${changeJSON('f2', second, second)}]}`);
  });
  const context = newTriggerContext();
  const seeded = await fetcher.seedCursor(context);
  expect(seeded.token).toBe('{"pageToken":"100","checkpoint":"2026-11-01T12:00:00Z"}');
  const page = await fetcher.fetchChanges(context, seeded);
  expect(page.hasMorePages).toBe(true);
  expect(page.changes.map(change => change.created?.id)).toEqual(['f1@created']);
  expect(page.next.token).toBe('{"pageToken":"101","checkpoint":"2026-11-01T12:00:00Z","roundLatest":"2026-11-01T12:01:00Z"}');
  const last = await fetcher.fetchChanges(context, page.next);
  expect(last.hasMorePages).toBe(false);
  expect(last.changes[0]!.created?.id).toBe('f2@created');
  expect(last.next.token).toBe('{"pageToken":"102","checkpoint":"2026-11-01T12:02:00Z"}');
  const listed = requests[1]!.searchParams;
  expect(Object.fromEntries(['pageSize', 'spaces', 'includeRemoved', 'includeItemsFromAllDrives', 'supportsAllDrives'].map(name => [name, listed.get(name)])))
    .toEqual({ pageSize: '100', spaces: 'drive', includeRemoved: 'false', includeItemsFromAllDrives: 'true', supportsAllDrives: 'true' });
  expect(listed.get('fields')).toContain('newStartPageToken');
  expect(requests[1]!.search.startsWith('?fields=nextPageToken%2CnewStartPageToken')).toBe(true);
  expect(requests[0]!.searchParams.get('supportsAllDrives')).toBe('true');
});

it('round-trips Go-written cursors that keep provider offsets and nanoseconds', async () => {
  const stored = '{"pageToken":"101","checkpoint":"2026-11-01T12:00:00.123456789Z","roundLatest":"2026-11-01T13:01:00.5+01:00"}';
  const fetcher = newFetcher(async () => respond(200, '{"nextPageToken":"102","changes":[]}'));
  const page = await fetcher.fetchChanges(newTriggerContext(), { token: stored });
  expect(page.next.token).toBe(stored.replace('"101"', '"102"'));
  // Go matches field names case-insensitively and ignores data after the first JSON value.
  const relaxed = await fetcher.fetchChanges(newTriggerContext(), { token: '{"PageToken":"7","checkpoint":"2026-11-01T12:00:00Z"} x' });
  expect(relaxed.next.token).toBe('{"pageToken":"102","checkpoint":"2026-11-01T12:00:00Z"}');
});

it('maps provider failures to poll signals', async () => {
  let response = respond(200);
  const fetcher = newFetcher(async () => response);
  const cursor: Cursor = { token: '{"pageToken":"100","checkpoint":"2026-11-01T12:00:00Z"}' };
  const context = newTriggerContext();
  const failure = async (next: ProviderResponse): Promise<unknown> => { response = next; return fetcher.fetchChanges(context, cursor).then(() => undefined, (error: unknown) => error); };

  const limited = await failure(respond(429, '', { 'Retry-After': '7' }));
  expect(limited).toBeInstanceOf(RateLimitedError);
  expect((limited as RateLimitedError).retryAfterMs).toBe(7000);
  expect(await failure(respond(403, '{"error":{"errors":[{"reason":"userRateLimitExceeded"}]}}'))).toBeInstanceOf(RateLimitedError);
  expect(isCursorInvalid(await failure(respond(400, '{"error":{"errors":[{"reason":"invalid"}]}}')))).toBe(true);
  expect(isCursorInvalid(await failure(respond(400, '{"error":{"errors":[{"reason":"invalid","locationType":"parameter","location":"pageToken"}]}}')))).toBe(true);
  for (const rejected of [404, 410]) expect(isCursorInvalid(await failure(respond(rejected)))).toBe(true);
  const fields = await failure(respond(400, '{"error":{"errors":[{"reason":"invalidParameter","locationType":"parameter","location":"fields"}]}}'));
  expect(fields).toBeInstanceOf(Error);
  expect(isCursorInvalid(fields), 'a 400 for another parameter would recur after every reseed').toBe(false);
  const seed = await fetcher.seedCursor(context).then(() => undefined, (error: unknown) => error);
  expect(seed).toBeInstanceOf(Error);
  expect(isCursorInvalid(seed), 'a rejected seed is retried, not reseeded').toBe(false);
  const unavailable = await failure(respond(503));
  expect(isCursorInvalid(unavailable) || unavailable instanceof RateLimitedError).toBe(false);
  expect(String(await failure(respond(200, '{"changes":[]}')))).toContain('neither a next page token nor a new start page token');
  expect(String(await failure(respond(200, '{"changes":[{"time":5}]}')))).toContain('invalid change response');
  expect(isCursorInvalid(await fetcher.fetchChanges(context, { token: '{"pageToken":"100"}' }).catch((error: unknown) => error))).toBe(true);
});

it('validates the poll connection settings', () => {
  const provider = new StaticCredentialProvider<Credentials>([]);
  for (const config of [{ pollIntervalMs: 500 }, { pollPageSize: 501 }, { pollPageSize: -1 }]) expect(() => new DriveClient(config, provider)).toThrow();
});
