// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';

/**
 * One Asana project's Events API stream and tasks behind a Client `send` option. It mirrors the Go
 * connector's internal/asanaeventsfake: sync tokens are "sync-<sequence>"; a request without one, or with an
 * expired one, answers 412 with a fresh token, and each page holds at most 100 events, as Asana documents.
 */

/** The personal access token the fake accepts. */
export const AccessToken = 'asana-fake-token';
/** The base URL a Client uses with the fake. */
export const Endpoint = 'https://asana.fake.test/api/1.0';
const pageSize = 100;
const baseTime = Date.UTC(2026, 2, 2, 9, 0, 0);

/** One request the fake answered. */
export interface RecordedRequest { path: string; query: URLSearchParams; status: number }

interface StoredTask { name: string; isCompleted: boolean }

/** A stateful single-project fake. */
export class AsanaEventsFake {
  readonly requests: RecordedRequest[] = [];
  private readonly events: Array<Record<string, unknown>> = [];
  private readonly tasks = new Map<string, StoredTask>();
  private oldestValidToken = 0;
  private rateLimited = 0;
  private retryAfter = '';

  /** Serve projectId. */ constructor(readonly projectId: string) {}

  /** The Client `send` option answered by this fake. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const url = new URL(request.url);
    const [status, body] = this.route(request, url);
    this.requests.push({ path: url.pathname, query: url.searchParams, status });
    const headers: Record<string, string> = { 'content-type': 'application/json' };
    if (status === 429 && this.retryAfter) headers['retry-after'] = this.retryAfter;
    return { status, headers, body: Buffer.from(JSON.stringify(body), 'utf8') };
  };

  /** Create a task in the project and record its added event, with the project as parent. */
  addTask(taskId: string, name: string): void {
    this.tasks.set(taskId, { name, isCompleted: false });
    this.appendEvent(taskId, 'added', { gid: this.projectId, resource_type: 'project', name: 'Facilities' });
  }

  /** Change a task's name and record a changed event for the name field. */
  renameTask(taskId: string, name: string): void {
    this.tasks.get(taskId)!.name = name;
    this.appendEvent(taskId, 'changed', null, { field: 'name', action: 'changed', new_value: name });
  }

  /** Mark a task completed and record a changed event for the completed field. */
  completeTask(taskId: string): void {
    this.tasks.get(taskId)!.isCompleted = true;
    this.appendEvent(taskId, 'changed', null, { field: 'completed', action: 'changed', new_value: true });
  }

  /** Move a task to the trash and record a deleted event. */
  deleteTask(taskId: string): void {
    this.tasks.delete(taskId);
    this.appendEvent(taskId, 'deleted', null);
  }

  /** Record a comment story event, which a taskChanged Trigger must ignore. */
  addStory(storyId: string): void {
    this.events.push({ action: 'added', created_at: this.eventTime(), user: user(), resource: { gid: storyId, resource_type: 'story', resource_subtype: 'comment_added' } });
  }

  /** Make every token older than the latest event answer 412, as Asana does after 24 hours. */
  expireSyncTokens(): void { this.oldestValidToken = this.events.length; }

  /** Answer the next count /events requests with 429 and the given Retry-After header. */
  rateLimitNext(count: number, retryAfter: string): void { this.rateLimited = count; this.retryAfter = retryAfter; }

  /** The token that follows every event recorded so far. */
  syncToken(): string { return `sync-${this.events.length}`; }

  private route(request: ProviderRequest, url: URL): [number, unknown] {
    if (request.headers?.Authorization !== `Bearer ${AccessToken}`) return [401, errorBody('Not Authorized')];
    if (request.method !== 'GET') return [405, errorBody('method not allowed')];
    const path = url.pathname.replace(/^\/api\/1\.0/, '');
    if (path === '/events') return this.serveEvents(url.searchParams);
    if (path.startsWith('/tasks/')) return this.serveTask(path.slice('/tasks/'.length));
    return [404, errorBody('Unknown object')];
  }

  private serveEvents(query: URLSearchParams): [number, unknown] {
    if (query.get('resource') !== this.projectId) return [404, errorBody('Unknown object')];
    if (this.rateLimited > 0) { this.rateLimited--; return [429, errorBody('Rate limit exceeded')]; }
    const fresh = { errors: [{ message: 'Sync token invalid or too old.' }], sync: this.syncToken() };
    const match = /^sync-([0-9]+)$/.exec(query.get('sync') ?? '');
    const sequence = match ? Number(match[1]) : -1;
    if (sequence < this.oldestValidToken || sequence > this.events.length) return [412, fresh];
    const end = Math.min(sequence + pageSize, this.events.length);
    return [200, { data: this.events.slice(sequence, end), sync: `sync-${end}`, has_more: end < this.events.length }];
  }

  private serveTask(taskId: string): [number, unknown] {
    const task = this.tasks.get(taskId);
    if (!task) return [404, errorBody('Unknown object')];
    const stamp = new Date(baseTime).toISOString();
    return [200, { data: {
      gid: taskId, name: task.name, resource_subtype: 'default_task', completed: task.isCompleted,
      memberships: [{ project: { gid: this.projectId, name: 'Facilities' } }], created_at: stamp, modified_at: stamp,
    } }];
  }

  private appendEvent(taskId: string, action: string, parent: Record<string, unknown> | null, change?: Record<string, unknown>): void {
    this.events.push({
      action, created_at: this.eventTime(), user: user(), parent,
      resource: { gid: taskId, resource_type: 'task', resource_subtype: 'default_task', name: this.tasks.get(taskId)?.name ?? '' },
      ...(change ? { change } : {}),
    });
  }

  private eventTime(): string { return new Date(baseTime + this.events.length * 1000).toISOString().replace('.000Z', 'Z'); }
}

function user(): Record<string, string> { return { gid: '1203000000000001', resource_type: 'user', name: 'Ada Lovelace' }; }

function errorBody(message: string): Record<string, unknown> { return { errors: [{ message }] }; }
