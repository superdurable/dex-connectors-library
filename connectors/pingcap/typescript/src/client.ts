// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, randomBytes } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListProjectsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListProjectsInput, ListProjectsOutput, ProjectInfo } from './generated/models.js';

const emptyOutput: ListProjectsOutput = { items: [], total: 0, page: 0, pageSize: 0 };
interface DigestChallenge { realm: string; nonce: string; opaque: string; algorithm: string; }
/** Optional provider transport and nonce source for deterministic tests. */
export interface PingcapClientOptions { /** Provider HTTP sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; /** Sixteen random Digest nonce bytes. */ nonce?: () => Uint8Array; }
/** Read TiDB Cloud project pages with HTTP Digest authentication. */
export class PingcapClient {
  private readonly baseUrl: string; private readonly timeoutMs: number; private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>; private readonly nonce: () => Uint8Array;
  /** Validate connection settings without contacting TiDB Cloud. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: PingcapClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('TiDB Cloud configuration and credential source are required');
    this.baseUrl = config.baseUrl ?? 'https://api.tidbcloud.com';
    if (!/^https:\/\/[^/?#]+$/.test(this.baseUrl)) throw new TypeError('TiDB Cloud base URL must be an HTTPS origin');
    this.timeoutMs = config.timeoutMs ?? 10_000; this.maxResponseBytes = config.maxResponseBytes ?? 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('TiDB Cloud request limits are invalid');
    this.send = options.send ?? sendProviderRequest; this.nonce = options.nonce ?? (() => randomBytes(16));
  }
  /** Bind the project-list Query. */ listProjects(): ListProjectsOperation { return new ListProjectsOperation(this); }
  /** Resolve credentials and execute one Digest challenge exchange. */
  async request(call: Call, input: ListProjectsInput): Promise<{ response?: ProviderResponse; error?: 'input' | 'credential' | 'transport' | 'unsupportedChallenge' | 'nonce' | 'tooLarge'; retryAfterMs: number; page: number; pageSize: number }> {
    const page = input?.page || 1; const pageSize = input?.pageSize || 10;
    if (!Number.isSafeInteger(page) || page < 1 || !Number.isSafeInteger(pageSize) || pageSize < 1 || pageSize > 100) return { error: 'input', retryAfterMs: 0, page, pageSize };
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); } catch { return { error: 'credential', retryAfterMs: 0, page, pageSize }; }
    const username = credentials.publicKey instanceof SecretString ? credentials.publicKey.reveal() : '';
    const password = credentials.privateKey instanceof SecretString ? credentials.privateKey.reveal() : '';
    if (![username, password].every(value => !!value && value.length <= 2048 && /^[!-~]+$/.test(value))) return { error: 'credential', retryAfterMs: 0, page, pageSize };
    const uri = `/api/v1beta/projects?page=${page}&page_size=${pageSize}`;
    const url = this.baseUrl + uri;
    let authorization = '';
    let response: ProviderResponse;
    let tooLarge = false;
    for (let attempt = 0; attempt < 2; attempt++) {
      const request: ProviderRequest = { url, method: 'GET', headers: authorization ? { Accept: 'application/json', Authorization: authorization } : { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
      try { response = await this.send(request); } catch (error) {
        if (error instanceof BodyTooLargeError && error.status !== undefined) { response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }; tooLarge = true; }
        else return { error: 'transport', retryAfterMs: 0, page, pageSize };
      }
      if (response.status !== 401 || attempt === 1) break;
      const challengeHeader = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'www-authenticate')?.[1];
      const challenge = parseDigestChallenge(Array.isArray(challengeHeader) ? challengeHeader[0] ?? '' : challengeHeader ?? '');
      if (!challenge) return { error: 'unsupportedChallenge', retryAfterMs: 0, page, pageSize };
      let nonceBytes: Uint8Array;
      try { nonceBytes = this.nonce(); } catch { return { error: 'nonce', retryAfterMs: 0, page, pageSize }; }
      if (nonceBytes.length !== 16) return { error: 'nonce', retryAfterMs: 0, page, pageSize };
      authorization = digestAuthorization(challenge, username, password, uri, Buffer.from(nonceBytes).toString('hex'));
    }
    const retryAfterValue = Object.entries(response!.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    return { response: response!, error: tooLarge || response!.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfterValue) ? retryAfterValue[0] ?? '' : retryAfterValue ?? ''), page, pageSize };
  }
}
/** Classify a TiDB Cloud project page. */
export class ListProjectsOperation implements Query<ListProjectsInput, ListProjectsOutput> {
  /** Bind one validated client. */ constructor(private readonly client: PingcapClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListProjectsDefinition; }
  /** Complete Digest authentication and validate one page. */
  async invoke(call: Call, input: ListProjectsInput): Promise<QueryAttempt<ListProjectsOutput>> {
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
    if (!isRecord(wire) || !Array.isArray(wire.items) || !Number.isSafeInteger(wire.total) || (wire.total as number) < 0 || wire.items.length > exchange.pageSize) return branch('invalidResponse', 'PROTOCOL');
    const items: ProjectInfo[] = [];
    for (const raw of wire.items) {
      if (!isRecord(raw) || !['id', 'org_id', 'name'].every(key => typeof raw[key] === 'string' && raw[key]) || !Number.isSafeInteger(raw.cluster_count ?? 0) || (raw.cluster_count as number) < 0 || !Number.isSafeInteger(raw.user_count ?? 0) || (raw.user_count as number) < 0) return branch('invalidResponse', 'PROTOCOL');
      items.push({ id: raw.id as string, orgId: raw.org_id as string, name: raw.name as string, clusterCount: raw.cluster_count as number ?? 0, userCount: raw.user_count as number ?? 0, createTimestamp: typeof raw.create_timestamp === 'string' ? raw.create_timestamp : '', ...(typeof raw.type === 'string' && raw.type ? { type: raw.type } : {}) });
    }
    return newQueryBranch('listed', { items, total: wire.total as number, page: exchange.page, pageSize: exchange.pageSize });
  }
}
function parseDigestChallenge(header: string): DigestChallenge | null {
  if (header.length > 8192 || !/^Digest /i.test(header)) return null;
  const values: Record<string, string> = {};
  let remaining = header.slice(7).trim();
  while (remaining) {
    const match = /^([A-Za-z][A-Za-z0-9_-]*)\s*=\s*(?:"((?:\\.|[^"\\])*)"|([^,\s]+))\s*(?:,\s*|$)/.exec(remaining);
    if (!match) return null;
    const key = match[1]!.toLowerCase(); const value = (match[2] ?? match[3] ?? '').replace(/\\(.)/g, '$1');
    if (values[key] !== undefined || !value || value.length > 2048 || [...value].some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127)) return null;
    values[key] = value; remaining = remaining.slice(match[0].length);
    if (match[0].endsWith(',') && !remaining) return null;
  }
  const algorithm = values.algorithm ?? 'MD5';
  if (!values.realm || !values.nonce || values.stale?.toLowerCase() === 'true' || !['MD5', 'MD5-SESS', 'SHA-256', 'SHA-256-SESS'].includes(algorithm.toUpperCase()) || !values.qop?.split(',').some(value => value.trim() === 'auth')) return null;
  return { realm: values.realm, nonce: values.nonce, opaque: values.opaque ?? '', algorithm };
}
function digestAuthorization(challenge: DigestChallenge, username: string, password: string, uri: string, cnonce: string): string {
  const hashAlgorithm = challenge.algorithm.toUpperCase().startsWith('SHA-256') ? 'sha256' : 'md5';
  const hash = (text: string) => createHash(hashAlgorithm).update(text).digest('hex');
  let ha1 = hash(`${username}:${challenge.realm}:${password}`);
  if (challenge.algorithm.toUpperCase().endsWith('-SESS')) ha1 = hash(`${ha1}:${challenge.nonce}:${cnonce}`);
  const ha2 = hash('GET:' + uri);
  const response = hash(`${ha1}:${challenge.nonce}:00000001:${cnonce}:auth:${ha2}`);
  const quote = (value: string) => `"${value.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`;
  return `Digest username=${quote(username)}, realm=${quote(challenge.realm)}, nonce=${quote(challenge.nonce)}, uri=${quote(uri)}, response=${quote(response)}, algorithm=${challenge.algorithm}, qop=auth, nc=00000001, cnonce=${quote(cnonce)}` + (challenge.opaque ? `, opaque=${quote(challenge.opaque)}` : '');
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'pingcap', operation: 'listProjects', kind, message: 'TiDB Cloud provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListProjectsOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListProjectsOutput> { return newQueryRetry(failure(kind), delay); }
