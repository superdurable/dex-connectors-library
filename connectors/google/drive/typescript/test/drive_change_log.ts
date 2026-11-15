// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { once } from 'node:events';
import { createServer, type Server } from 'node:http';

/**
 * An in-process fake of the Drive changes and files APIs for the file Trigger tests. Like Drive, and like
 * the Go connector's drivechangetest, it reports only each file's latest change after a page token.
 */

/** The fake's state of one Drive file; times are milliseconds since the epoch. */
export interface DriveFile {
  id: string; name: string; mimeType: string; parent: string;
  /** The shared drive ID, or blank for a My Drive file. */ driveId?: string;
  createdTime: number; modifiedTime: number; isTrashed?: boolean; trashedTime?: number;
}

/** One failure the fake returns to the next changes.list request. */
export interface ScriptedResponse { status: number; retryAfter?: string }

/** Format a time as Drive does: UTC with milliseconds. */
export function driveTimeText(milliseconds: number): string { return new Date(milliseconds).toISOString(); }

/** The fake Drive on a loopback HTTP server. Close it with close. */
export class DriveChangeLog {
  readonly #files = new Map<string, DriveFile>();
  readonly #changes: { fileId: string; time: number }[] = [];
  readonly #scripted: ScriptedResponse[] = [];
  readonly #requests = new Map<string, number>();
  #listPages = 0;
  #server: Server | undefined;
  /** The fake's base URL once started. */ url = '';

  /** Accept only accessToken. */
  constructor(private readonly accessToken: string) {}

  /** Start the server on a free loopback port. */
  async start(): Promise<this> {
    this.#server = createServer((request, response) => {
      const url = new URL(request.url ?? '/', 'http://localhost');
      const { status, headers, body } = this.#serve(url, request.headers.authorization ?? '');
      response.writeHead(status, headers).end(body);
    });
    this.#server.listen(0, '127.0.0.1');
    await once(this.#server, 'listening');
    const address = this.#server.address();
    if (!address || typeof address === 'string') throw new Error('fake Drive address is unavailable');
    this.url = `http://127.0.0.1:${address.port}`;
    return this;
  }

  /** Stop the server. */
  async close(): Promise<void> {
    const server = this.#server;
    if (server) await new Promise<void>(resolve => { server.closeAllConnections(); server.close(() => resolve()); });
  }

  /** Add a file and record its change. */
  createFile(id: string, name: string, mimeType: string, parent: string): void {
    const now = Date.now();
    this.put({ id, name, mimeType, parent, createdTime: now, modifiedTime: now });
  }

  /** Store file as is, such as one with an older creation time, and record its change. */
  put(file: DriveFile): void { this.#files.set(file.id, { ...file }); this.#changes.push({ fileId: file.id, time: Date.now() }); }
  /** Set a file's modified time and record its change. */
  modifyFile(id: string, modifiedAt: number): void { this.#edit(id, file => { file.modifiedTime = modifiedAt; }); }
  /** Move a file to the trash; like Drive, set trashedTime only for a shared drive file. */
  trashFile(id: string): void { this.#edit(id, file => { file.isTrashed = true; if (file.driveId) file.trashedTime = Date.now(); }); }
  /** Record a change that leaves the file's times unchanged, such as a permission change. */
  touchFile(id: string): void { this.#edit(id, () => undefined); }
  /** Queue failures for the next changes.list requests. */
  script(...responses: ScriptedResponse[]): void { this.#scripted.push(...responses); }
  /** Count the requests made to a path, such as /drive/v3/changes. */
  requests(path: string): number { return this.#requests.get(path) ?? 0; }
  /** Count the successful changes.list pages served. */
  listPages(): number { return this.#listPages; }

  #edit(id: string, change: (file: DriveFile) => void): void {
    change(this.#files.get(id)!);
    this.#changes.push({ fileId: id, time: Date.now() });
  }

  #serve(url: URL, authorization: string): { status: number; headers: Record<string, string>; body: string } {
    this.#requests.set(url.pathname, this.requests(url.pathname) + 1);
    if (authorization !== `Bearer ${this.accessToken}`) return { status: 401, headers: {}, body: '' };
    if (url.pathname === '/drive/v3/changes/startPageToken') return json({ startPageToken: String(this.#changes.length + 1) });
    if (url.pathname === '/drive/v3/changes') {
      const scripted = this.#scripted.shift();
      if (scripted) return { status: scripted.status, headers: scripted.retryAfter ? { 'Retry-After': scripted.retryAfter } : {}, body: '' };
      return this.#serveChanges(url.searchParams);
    }
    if (url.pathname.startsWith('/drive/v3/files/')) {
      const file = this.#files.get(url.pathname.slice('/drive/v3/files/'.length));
      return file ? json(fileResource(file)) : { status: 404, headers: {}, body: '' };
    }
    return { status: 404, headers: {}, body: '' };
  }

  /** List each file's latest change at or after pageToken, oldest first. */
  #serveChanges(query: URLSearchParams): { status: number; headers: Record<string, string>; body: string } {
    const from = Number(query.get('pageToken')), pageSize = Number(query.get('pageSize'));
    if (!Number.isInteger(from) || from < 1 || from > this.#changes.length + 1 || !Number.isInteger(pageSize) || pageSize < 1) return { status: 400, headers: {}, body: '' };
    const latest = new Map<string, number>();
    for (let index = from - 1; index < this.#changes.length; index++) latest.set(this.#changes[index]!.fileId, index);
    let positions = [...latest.values()].sort((left, right) => left - right);
    const response: Record<string, unknown> = {};
    if (positions.length > pageSize) {
      positions = positions.slice(0, pageSize);
      response.nextPageToken = String(positions[positions.length - 1]! + 2);
    } else response.newStartPageToken = String(this.#changes.length + 1);
    response.changes = positions.map(index => {
      const record = this.#changes[index]!;
      return { changeType: 'file', fileId: record.fileId, time: driveTimeText(record.time), file: fileResource(this.#files.get(record.fileId)!) };
    });
    this.#listPages++;
    return json(response);
  }
}

function fileResource(file: DriveFile): Record<string, unknown> {
  return {
    id: file.id, name: file.name, mimeType: file.mimeType, parents: [file.parent], createdTime: driveTimeText(file.createdTime),
    modifiedTime: driveTimeText(file.modifiedTime), trashed: file.isTrashed === true, size: '42',
    ...(file.driveId ? { driveId: file.driveId } : {}), ...(file.driveId && file.trashedTime ? { trashedTime: driveTimeText(file.trashedTime) } : {}),
  };
}

function json(value: unknown): { status: number; headers: Record<string, string>; body: string } {
  return { status: 200, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(value) };
}
