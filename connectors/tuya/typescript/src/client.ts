// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, createHmac, randomBytes } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetDeviceDetailsDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetDeviceDetailsInput, GetDeviceDetailsOutput } from './generated/models.js';

const emptyBodySHA256 = createHash('sha256').update('').digest('hex');
const permittedEndpoints = new Set(['https://openapi.tuyacn.com', 'https://openapi.tuyaus.com', 'https://openapi-ueaz.tuyaus.com', 'https://openapi.tuyaeu.com', 'https://openapi-weaz.tuyaeu.com', 'https://openapi.tuyain.com', 'https://openapi-sg.iotbing.com']);

type SignedReply = { status: number; body: Uint8Array; retryAfterMs: number } | { attempt: QueryAttempt<GetDeviceDetailsOutput> };

/** Optional transport, clock, and nonce source for signed provider tests. */
export interface TuyaClientOptions {
  /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Clock; omitted uses the current UTC time. */ now?: () => Date;
  /** Sixteen random bytes per request; omitted uses cryptographic randomness. */ nonce?: () => Uint8Array;
}

/** Sign token and device reads with current Tuya project credentials. */
export class TuyaClient {
  private readonly endpoint: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly nonce: () => Uint8Array;

  /** Validate region and bounded request settings without contacting Tuya. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: TuyaClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Tuya configuration and credentials are required');
    this.endpoint = config.endpoint || 'https://openapi.tuyacn.com';
    if (!permittedEndpoints.has(this.endpoint)) throw new TypeError('Tuya endpoint must be a supported data center');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Tuya request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    this.nonce = options.nonce ?? (() => randomBytes(16));
  }

  /** Bind the device-details Query. */ getDeviceDetails(): GetDeviceDetailsOperation { return new GetDeviceDetailsOperation(this); }

  /** Resolve the current project credential pair. */
  async resolve(call: Call): Promise<{ clientId: string; clientSecret: string } | undefined> {
    try {
      const credential = await this.credentials.resolve(call, call.context.cancellationSignal);
      const clientId = credential.clientId instanceof SecretString ? credential.clientId.reveal() : '';
      const clientSecret = credential.clientSecret instanceof SecretString ? credential.clientSecret.reveal() : '';
      return validCredential(clientId) && validCredential(clientSecret) ? { clientId, clientSecret } : undefined;
    } catch { return undefined; }
  }

  /** Sign one GET and classify transport, rate, and response-bound failures. */
  async signedGet(call: Call, credential: { clientId: string; clientSecret: string }, path: string, accessToken: string): Promise<SignedReply> {
    let nonce: Uint8Array;
    let timestamp: string;
    try {
      nonce = this.nonce();
      if (nonce.length !== 16) throw new TypeError('nonce must be sixteen bytes');
      timestamp = String(this.now().getTime());
      if (!/^\d+$/.test(timestamp)) throw new TypeError('timestamp is invalid');
    } catch { return { attempt: retry('AVAILABILITY') }; }
    const nonceHex = Buffer.from(nonce).toString('hex');
    const stringToSign = `GET\n${emptyBodySHA256}\n\n${path}`;
    const message = credential.clientId + accessToken + timestamp + nonceHex + stringToSign;
    const signature = createHmac('sha256', credential.clientSecret).update(message).digest('hex').toUpperCase();
    const headers: Record<string, string> = { client_id: credential.clientId, t: timestamp, nonce: nonceHex, sign_method: 'HMAC-SHA256', sign: signature, Accept: 'application/json' };
    if (accessToken) headers.access_token = accessToken;
    const request: ProviderRequest = { url: this.endpoint + path, method: 'GET', headers, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { attempt: branch('invalidResponse', 'RESPONSE_TOO_LARGE') };
      return { attempt: retry('AVAILABILITY') };
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'));
    if (response.status === 429) return { attempt: retry('RATE_LIMIT', retryAfterMs) };
    if (response.status === 408 || response.status >= 500) return { attempt: retry('AVAILABILITY', retryAfterMs) };
    if (response.body.length > this.maxResponseBytes) return { attempt: branch('invalidResponse', 'RESPONSE_TOO_LARGE') };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

/** Obtain a simple-mode token, then read safe device metadata. */
export class GetDeviceDetailsOperation implements Query<GetDeviceDetailsInput, GetDeviceDetailsOutput> {
  /** Bind a validated Tuya client. */ constructor(private readonly client: TuyaClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetDeviceDetailsDefinition; }
  /** Classify token and device outcomes with the Go branch contract. */
  async invoke(call: Call, input: GetDeviceDetailsInput): Promise<QueryAttempt<GetDeviceDetailsOutput>> {
    if (!validDeviceId(input?.deviceId)) return branch('defect', 'VALIDATION');
    const credential = await this.client.resolve(call);
    if (!credential) return branch('defect', 'AUTHENTICATION');
    const tokenReply = await this.client.signedGet(call, credential, '/v1.0/token?grant_type=1', '');
    if ('attempt' in tokenReply) return tokenReply.attempt;
    if (tokenReply.status !== 200) return classifyStatus(tokenReply.status, 'token');
    const tokenWire = decodeJson(tokenReply.body);
    if (!isObject(tokenWire) || typeof tokenWire.success !== 'boolean') return branch('invalidResponse', 'PROTOCOL');
    if (!tokenWire.success) return tokenWire.code === 500 || tokenWire.code === 1010 ? retry('AVAILABILITY') : branch('providerRejected', 'AUTHENTICATION');
    if (!isObject(tokenWire.result) || !validCredential(tokenWire.result.access_token) || !Number.isSafeInteger(tokenWire.result.expire_time) || (tokenWire.result.expire_time as number) <= 0) return branch('invalidResponse', 'PROTOCOL');
    const accessToken = tokenWire.result.access_token as string;
    const deviceReply = await this.client.signedGet(call, credential, `/v1.0/devices/${input.deviceId}`, accessToken);
    if ('attempt' in deviceReply) return deviceReply.attempt;
    if (deviceReply.status !== 200) return classifyStatus(deviceReply.status, 'device');
    const deviceWire = decodeJson(deviceReply.body);
    if (!isObject(deviceWire) || typeof deviceWire.success !== 'boolean') return branch('invalidResponse', 'PROTOCOL');
    if (!deviceWire.success) {
      if (deviceWire.code === 500 || deviceWire.code === 1010) return retry('AVAILABILITY');
      return deviceWire.code === 1000 ? branch('notFound', 'NOT_FOUND') : branch('providerRejected', 'PROVIDER_REJECTION');
    }
    if (!isObject(deviceWire.result)) return branch('invalidResponse', 'PROTOCOL');
    const data = deviceWire.result;
    if (data.id !== input.deviceId || typeof data.name !== 'string' || !data.name || typeof data.online !== 'boolean' || !Number.isSafeInteger(data.create_time) || (data.create_time as number) < 0 || !Number.isSafeInteger(data.update_time) || (data.update_time as number) < 0) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { deviceId: input.deviceId, name: data.name, online: data.online, category: typeof data.category === 'string' ? data.category : '', productId: typeof data.product_id === 'string' ? data.product_id : '', createdAtUnix: data.create_time as number, updatedAtUnix: data.update_time as number });
  }
}

function validCredential(value: unknown): value is string { return typeof value === 'string' && !!value && value.length <= 512 && /^[!-~]+$/.test(value); }
function validDeviceId(value: unknown): value is string { return typeof value === 'string' && /^[a-zA-Z0-9_-]{1,128}$/.test(value); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeJson(body: Uint8Array): unknown { try { return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return undefined; } }
function classifyStatus(status: number, phase: 'token' | 'device'): QueryAttempt<GetDeviceDetailsOutput> { return status === 404 && phase === 'device' ? branch('notFound', 'NOT_FOUND') : branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION'); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'tuya', operation: 'getDeviceDetails', kind, message: 'Tuya device query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetDeviceDetailsOutput> { return newQueryBranch(name, { deviceId: '', name: '', online: false, category: '', productId: '', createdAtUnix: 0, updatedAtUnix: 0 }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetDeviceDetailsOutput> { return newQueryRetry(failure(kind), delay); }
