// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { ZoomClient, newConnection, type Connection, type Credentials, type ZoomClientOptions } from '../src/index.js';

/** The bearer token the fake accepts. */
export const AccessToken = 'zoom-trigger-token';
const baseTime = Date.UTC(2036, 1, 23, 17);
const hourMs = 3_600_000;
const rfc3339 = (milliseconds: number) => new Date(milliseconds).toISOString().replace(/\.\d{3}Z$/, 'Z');

interface StoredMeeting { id: number; topic: string; startMs: number; duration: number; createdOrder: number }
/** One recorded request and the status the fake answered. */
export interface RecordedRequest { path: string; query: URLSearchParams; status: number }

/**
 * A single-user Zoom fake serving GET /users/me/meetings and GET /meetings/{id}, mirroring the Go connector's
 * internal/zoommeetingsfake: page_size up to 300, an opaque next_page_token, and meetings in creation order.
 */
export class ZoomMeetingsFake {
  readonly requests: RecordedRequest[] = [];
  private created = 0;
  private readonly meetings = new Map<number, StoredMeeting>();
  private readonly deleted = new Map<number, StoredMeeting>();
  private rejectedPageTokens = 0;
  private rateLimited = 0;
  private rateLimitIsDaily = false;
  private retryAfterHeader = '';
  private readonly issuedPageTokens = new Map<string, number>();

  /** Serve one Zoom user's meetings. */ constructor(public hostId: string) {}

  /** Create a 30-minute meeting starting days after the base time. */
  schedule(id: number, topic: string, days: number): void {
    this.created++;
    this.meetings.set(id, { id, topic, startMs: baseTime + days * 24 * hourMs, duration: 30, createdOrder: this.created });
  }
  /** Move an existing meeting's start by hours. */ reschedule(id: number, hours: number): void { this.meetings.get(id)!.startMs += hours * hourMs; }
  /** Change an existing meeting's topic. */ rename(id: number, topic: string): void { this.meetings.get(id)!.topic = topic; }
  /** Move a meeting to Recently Deleted. */ delete(id: number): void { this.deleted.set(id, this.meetings.get(id)!); this.meetings.delete(id); }
  /** Restore a deleted meeting unchanged. */ recover(id: number): void { this.meetings.set(id, this.deleted.get(id)!); this.deleted.delete(id); }
  /** Answer the next count requests carrying a page token with 400. */ rejectNextPageTokens(count: number): void { this.rejectedPageTokens = count; }
  /** Answer the next count listings with 429, with Zoom's daily message when isDaily. */
  rateLimitNext(count: number, isDaily: boolean, retryAfter: string): void { this.rateLimited = count; this.rateLimitIsDaily = isDaily; this.retryAfterHeader = retryAfter; }
  /** Count listing requests. */ listingRequestCount(): number { return this.requests.filter(request => request.path.endsWith('/users/me/meetings')).length; }

  /** Answer one provider request; pass it as ZoomClientOptions.send. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const url = new URL(request.url), headers: Record<string, string> = { 'content-type': 'application/json' };
    const [status, body] = this.answer(request, url, headers);
    this.requests.push({ path: url.pathname.replace(/^\/v2/, ''), query: url.searchParams, status });
    return { status, headers, body: Buffer.from(JSON.stringify(body)) };
  };

  private answer(request: ProviderRequest, url: URL, headers: Record<string, string>): [number, unknown] {
    const authorization = (request.headers as Record<string, string> | undefined)?.Authorization;
    if (request.method !== 'GET' || authorization !== `Bearer ${AccessToken}`) return [401, { code: 124, message: 'Invalid access token.' }];
    const path = url.pathname.replace(/^\/v2/, '');
    if (path.startsWith('/meetings/')) return this.answerMeeting(path.slice('/meetings/'.length));
    if (path !== '/users/me/meetings') return [404, { code: 1001, message: 'User does not exist.' }];
    if (this.rateLimited > 0) {
      this.rateLimited--;
      if (this.retryAfterHeader) headers['retry-after'] = this.retryAfterHeader;
      const message = this.rateLimitIsDaily
        ? 'You have reached the maximum daily rate limit for this API. Refer to the response header for details on when you can make another call.'
        : 'You have reached the maximum per-second rate limit for this API. Try again later.';
      return [429, { code: 429, message }];
    }
    return this.answerListing(url.searchParams);
  }

  private answerListing(query: URLSearchParams): [number, unknown] {
    const invalidToken = { code: 300, message: 'The next page token is invalid or expired.' };
    if (query.get('type') !== 'scheduled') return [400, { code: 300, message: 'Unexpected type.' }];
    const pageSize = Number(query.get('page_size'));
    if (!Number.isInteger(pageSize) || pageSize < 1 || pageSize > 300) return [400, { code: 300, message: 'Invalid page_size.' }];
    let offset = 0;
    const token = query.get('next_page_token') ?? '';
    if (token) {
      if (this.rejectedPageTokens > 0) { this.rejectedPageTokens--; return [400, invalidToken]; }
      if (!this.issuedPageTokens.has(token)) return [400, invalidToken];
      offset = this.issuedPageTokens.get(token)!;
    }
    const ordered = [...this.meetings.values()].sort((left, right) => left.createdOrder - right.createdOrder);
    const end = Math.min(offset + pageSize, ordered.length);
    let nextPageToken = '';
    if (end < ordered.length) { nextPageToken = `page-${this.issuedPageTokens.size + 1}-${end}`; this.issuedPageTokens.set(nextPageToken, end); }
    return [200, { next_page_token: nextPageToken, page_size: pageSize, total_records: ordered.length, meetings: ordered.slice(Math.min(offset, end), end).map(meeting => this.resource(meeting)) }];
  }

  private answerMeeting(rawId: string): [number, unknown] {
    const stored = /^[0-9]+$/.test(rawId) ? this.meetings.get(Number(rawId)) : undefined;
    if (!stored) return [404, { code: 3001, message: 'Meeting does not exist.' }];
    return [200, { ...this.resource(stored), settings: { waiting_room: true } }];
  }

  private resource(stored: StoredMeeting): Record<string, unknown> {
    return {
      id: stored.id, uuid: `uuid-${stored.id}==`, host_id: this.hostId, topic: stored.topic, type: 2, start_time: rfc3339(stored.startMs),
      duration: stored.duration, timezone: 'America/Los_Angeles', join_url: `https://us05web.zoom.us/j/${stored.id}`, created_at: rfc3339(baseTime - stored.createdOrder * hourMs),
    };
  }
}

/** A Zoom connection named connectionName whose requests the fake answers. */
export function newFakeConnection(fake: ZoomMeetingsFake, connectionName = 'zoom-watcher', pollIntervalMs = 30_000): Connection {
  const reference = { provider: 'zoom', name: connectionName };
  return newConnection(newFakeClient(fake, connectionName, pollIntervalMs), reference);
}

/** A ZoomClient whose requests the fake answers, with a fixed clock when now is given. */
export function newFakeClient(fake: { send: ZoomClientOptions['send'] }, connectionName = 'zoom-watcher', pollIntervalMs = 30_000, now?: () => Date, accessToken = AccessToken): ZoomClient {
  const reference = { provider: 'zoom', name: connectionName };
  const credentials = new StaticCredentialProvider<Credentials>([[reference, { oauthClientId: '', oauthClientSecret: new SecretString(''), accessToken: new SecretString(accessToken), refreshToken: new SecretString('') }]]);
  return new ZoomClient({ endpoint: 'https://zoom.example/v2', pollIntervalMs }, credentials, { ...(fake.send ? { send: fake.send } : {}), ...(now ? { now } : {}) });
}

/**
 * A loaded project with one Zoom OAuth connection and its bindings over in-memory storage; pass the same
 * objects again to model a restarted process that reopens the storage the first one wrote.
 */
export async function newTestProject(connectionName: string, triggerBindings: Array<{ bindingName: string; configuration: Record<string, unknown> }>, restartedObjects?: InMemoryObjectStore, pollInterval: unknown = 30_000_000_000): Promise<{ project: LoadedProject; objects: InMemoryObjectStore }> {
  const scope = { projectId: 'zoom-trigger-tests', kind: 'live' } as const;
  const objects = restartedObjects ?? new InMemoryObjectStore();
  if (!restartedObjects) await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'zoom', connectionName }, 0,
    new CredentialMaterial({ oauth_client_id: 'zoom-client-id', oauth_client_secret: 'zoom-client-secret', access_token: AccessToken, refresh_token: 'zoom-refresh-token' }, new Date('2099-01-01T00:00:00Z'), 'oauth2'));
  const project = new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    // Dex Web stores a duration as Go nanoseconds.
    connections: [{ connectorId: 'zoom', connectionName, modulePath: 'zoom', provider: 'zoom', configuration: { endpoint: 'https://zoom.example/v2', pollInterval } }],
    triggerBindings: triggerBindings.map(binding => ({ connectorId: 'zoom', connectionName, triggerName: 'meetingChanged', ...binding })),
  }, undefined, objects, undefined, undefined);
  return { project, objects };
}
