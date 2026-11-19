// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, SecretString, zeroReceipt, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListAccessibleProjectsDefinition } from './generated/definitions.js';
import type { AccessibleProject, Config, CredentialSource, ListAccessibleProjectsInput, ListAccessibleProjectsOutput } from './generated/models.js';

const iamEndpoint = 'https://iam.myhuaweicloud.com';

/** Provider transport override for deterministic IAM tests. */
export interface HuaweiCloudClientOptions { /** SDK transport when omitted. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Huawei Cloud IAM client that keeps tokens and passwords outside Dex state. */
export class HuaweiCloudClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate limits without contacting IAM. */
  constructor(config: Config, private readonly credentials: CredentialSource, options: HuaweiCloudClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Huawei Cloud configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Huawei Cloud request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the listAccessibleProjects Query. */
  listAccessibleProjects(): ListAccessibleProjectsOperation { return new ListAccessibleProjectsOperation(this); }

  /** Resolve IAM user credentials per call. */
  async resolveIdentity(call: Call): Promise<{ accountName: string; iamUsername: string; iamPassword: string } | undefined> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      const accountName = credentials.accountName instanceof SecretString ? credentials.accountName.reveal() : '';
      const iamUsername = credentials.iamUsername instanceof SecretString ? credentials.iamUsername.reveal() : '';
      const iamPassword = credentials.iamPassword instanceof SecretString ? credentials.iamPassword.reveal() : '';
      return [accountName, iamUsername, iamPassword].every(validCredential) ? { accountName, iamUsername, iamPassword } : undefined;
    } catch { return undefined; }
  }

  /** Send one bounded IAM request with no redirects or token persistence. */
  async request(call: Call, method: 'GET' | 'POST', path: string, body?: Uint8Array, token?: string):
    Promise<{ response?: ProviderResponse; tooLarge?: boolean; transportFailed?: boolean }> {
    const headers: Record<string, string> = { Accept: 'application/json', 'Content-Type': 'application/json;charset=utf8' };
    if (token) headers['X-Auth-Token'] = token;
    const request: ProviderRequest = {
      url: iamEndpoint + path, method, headers, ...(body ? { body } : {}),
      timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal,
    };
    try { return { response: await this.send(request) }; }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined)
        return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, tooLarge: true };
      return { transportFailed: true };
    }
  }
}

/** Authenticate once and list IAM projects accessible to that user. */
export class ListAccessibleProjectsOperation implements Query<ListAccessibleProjectsInput, ListAccessibleProjectsOutput> {
  /** Bind the client. */ constructor(private readonly client: HuaweiCloudClient) {}
  /** Return the generated Query definition. */ definition() { return ListAccessibleProjectsDefinition; }
  /** Classify token and project requests without returning secrets. */
  async invoke(call: Call, input: ListAccessibleProjectsInput): Promise<QueryAttempt<ListAccessibleProjectsOutput>> {
    void input;
    const identity = await this.client.resolveIdentity(call);
    if (!identity) return terminal('defect', 'AUTHENTICATION', 'IAM credentials are unavailable or invalid');
    const body = new TextEncoder().encode(JSON.stringify({ auth: {
      identity: { methods: ['password'], password: { user: {
        domain: { name: identity.accountName }, name: identity.iamUsername, password: identity.iamPassword,
      } } },
      scope: { domain: { name: identity.accountName } },
    } }));
    const first = await this.client.request(call, 'POST', '/v3/auth/tokens', body);
    const firstDecision = classifyRequest(first);
    if (firstDecision) return firstDecision;
    const tokenResponse = first.response!;
    if (tokenResponse.status !== 201) return rejectedStatus(tokenResponse.status, 'token');
    const token = headerValue(tokenResponse.headers, 'x-subject-token');
    if (!validToken(token)) return terminal('invalidResponse', 'PROTOCOL', 'IAM returned an invalid token header');
    const second = await this.client.request(call, 'GET', '/v3/auth/projects', undefined, token);
    const secondDecision = classifyRequest(second);
    if (secondDecision) return secondDecision;
    const projectsResponse = second.response!;
    if (projectsResponse.status !== 200) return rejectedStatus(projectsResponse.status, 'projects');
    let value: unknown;
    try { value = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(projectsResponse.body)); }
    catch { return terminal('invalidResponse', 'PROTOCOL', 'IAM returned invalid project data'); }
    if (!value || typeof value !== 'object' || Array.isArray(value)) return terminal('invalidResponse', 'PROTOCOL', 'IAM returned invalid project data');
    const records = (value as Record<string, unknown>).projects;
    if (!Array.isArray(records)) return terminal('invalidResponse', 'PROTOCOL', 'IAM returned invalid project data');
    const projects: AccessibleProject[] = [];
    for (const item of records) {
      if (!item || typeof item !== 'object' || Array.isArray(item)) return terminal('invalidResponse', 'PROTOCOL', 'IAM returned invalid project data');
      const fields = item as Record<string, unknown>;
      if (typeof fields.id !== 'string' || !fields.id || typeof fields.enabled !== 'boolean' ||
          (fields.name !== undefined && typeof fields.name !== 'string') || (fields.domain_id !== undefined && typeof fields.domain_id !== 'string'))
        return terminal('invalidResponse', 'PROTOCOL', 'IAM returned invalid project data');
      projects.push({ id: fields.id, name: (fields.name as string | undefined) ?? '', enabled: fields.enabled, domainId: (fields.domain_id as string | undefined) ?? '' });
    }
    return newQueryBranch('found', { projects }, zeroReceipt());
  }
}

function classifyRequest(result: { response?: ProviderResponse; tooLarge?: boolean; transportFailed?: boolean }): QueryAttempt<ListAccessibleProjectsOutput> | undefined {
  if (result.transportFailed || !result.response) return newQueryRetry(failure('AVAILABILITY', 'Huawei Cloud IAM request failed'));
  const response = result.response;
  const retryAfter = retryDelay(response);
  if (response.status === 429) return newQueryRetry(failure('RATE_LIMIT', 'Huawei Cloud IAM rate limited the request'), retryAfter);
  if (response.status === 408 || response.status >= 500) return newQueryRetry(failure('AVAILABILITY', 'Huawei Cloud IAM is temporarily unavailable'), retryAfter);
  if (result.tooLarge) return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Huawei Cloud IAM response exceeded the configured limit');
  return undefined;
}
function rejectedStatus(status: number, phase: string): QueryAttempt<ListAccessibleProjectsOutput> {
  const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION';
  return terminal('providerRejected', kind, 'Huawei Cloud IAM rejected the ' + phase + ' request with HTTP ' + status);
}
function validCredential(value: string): boolean { return value.length > 0 && value.length <= 512 && ![...value].some(character => character.charCodeAt(0) <= 31 || character.charCodeAt(0) === 127) && value.trim() === value; }
function validToken(value: string): boolean { return value.length > 0 && value.length < 32_768 && /^[!-~]+$/.test(value); }
function headerValue(headers: ProviderResponse['headers'], name: string): string {
  const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}
function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: 'huaweicloud', operation: 'listAccessibleProjects', message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<ListAccessibleProjectsOutput> { return newQueryBranch(branch, { projects: [] }, zeroReceipt(), failure(kind, message)); }
function retryDelay(response: ProviderResponse): number {
  const value = headerValue(response.headers, 'retry-after');
  return parseRetryAfter(value) || 0;
}
