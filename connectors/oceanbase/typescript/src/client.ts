// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, randomBytes } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListInstancesDefinition } from './generated/definitions.js';
import type { Config, Credentials, InstanceInfo, ListInstancesInput, ListInstancesOutput } from './generated/models.js';

interface DigestChallenge { realm: string; nonce: string; opaque: string; algorithm: string }
interface Exchange { response?: ProviderResponse; error?: 'input' | 'credential' | 'transport' | 'unsupportedChallenge' | 'nonce' | 'tooLarge'; retryAfterMs: number; pageNumber: number; pageSize: number; credentials?: string[] }
/** Injectable provider transport, clock, and random source for tests. */
export interface OceanbaseClientOptions {
  /** Send one provider request. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Return sixteen random bytes for a request ID or Digest nonce. */ nonce?: () => Uint8Array;
  /** Return the current time for Retry-After parsing. */ now?: () => Date;
}
/** Read OceanBase Cloud instance pages with HTTP Digest authentication. */
export class OceanbaseClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly nonce: () => Uint8Array;
  private readonly now: () => Date;

  /** Validate the fixed provider site and bounded request settings. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: OceanbaseClientOptions = {}) {
    if (!config || !credentials || (config.site !== undefined && !['domestic', 'international'].includes(config.site))) throw new TypeError('OceanBase Cloud configuration and credentials are required');
    this.baseUrl = config.site === 'international' ? 'https://api-cloud.oceanbase.com' : 'https://api-cloud-cn.oceanbase.com';
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('OceanBase Cloud request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.nonce = options.nonce ?? (() => randomBytes(16));
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the instance-list Query. */ listInstances(): ListInstancesOperation { return new ListInstancesOperation(this); }

  /** Resolve credentials and complete one Digest challenge exchange. */
  async request(call: Call, input: ListInstancesInput): Promise<Exchange> {
    const pageNumber = input?.pageNumber || 1;
    const pageSize = input?.pageSize || 10;
    const initial = { retryAfterMs: 0, pageNumber, pageSize };
    if (!Number.isSafeInteger(pageNumber) || pageNumber < 1 || !Number.isSafeInteger(pageSize) || pageSize < 1 || pageSize > 100) return { ...initial, error: 'input' };
    let fields: Credentials;
    try { fields = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { ...initial, error: 'credential' }; }
    const accessKey = fields.accessKeyId instanceof SecretString ? fields.accessKeyId.reveal() : '';
    const secret = fields.accessKeySecret instanceof SecretString ? fields.accessKeySecret.reveal() : '';
    const projectId = fields.projectId instanceof SecretString ? fields.projectId.reveal() : '';
    if (![accessKey, secret].every(validSecret) || !/^[A-Za-z0-9_-]{1,128}$/.test(projectId)) return { ...initial, error: 'credential' };
    let requestBytes: Uint8Array;
    try { requestBytes = this.nonce(); } catch { return { ...initial, error: 'nonce' }; }
    if (requestBytes.length !== 16) return { ...initial, error: 'nonce' };
    const requestId = uuidFromBytes(requestBytes);
    const uri = `/api/v2/instances?pageNumber=${pageNumber}&pageSize=${pageSize}&requestId=${requestId}`;
    const url = this.baseUrl + uri;
    let authorization = '';
    let response: ProviderResponse;
    let tooLarge = false;
    for (let attempt = 0; attempt < 2; attempt++) {
      const headers = { Accept: 'application/json', 'X-Ob-Project-Id': projectId, ...(authorization ? { Authorization: authorization } : {}) };
      const request: ProviderRequest = { url, method: 'GET', headers, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
      try { response = await this.send(request); }
      catch (error) {
        if (error instanceof BodyTooLargeError && error.status !== undefined) { response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }; tooLarge = true; }
        else return { ...initial, error: 'transport' };
      }
      if (response.status !== 401 || attempt === 1) break;
      const challenge = parseDigestChallenge(header(response.headers, 'www-authenticate'));
      if (!challenge) return { ...initial, error: 'unsupportedChallenge' };
      let nonceBytes: Uint8Array;
      try { nonceBytes = this.nonce(); } catch { return { ...initial, error: 'nonce' }; }
      if (nonceBytes.length !== 16) return { ...initial, error: 'nonce' };
      authorization = digestAuthorization(challenge, accessKey, secret, uri, Buffer.from(nonceBytes).toString('hex'));
    }
    return { response: response!, error: tooLarge || response!.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response!.headers, 'retry-after'), this.now()), pageNumber, pageSize, credentials: [accessKey, secret, projectId] };
  }
}

/** Classify one OceanBase Cloud instance page. */
export class ListInstancesOperation implements Query<ListInstancesInput, ListInstancesOutput> {
  /** Bind a validated provider client. */ constructor(private readonly client: OceanbaseClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListInstancesDefinition; }
  /** Complete Digest authentication and validate the provider response. */
  async invoke(call: Call, input: ListInstancesInput): Promise<QueryAttempt<ListInstancesOutput>> {
    const exchange = await this.client.request(call, input);
    if (exchange.error === 'input') return branch('defect', 'VALIDATION');
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'unsupportedChallenge') return branch('providerRejected', 'AUTHENTICATION');
    if (exchange.error === 'nonce') return branch('defect', 'AVAILABILITY');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || typeof wire.success !== 'boolean') return branch('invalidResponse', 'PROTOCOL');
    if (!wire.success) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!isRecord(wire.data) || !Array.isArray(wire.data.dataList) || !Number.isSafeInteger(wire.totalCount) || (wire.totalCount as number) < 0 || wire.data.dataList.length > exchange.pageSize) return branch('invalidResponse', 'PROTOCOL');
    const items: InstanceInfo[] = [];
    for (const raw of wire.data.dataList) {
      if (!isRecord(raw)) return branch('invalidResponse', 'PROTOCOL');
      const fields = [raw.instanceId, raw.instanceName, raw.state, raw.region];
      const limits = [256, 256, 64, 128];
      if (fields.some((value, index) => typeof value !== 'string' || !value || Buffer.byteLength(value) > limits[index]! || exchange.credentials?.some(secret => value.includes(secret)))) return branch('invalidResponse', 'PROTOCOL');
      items.push({ id: raw.instanceId as string, name: raw.instanceName as string, state: raw.state as string, region: raw.region as string });
    }
    return newQueryBranch('listed', { items, total: wire.totalCount as number, pageNumber: exchange.pageNumber, pageSize: exchange.pageSize });
  }
}

function uuidFromBytes(value: Uint8Array): string {
  const bytes = Uint8Array.from(value);
  bytes[6] = (bytes[6]! & 0x0f) | 0x40;
  bytes[8] = (bytes[8]! & 0x3f) | 0x80;
  const hex = Buffer.from(bytes).toString('hex');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
function parseDigestChallenge(headerValue: string): DigestChallenge | null {
  if (headerValue.length > 8192 || !/^Digest /i.test(headerValue)) return null;
  const values: Record<string, string> = {};
  let remaining = headerValue.slice(7).trim();
  while (remaining) {
    const match = /^([A-Za-z][A-Za-z0-9_-]*)\s*=\s*(?:"((?:\\.|[^"\\])*)"|([^,\s]+))\s*(?:,\s*|$)/.exec(remaining);
    if (!match) return null;
    const key = match[1]!.toLowerCase();
    const value = (match[2] ?? match[3] ?? '').replace(/\\(.)/g, '$1');
    if (values[key] !== undefined || !value || value.length > 2048 || [...value].some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127)) return null;
    values[key] = value;
    remaining = remaining.slice(match[0].length);
    if (match[0].endsWith(',') && !remaining) return null;
  }
  const algorithm = values.algorithm ?? 'MD5';
  if (!values.realm || !values.nonce || values.stale?.toLowerCase() === 'true' || !['MD5', 'MD5-SESS', 'SHA-256', 'SHA-256-SESS'].includes(algorithm.toUpperCase()) || !values.qop?.split(',').some(value => value.trim() === 'auth')) return null;
  return { realm: values.realm, nonce: values.nonce, opaque: values.opaque ?? '', algorithm };
}
function digestAuthorization(challenge: DigestChallenge, username: string, password: string, uri: string, cnonce: string): string {
  const hashAlgorithm = challenge.algorithm.toUpperCase().startsWith('SHA-256') ? 'sha256' : 'md5';
  const hash = (value: string) => createHash(hashAlgorithm).update(value).digest('hex');
  let ha1 = hash(`${username}:${challenge.realm}:${password}`);
  if (challenge.algorithm.toUpperCase().endsWith('-SESS')) ha1 = hash(`${ha1}:${challenge.nonce}:${cnonce}`);
  const ha2 = hash('GET:' + uri);
  const response = hash(`${ha1}:${challenge.nonce}:00000001:${cnonce}:auth:${ha2}`);
  const quote = (value: string) => `"${value.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`;
  return `Digest username=${quote(username)}, realm=${quote(challenge.realm)}, nonce=${quote(challenge.nonce)}, uri=${quote(uri)}, response=${quote(response)}, algorithm=${challenge.algorithm}, qop=auth, nc=00000001, cnonce=${quote(cnonce)}` + (challenge.opaque ? `, opaque=${quote(challenge.opaque)}` : '');
}
function validSecret(value: string): boolean { return !!value && value.length <= 512 && /^[!-~]+$/.test(value); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'oceanbase', operation: 'listInstances', kind, message: 'OceanBase Cloud instance query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListInstancesOutput> { return newQueryBranch(name, { items: [], total: 0, pageNumber: 0, pageSize: 0 }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListInstancesOutput> { return newQueryRetry(failure(kind), delay); }
