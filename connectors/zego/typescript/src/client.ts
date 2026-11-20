// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, randomBytes } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetRoomUserCountDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetRoomUserCountInput, GetRoomUserCountOutput } from './generated/models.js';

/** Optional provider sender, clock, and nonce source for deterministic tests. */
export interface ZEGOClientOptions {
  /** Send a bounded provider request. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Return the signature timestamp. */ now?: () => Date;
  /** Generate eight random bytes per signed request. */ random?: (count: number) => Uint8Array;
}

/** Read ZIM room member counts without exposing identities. */
export class ZEGOClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly random: (count: number) => Uint8Array;
  /** Validate immutable request configuration. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ZEGOClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('ZEGO configuration and credentials are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('ZEGO request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    this.random = options.random ?? randomBytes;
  }
  /** Bind the room-count Query. */ getRoomUserCount(): GetRoomUserCountOperation { return new GetRoomUserCountOperation(this); }
  /** Send one signed room query. */
  async request(call: Call, roomId: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const appId = credentials.appId;
    const secret = credentials.serverSecret instanceof SecretString ? credentials.serverSecret.reveal() : '';
    if (!isValidProjectCredentials(appId, secret)) return { error: 'credential', retryAfterMs: 0 };
    let nonce: string;
    try { const bytes = this.random(8); if (bytes.length !== 8) throw new Error('invalid nonce'); nonce = Buffer.from(bytes).toString('hex'); }
    catch { return { error: 'transport', retryAfterMs: 0 }; }
    const timestamp = String(Math.floor(this.now().getTime() / 1000));
    const signature = createHash('md5').update(appId + nonce + secret + timestamp).digest('hex');
    const query = new URLSearchParams({ Action: 'QueryRoomUserList', AppId: appId, SignatureNonce: nonce, Timestamp: timestamp, SignatureVersion: '2.0', Signature: signature, RoomId: roomId, Limit: '1' });
    const request: ProviderRequest = { url: `https://zim-api.zego.im/?${query}`, method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfter = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '') };
  }
}

/** Classify ZEGO room-query outcomes. */
export class GetRoomUserCountOperation implements Query<GetRoomUserCountInput, GetRoomUserCountOutput> {
  /** Bind the validated client. */ constructor(private readonly client: ZEGOClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetRoomUserCountDefinition; }
  /** Return the member count or a retry decision. */
  async invoke(call: Call, input: GetRoomUserCountInput): Promise<QueryAttempt<GetRoomUserCountOutput>> {
    if (!isValidRoomId(input.roomId)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.request(call, input.roomId);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || !Number.isSafeInteger(wire.Code)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.Code === 660300006) return retry('RATE_LIMIT');
    if (wire.Code === 660300007 || wire.Code === 100000004) return retry('AVAILABILITY');
    if (wire.Code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!Number.isSafeInteger(wire.Count) || (wire.Count as number) < 0) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { count: wire.Count as number });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isValidProjectCredentials(appId: string, secret: string): boolean { return /^(?:[1-9][0-9]*)$/.test(appId) && BigInt(appId) <= 4_294_967_295n && !!secret && secret.length <= 1024 && /^[!-~]+$/.test(secret); }
function isValidRoomId(roomId: string): boolean { return !!roomId && roomId.length <= 32 && /^[A-Za-z0-9!#$%&()+\-:;<=>?@[\]^_{}|~.]+$/.test(roomId); }
function failure(kind: Failure['kind']): Failure { return { provider: 'zego', operation: 'getRoomUserCount', kind, message: 'ZEGO room query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetRoomUserCountOutput> { return newQueryBranch(name, { count: 0 }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetRoomUserCountOutput> { return newQueryRetry(failure(kind), delay); }
