// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, randomBytes } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListRoomsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListRoomsInput, ListRoomsOutput, RoomSummary } from './generated/models.js';

const empty: ListRoomsOutput = { rooms: [] };
/** Inject provider transport, clock, and nonce for deterministic tests. */
export interface QuanshiClientOptions {
  /** Dispatch requests; production uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Supply signing time; production uses the current clock. */ now?: () => Date;
  /** Supply 16 nonce bytes; production uses cryptographic randomness. */ nonce?: () => Uint8Array;
}
/** List Quanshi rooms after obtaining a short-lived access token. */
export class QuanshiClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  readonly now: () => Date;
  readonly nonce: () => Uint8Array;
  /** Validate provider settings before contacting Quanshi. */
  constructor(config: Config, readonly credentials: CredentialProvider<Credentials>, options: QuanshiClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Quanshi configuration and credential source are required');
    const url = new URL(config.baseUrl ?? 'https://openapi.quanshicloud.com');
    if (url.protocol !== 'https:' || !['openapi.quanshicloud.com', 'openapi-beta.quanshicloud.com'].includes(url.host) || url.username || url.password || url.search || url.hash || url.pathname !== '/') throw new TypeError('Quanshi OpenAPI URL is invalid');
    this.baseUrl = url.origin;
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('Quanshi request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    this.nonce = options.nonce ?? (() => randomBytes(16));
  }
  /** Bind the room listing Query. */ listRooms(): ListRoomsOperation { return new ListRoomsOperation(this); }
}
/** Classify token and room responses without exposing access tokens. */
export class ListRoomsOperation implements Query<ListRoomsInput, ListRoomsOutput> {
  /** Bind one validated client. */ constructor(private readonly client: QuanshiClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListRoomsDefinition; }
  /** Obtain a token and read the administrator's rooms. */
  async invoke(call: Call, input: ListRoomsInput): Promise<QueryAttempt<ListRoomsOutput>> {
    void input;
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); } catch { return branch('defect', 'AUTHENTICATION'); }
    const appId = credentials.appId instanceof SecretString ? credentials.appId.reveal() : '';
    const secret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
    if (!isCredential(appId) || !isCredential(secret)) return branch('defect', 'AUTHENTICATION');
    let nonce: string;
    try { const bytes = this.client.nonce(); if (bytes.length !== 16) throw new TypeError('nonce length'); nonce = Buffer.from(bytes).toString('hex'); } catch { return retry('AVAILABILITY'); }
    const curTime = String(this.client.now().getTime());
    const checksum = createHash('sha1').update(secret + nonce + curTime).digest('hex');
    const tokenRequest = this.request(call, '/eventopenapi/token/v2/create', '{}', { AppId: appId, Nonce: nonce, CurTime: curTime, CheckSum: checksum });
    const tokenResponse = await this.fetch(tokenRequest);
    if ('decision' in tokenResponse) return tokenResponse.decision;
    const token = parseDocument(tokenResponse.response.body);
    if (!isRecord(token) || !Number.isSafeInteger(token.code)) return branch('invalidResponse', 'PROTOCOL');
    if (token.code !== 200) return codeBranch(token.code as number);
    const data = token.data;
    if (!isRecord(data) || !isCredential(data.token) || !Number.isSafeInteger(data.userId) || (data.userId as number) <= 0) return branch('invalidResponse', 'PROTOCOL');
    const roomsRequest = this.request(call, '/confopenapi/room/list', JSON.stringify({ userId: data.userId }), { token: data.token as string });
    const roomsResponse = await this.fetch(roomsRequest);
    if ('decision' in roomsResponse) return roomsResponse.decision;
    const document = parseDocument(roomsResponse.response.body);
    if (!isRecord(document) || !Number.isSafeInteger(document.status)) return branch('invalidResponse', 'PROTOCOL');
    if (document.status !== 0) return codeBranch(document.status as number);
    const result = document.result;
    if (!isRecord(result) || !Array.isArray(result.room) || result.room.length > 1000) return branch('invalidResponse', 'PROTOCOL');
    const rooms: RoomSummary[] = [];
    for (const room of result.room) {
      if (!isRecord(room) || !Number.isSafeInteger(room.roomId) || (room.roomId as number) <= 0 || typeof room.roomName !== 'string' || !room.roomName || room.roomName.length > 512 || !Number.isSafeInteger(room.roomCapacity) || (room.roomCapacity as number) < 0 || !Number.isSafeInteger(room.roomStatus)) return branch('invalidResponse', 'PROTOCOL');
      rooms.push({ id: room.roomId as number, name: room.roomName, capacity: room.roomCapacity as number, status: room.roomStatus as number });
    }
    return newQueryBranch('listed', { rooms });
  }
  private request(call: Call, path: string, body: string, headers: Record<string, string>): ProviderRequest {
    return { url: `${this.client.baseUrl}${path}`, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/json', ...headers }, body: new TextEncoder().encode(body), timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
  }
  private async fetch(request: ProviderRequest): Promise<{ response: ProviderResponse } | { decision: QueryAttempt<ListRoomsOutput> }> {
    let response: ProviderResponse;
    try { response = await this.client.send(request); } catch (error) { return { decision: error instanceof BodyTooLargeError && error.status !== undefined ? branch('invalidResponse', 'RESPONSE_TOO_LARGE') : retry('AVAILABILITY') }; }
    const header = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    const delay = parseRetryAfter(Array.isArray(header) ? header[0] ?? '' : header ?? '');
    if (response.status === 429) return { decision: retry('RATE_LIMIT', delay) };
    if (response.status === 408 || response.status >= 500) return { decision: retry('AVAILABILITY', delay) };
    if (response.body.length > this.client.maxResponseBytes) return { decision: branch('invalidResponse', 'RESPONSE_TOO_LARGE') };
    if (response.status !== 200) return { decision: branch('providerRejected', response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION') };
    return { response };
  }
}
function parseDocument(body: Uint8Array): unknown { try { return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return null; } }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isCredential(value: unknown): value is string { return typeof value === 'string' && value.length > 0 && value.length <= 512 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'quanshi', operation: 'listRooms', kind, message: 'Quanshi room query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListRoomsOutput> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListRoomsOutput> { return newQueryRetry(failure(kind), delay); }
function codeBranch(code: number): QueryAttempt<ListRoomsOutput> { return branch('providerRejected', code === 9004 ? 'AUTHENTICATION' : code === 9020 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION'); }
