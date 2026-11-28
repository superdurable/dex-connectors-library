// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListSpacesDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListSpacesInput, SpacePage } from './generated/models.js';

const emptyOutput: SpacePage = { spaces: [] };
/** Send one bounded workspace request. */
export type SpaceRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional transport dependency for provider tests. */
export interface VikaClientOptions { send?: SpaceRequestSender }
/** Resolve Vika credentials for each request. */
export class VikaClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: SpaceRequestSender;
  /** Validate configuration without contacting Vika. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: VikaClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Vika configuration and credentials are required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://vika.cn');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Vika configuration is invalid');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the list-spaces Query. */ listSpaces(): ListSpacesOperation { return new ListSpacesOperation(this); }
}
/** List only the workspace metadata visible to the token. */
export class ListSpacesOperation implements Query<ListSpacesInput, SpacePage> {
  /** Bind a configured Vika client. */ constructor(private readonly client: VikaClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListSpacesDefinition; }
  /** Send and classify one workspace request. */
  async invoke(call: Call, input: ListSpacesInput): Promise<QueryAttempt<SpacePage>> {
    if (input === null || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION', 'Vika workspace request is invalid');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'Vika connection token is unavailable or invalid'); }
    const token = credentials.apiToken instanceof SecretString ? credentials.apiToken.reveal() : '';
    if (!token || token.length > 512 || !/^[!-~]+$/.test(token) || /^bearer /i.test(token)) return branch('defect', 'AUTHENTICATION', 'Vika connection token is unavailable or invalid');
    let response: ProviderResponse;
    let oversized = false;
    try { response = await this.client.send({ url: `${this.client.baseUrl}/fusion/v1/spaces`, method: 'GET', headers: { Accept: 'application/json', Authorization: `Bearer ${token}` }, timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: this.client.baseUrl.startsWith('https:') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal }); }
    catch (error) {
      if (!(error instanceof BodyTooLargeError) || error.status === undefined) return retry('AVAILABILITY', 'Vika request failed');
      response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
      oversized = true;
    }
    if (response.status === 408 || response.status === 429 || response.status >= 500) return classifyStatus(response.status, response.headers);
    if (oversized || response.body.length > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Vika response exceeded the configured limit');
    if (response.status !== 200) return classifyStatus(response.status, response.headers);
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'Vika returned an invalid workspace response'); }
    if (!isRecord(wire) || typeof wire.success !== 'boolean' || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL', 'Vika returned an invalid workspace response');
    const code = wire.code as number;
    if (!wire.success) {
      if (code === 200) return newQueryBranch('listed', { spaces: [] });
      if (code === 429 || code >= 500) return classifyStatus(code, response.headers);
      return branch('providerRejected', 'PROVIDER_REJECTION', `Vika rejected the query with code ${code}`);
    }
    if (code !== 200 || !isRecord(wire.data) || !Array.isArray(wire.data.spaces) || wire.data.spaces.length > 1000) return branch('invalidResponse', 'PROTOCOL', 'Vika returned an invalid workspace response');
    const spaces: NonNullable<SpacePage['spaces']> = [];
    for (const item of wire.data.spaces) {
      if (!isRecord(item) || typeof item.id !== 'string' || !item.id.startsWith('spc') || typeof item.name !== 'string' || !item.name || (item.isAdmin != null && typeof item.isAdmin !== 'boolean')) return branch('invalidResponse', 'PROTOCOL', 'Vika returned an invalid workspace response');
      spaces.push({ id: item.id, name: item.name, isAdmin: item.isAdmin === true });
    }
    return newQueryBranch('listed', { spaces });
  }
}
function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<SpacePage> {
  const value = Object.entries(headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
  const delay = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '');
  if (status === 429) return retry('RATE_LIMIT', 'Vika rate limited the request', delay);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', 'Vika is temporarily unavailable', delay);
  const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 400 || status === 404 ? 'VALIDATION' : 'PROVIDER_REJECTION';
  return branch('providerRejected', kind, `Vika rejected the query with HTTP ${status}`);
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'vika', operation: 'listSpaces', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<SpacePage> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<SpacePage> { return newQueryRetry(failure(kind, message), delayMs); }
