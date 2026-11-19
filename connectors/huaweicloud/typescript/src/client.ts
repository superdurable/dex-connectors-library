// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListAccessibleProjectsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListAccessibleProjectsInput, ListAccessibleProjectsOutput } from './generated/models.js';

/** Optional transport for local IAM tests. */
export interface HuaweiCloudClientOptions {
  /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

type Exchange = { response?: ProviderResponse; error?: 'transport' | 'tooLarge'; retryAfterMs: number };

/** Read Huawei Cloud IAM projects with short-lived credentials outside Flow state. */
export class HuaweiCloudClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate IAM request bounds without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: HuaweiCloudClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Huawei Cloud configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Huawei Cloud request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the project-list Query. */ listAccessibleProjects(): ListAccessibleProjectsOperation { return new ListAccessibleProjectsOperation(this); }

  /** Resolve IAM username, account, and password for one call. */
  async getCredentials(call: Call): Promise<{ accountName: string; iamUsername: string; iamPassword: string } | undefined> {
    try {
      const fields = await this.credentials.resolve(call, call.context.cancellationSignal);
      const accountName = fields.accountName instanceof SecretString ? fields.accountName.reveal() : '';
      const iamUsername = fields.iamUsername instanceof SecretString ? fields.iamUsername.reveal() : '';
      const iamPassword = fields.iamPassword instanceof SecretString ? fields.iamPassword.reveal() : '';
      return [accountName, iamUsername, iamPassword].every(validCredential) ? { accountName, iamUsername, iamPassword } : undefined;
    } catch { return undefined; }
  }

  /** Send one bounded IAM token or project request. */
  async request(call: Call, method: 'POST' | 'GET', path: string, body?: Uint8Array, token = ''): Promise<Exchange> {
    const headers: Record<string, string> = { 'Content-Type': 'application/json;charset=utf8', Accept: 'application/json' };
    if (token) headers['X-Auth-Token'] = token;
    const request: ProviderRequest = { url: `https://iam.myhuaweicloud.com${path}`, method, headers, ...(body ? { body } : {}), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Authenticate to IAM and list the caller's accessible projects. */
export class ListAccessibleProjectsOperation implements Query<ListAccessibleProjectsInput, ListAccessibleProjectsOutput> {
  /** Bind a validated IAM client. */ constructor(private readonly client: HuaweiCloudClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListAccessibleProjectsDefinition; }
  /** Classify each IAM request and expose only safe project metadata. */
  async invoke(call: Call): Promise<QueryAttempt<ListAccessibleProjectsOutput>> {
    const credentials = await this.client.getCredentials(call);
    if (!credentials) return branch('defect', 'AUTHENTICATION');
    const body = Buffer.from(JSON.stringify({ auth: { identity: { methods: ['password'], password: { user: { domain: { name: credentials.accountName }, name: credentials.iamUsername, password: credentials.iamPassword } } }, scope: { domain: { name: credentials.accountName } } } }));
    const tokenExchange = await this.client.request(call, 'POST', '/v3/auth/tokens', body);
    const tokenFailure = classifyExchange(tokenExchange, 201);
    if (tokenFailure) return tokenFailure;
    const token = header(tokenExchange.response!.headers, 'x-subject-token');
    if (!validToken(token)) return branch('invalidResponse', 'PROTOCOL');
    const projectExchange = await this.client.request(call, 'GET', '/v3/auth/projects', undefined, token);
    const projectFailure = classifyExchange(projectExchange, 200);
    if (projectFailure) return projectFailure;
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(projectExchange.response!.body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isObject(wire) || !Array.isArray(wire.projects)) return branch('invalidResponse', 'PROTOCOL');
    const projects: ListAccessibleProjectsOutput['projects'] = [];
    for (const project of wire.projects) {
      if (!isObject(project) || typeof project.id !== 'string' || !project.id || typeof project.name !== 'string' || typeof project.enabled !== 'boolean' || typeof project.domain_id !== 'string' || !project.domain_id) return branch('invalidResponse', 'PROTOCOL');
      projects.push({ id: project.id, name: project.name, enabled: project.enabled, domainId: project.domain_id });
    }
    return newQueryBranch('found', { projects });
  }
}

function classifyExchange(exchange: Exchange, expectedStatus: number): QueryAttempt<ListAccessibleProjectsOutput> | undefined {
  if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
  const { status } = exchange.response;
  if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
  if (status !== expectedStatus) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
  return undefined;
}
function validCredential(value: string): boolean { return !!value && value.length <= 512 && [...value].every(character => character.charCodeAt(0) >= 32 && character.charCodeAt(0) !== 127) && value.trim() === value; }
function validToken(value: string): boolean { return !!value && value.length < 32768 && /^[\x21-\x7e]+$/.test(value); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'huaweicloud', operation: 'listAccessibleProjects', kind, message: 'Huawei Cloud IAM query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListAccessibleProjectsOutput> { return newQueryBranch(name, { projects: [] }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListAccessibleProjectsOutput> { return newQueryRetry(failure(kind), delay); }
