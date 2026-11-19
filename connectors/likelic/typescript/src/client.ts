// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListRoomsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListRoomsInput, ListRoomsOutput, Room } from './generated/models.js';

const emptyOutput: ListRoomsOutput = { page: 0, total: 0, rooms: [] };
const roomListEndpoint = 'https://agent.likelic.com/api/v1/rooms';
/** Send a Likelic request through a caller-owned transport. */
export type RoomRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional transport dependency for provider tests. */
export interface LikelicClientOptions { /** Request sender; production uses the Connector SDK transport. */ send?: RoomRequestSender; }
/** Resolve Likelic credentials for each room query. */
export class LikelicClient {
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: RoomRequestSender;
  /** Validate configuration without contacting Likelic. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: LikelicClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Likelic configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Likelic configuration is invalid');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the room-list Query. */
  listRooms(): ListRoomsOperation { return new ListRoomsOperation(this); }
}
/** Read one page of Likelic notification targets. */
export class ListRoomsOperation implements Query<ListRoomsInput, ListRoomsOutput> {
  /** Bind an authenticated Likelic client. */
  constructor(private readonly client: LikelicClient) {}
  /** Return the stable generated Query contract. */
  definition(): QueryDefinition { return ListRoomsDefinition; }
  /** Send and classify one room-list request. */
  async invoke(call: Call, input: ListRoomsInput): Promise<QueryAttempt<ListRoomsOutput>> {
    if (!Number.isInteger(input.page) || input.page < 1 || input.page > 1_000_000) return branch('defect', 'VALIDATION', 'Likelic page must be from 1 to 1000000');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'Likelic credentials are unavailable or invalid'); }
    const key = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!/^sk-[0-9a-fA-F]{48}$/.test(key) || !/^[A-Za-z0-9_-]{1,50}$/.test(credentials.customerCode ?? '')) return branch('defect', 'AUTHENTICATION', 'Likelic credentials are unavailable or invalid');
    let response: ProviderResponse;
    try {
      response = await this.client.send({ url: `${roomListEndpoint}?page=${input.page}&limit=20`, method: 'GET',
        headers: { Accept: 'application/json', 'X-Customer-Code': credentials.customerCode, 'X-API-Key': key },
        timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes,
        addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (error.status === 429 || error.status === 408 || error.status >= 500) return retry(error.status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', 'Likelic room service is temporarily unavailable', getRetryAfter(error.headers ?? {}));
        return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Likelic response exceeded the configured limit');
      }
      return retry('AVAILABILITY', 'Likelic room request failed');
    }
    const delay = getRetryAfter(response.headers);
    if (response.status === 429) return retry('RATE_LIMIT', 'Likelic rate limited the room request', delay);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', 'Likelic room service is temporarily unavailable', delay);
    if (response.status !== 200) return branch('providerRejected', response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION', `Likelic rejected room request with HTTP ${response.status}`);
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'Likelic returned invalid room page'); }
    if (!isRecord(document) || document.success !== true || !Array.isArray(document.data) || !isRecord(document.meta) || document.meta.page !== input.page || document.meta.limit !== 20 || !Number.isInteger(document.meta.total) || (document.meta.total as number) < 0 || document.data.length > 20) return branch('invalidResponse', 'PROTOCOL', 'Likelic returned invalid room page');
    const rooms: Room[] = [];
    for (const item of document.data as unknown[]) {
      if (!isRecord(item) || (item.channel !== 'wework' && item.channel !== 'qq') || !Number.isSafeInteger(item.instance_id) || (item.instance_id as number) <= 0 || typeof item.room_id !== 'string' || !item.room_id || item.room_id.length > 256 || /[\r\n\0]/.test(item.room_id) || !['ready','blocked','unknown'].includes(String(item.push_state))) return branch('invalidResponse', 'PROTOCOL', 'Likelic returned invalid room data');
      rooms.push({ channel: item.channel, instanceID: item.instance_id as number, roomID: item.room_id, pushState: item.push_state as string });
    }
    return newQueryBranch('found', { page: input.page, total: document.meta.total as number, rooms });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function getRetryAfter(headers: ProviderResponse['headers']): number { const value = headers['retry-after']; return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? ''); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'likelic', operation: 'listRooms', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<ListRoomsOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<ListRoomsOutput> { return newQueryRetry(failure(kind, message), delayMs); }
