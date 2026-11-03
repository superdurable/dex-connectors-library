// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider, type Failure, type QueryAttempt, newQueryBranch, newQueryRetry } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL } from '@superdurable/connectors-sdk/providerhttp';
import { GetDeploymentDefinition, ListDeploymentsDefinition } from './generated/definitions.js';
import type { Config, Credentials, Deployment, DeploymentsPage, GetDeploymentInput, ListDeploymentsInput } from './generated/models.js';

const emptyDeployment: Deployment = { uid: '', id: '', name: '', url: '', state: '', readyState: '', created: 0 };
const emptyPage: DeploymentsPage = { deployments: [], pagination: { next: null } };
type ProviderReply = { status: number; headers: Record<string, string | string[] | undefined>; body: Uint8Array };
/** Replace the provider transport in deterministic tests. */
export type VercelRequestSender = typeof sendProviderRequest;
/** Optional provider transport dependency. */
export interface VercelClientOptions { /** HTTP sender for tests. */ send?: VercelRequestSender }

/** Vercel deployment queries bound to validated settings and call-time credentials. */
export class VercelClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: VercelRequestSender;
  /** Validate configuration without contacting Vercel. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: VercelClientOptions = {}) {
    if (!credentials) throw new TypeError('Vercel credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://api.vercel.com');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Vercel timeout must be positive');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Vercel maxResponseBytes must be positive');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the deployment list query. */ listDeployments(): ListDeploymentsOperation { return new ListDeploymentsOperation(this); }
  /** Bind the deployment detail query. */ getDeployment(): GetDeploymentOperation { return new GetDeploymentOperation(this); }

  /** Send one provider request and apply the Go connector's status classification. */
  async execute<Output>(call: Call, operation: string, path: string, empty: Output, success: string, notFound: string | undefined, decode: (body: Uint8Array) => Output): Promise<QueryAttempt<Output>> {
    let token: string;
    try {
      const credential = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credential.accessToken instanceof SecretString)) throw new TypeError('missing token');
      token = credential.accessToken.reveal();
    } catch { return branch(operation, 'defect', empty, 'AUTHENTICATION', 'connection credentials are unavailable'); }
    if (!token || !isHeaderSafeCredential(token)) return branch(operation, 'defect', empty, 'AUTHENTICATION', 'connection credentials are invalid');
    let response: ProviderReply;
    try {
      response = await this.send({ url: this.baseUrl + path, method: 'GET', headers: { Authorization: `Bearer ${token}`, Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: Math.max(this.maxResponseBytes, 65_536), addressPolicy: 'privateNetworkHTTP', signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined && error.status !== 200) return classifyStatus(operation, error.status, error.headers?.['retry-after'], empty, notFound);
      if (error instanceof BodyTooLargeError) return branch(operation, 'invalidResponse', empty, 'RESPONSE_TOO_LARGE', 'response exceeded the configured limit');
      return newQueryRetry(failure(operation, 'AVAILABILITY', 'Vercel request failed'));
    }
    if (response.status !== 200) return classifyStatus(operation, response.status, response.headers['retry-after'], empty, notFound);
    if (response.body.length > this.maxResponseBytes) return branch(operation, 'invalidResponse', empty, 'RESPONSE_TOO_LARGE', 'response exceeded the configured limit');
    try { return newQueryBranch(success, decode(response.body), {}); }
    catch { return branch(operation, 'invalidResponse', empty, 'PROTOCOL', 'response was malformed'); }
  }
}

/** Query Vercel's deployments page. */
export class ListDeploymentsOperation {
  /** Bind one client. */ constructor(private readonly client: VercelClient) {}
  /** Return stable metadata. */ definition() { return ListDeploymentsDefinition; }
  /** Invoke one list attempt. */ invoke(call: Call, input: ListDeploymentsInput): Promise<QueryAttempt<DeploymentsPage>> {
    const query = input.teamId ? `?teamId=${goQueryEscape(input.teamId)}` : '';
    return this.client.execute(call, 'listDeployments', `/v6/deployments${query}`, emptyPage, 'listed', undefined, decodePage);
  }
}

/** Query one deployment by ID or hostname. */
export class GetDeploymentOperation {
  /** Bind one client. */ constructor(private readonly client: VercelClient) {}
  /** Return stable metadata. */ definition() { return GetDeploymentDefinition; }
  /** Invoke one detail attempt. */ invoke(call: Call, input: GetDeploymentInput): Promise<QueryAttempt<Deployment>> {
    if (!input.idOrUrl?.trim()) return Promise.resolve(branch('getDeployment', 'defect', emptyDeployment, 'VALIDATION', 'idOrUrl is required'));
    const query = input.teamId ? `?teamId=${goQueryEscape(input.teamId)}` : '';
    return this.client.execute(call, 'getDeployment', `/v13/deployments/${goPathEscape(input.idOrUrl)}${query}`, emptyDeployment, 'found', 'notFound', decodeDeployment);
  }
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'vercel', operation, message }; }
function branch<Output>(operation: string, branchId: string, empty: Output, kind: Failure['kind'], message: string): QueryAttempt<Output> { return newQueryBranch(branchId, empty, {}, failure(operation, kind, message)); }
function classifyStatus<Output>(operation: string, status: number, retryAfter: string | string[] | undefined, empty: Output, notFound?: string): QueryAttempt<Output> {
  if (status === 408 || status === 409 || status === 429 || status >= 500) return newQueryRetry(failure(operation, status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', `Vercel returned HTTP ${status}`), parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '', new Date()));
  const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 404 ? 'NOT_FOUND' : status === 400 || status === 422 ? 'VALIDATION' : 'PROVIDER_REJECTION';
  return branch(operation, status === 404 && notFound ? notFound : 'providerRejected', empty, kind, `Vercel returned HTTP ${status}`);
}
function goPathEscape(value: string): string { return [...Buffer.from(value)].map(byte => { const character = String.fromCharCode(byte); return /^[A-Za-z0-9._~\-$&+=:@]$/.test(character) ? character : `%${byte.toString(16).toUpperCase().padStart(2, '0')}`; }).join(''); }
function goQueryEscape(value: string): string { return encodeURIComponent(value).replace(/%20/g, '+').replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/~/g, '%7E'); }
function object(value: unknown): Record<string, unknown> { if (value === null || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('expected object'); return value as Record<string, unknown>; }
function stringField(value: unknown): string { if (value === undefined || value === null) return ''; if (typeof value !== 'string') throw new TypeError('expected string'); return value; }
function integerField(value: unknown): number { if (value === undefined || value === null) return 0; if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError('expected integer'); return value; }
function decodeDeploymentObject(value: unknown): Deployment { const row = object(value); return { uid: stringField(row.uid), id: stringField(row.id), name: stringField(row.name), url: stringField(row.url), state: stringField(row.state), readyState: stringField(row.readyState), created: integerField(row.created) }; }
function parseBody(body: Uint8Array): unknown { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; }
function decodeDeployment(body: Uint8Array): Deployment { const deployment = decodeDeploymentObject(parseBody(body)); if (!deployment.uid && !deployment.id) throw new TypeError('missing deployment identifier'); return deployment; }
function decodePage(body: Uint8Array): DeploymentsPage { const row = object(parseBody(body)); if (!Array.isArray(row.deployments)) throw new TypeError('missing deployments'); const pagination = row.pagination === undefined || row.pagination === null ? {} : object(row.pagination); return { deployments: row.deployments.map(decodeDeploymentObject), pagination: { next: pagination.next === null || pagination.next === undefined ? null : integerField(pagination.next) } }; }
