// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetWorkItemDefinition, ListProjectsDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetWorkItemInput, ListProjectsInput, ProjectMetadata, Projects, WorkItemMetadata } from './generated/models.js';

const emptyProjects = (): Projects => ({ projects: [], top: 0, skip: 0 });
const emptyWorkItem = (): WorkItemMetadata => ({ id: 0, title: '', state: '', workItemType: '' });
const userAgent = 'Dex Connector (https://superdurable.io)';

/** Optional transport dependency for provider tests and local Dex examples. */
export interface AzureDevOpsClientOptions {
  /** One bounded provider request sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface ProviderOutcome {
  body?: Uint8Array;
  branch?: string;
  failure?: Failure;
  retry?: boolean;
  retryAfterMs?: number;
}

/** Authenticated Azure DevOps API 7.1 client with call-time credential resolution. */
export class AzureDevOpsClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection configuration without contacting Azure DevOps. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: AzureDevOpsClientOptions = {}) {
    if (!credentials) throw new TypeError('Azure DevOps credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Azure DevOps timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Azure DevOps response limit is invalid');
    this.sendRequest = options.send ?? sendProviderRequest;
  }

  /** Bind the project page Query. */ listProjects(): ListProjectsOperation { return new ListProjectsOperation(this); }
  /** Bind the work item Query. */ getWorkItem(): GetWorkItemOperation { return new GetWorkItemOperation(this); }

  /** Send one read and classify transport, HTTP, and response-size outcomes. */
  async exchange(call: Call, operation: string, path: string, hasNotFound: boolean): Promise<ProviderOutcome> {
    let token: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = resolved.personalAccessToken instanceof SecretString ? resolved.personalAccessToken.reveal() : '';
    } catch { return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') }; }
    if (!isHeaderSafeCredential(token)) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    const authorization = Buffer.from(`:${token}`).toString('base64');
    const request: ProviderRequest = {
      url: `${this.baseUrl}${path}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Basic ${authorization}`, 'User-Agent': userAgent },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        if (response.status === 200) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Azure DevOps response exceeded the configured limit') };
      } else return { retry: true, failure: failure(operation, 'TRANSPORT', 'Azure DevOps request failed') };
    }
    const status = response.status;
    const retryAfter = parseRetryAfter(headerValue(response.headers['retry-after']));
    if (status === 429) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'RATE_LIMIT', 'Azure DevOps rate limited the request') };
    if (status === 408 || status >= 500) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'AVAILABILITY', 'Azure DevOps is temporarily unavailable') };
    if (status === 404 && hasNotFound) return { branch: 'notFound', failure: failure(operation, 'NOT_FOUND', 'Azure DevOps did not find the resource') };
    if (status !== 200) return { branch: 'providerRejected', failure: failure(operation, 'PROVIDER_REJECTION', `Azure DevOps rejected HTTP ${status}`) };
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Azure DevOps response exceeded the configured limit') };
    return { body: response.body };
  }
}

/** Read one bounded page of Azure DevOps projects. */
export class ListProjectsOperation implements Query<ListProjectsInput, Projects> {
  /** Bind the provider client. */ constructor(private readonly client: AzureDevOpsClient) {}
  /** Return the manifest-generated operation definition. */ definition(): QueryDefinition { return ListProjectsDefinition; }
  /** Read one project page. */
  async invoke(call: Call, input: ListProjectsInput): Promise<QueryAttempt<Projects>> {
    const skip = input?.skip ?? 0;
    if (!input || !Number.isSafeInteger(input.top) || input.top < 1 || input.top > 100 || !Number.isSafeInteger(skip) || skip < 0) return terminal('listProjects', 'defect', 'VALIDATION', 'project page size or offset is invalid', emptyProjects());
    const query = new URLSearchParams({ '$top': String(input.top), '$skip': String(skip), 'api-version': '7.1' });
    const result = await this.client.exchange(call, 'listProjects', `/_apis/projects?${query.toString()}`, false);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyProjects(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document) || !Array.isArray(document.value)) return invalid('listProjects', emptyProjects());
    const projects: ProjectMetadata[] = [];
    for (const value of document.value) {
      if (!isObject(value) || typeof value.id !== 'string' || !value.id || typeof value.name !== 'string' || !value.name) return invalid('listProjects', emptyProjects());
      projects.push({ id: value.id, name: value.name, state: typeof value.state === 'string' ? value.state : '' });
    }
    return newQueryBranch('listed', { projects, top: input.top, skip });
  }
}

/** Read one Azure DevOps work item's basic identity. */
export class GetWorkItemOperation implements Query<GetWorkItemInput, WorkItemMetadata> {
  /** Bind the provider client. */ constructor(private readonly client: AzureDevOpsClient) {}
  /** Return the manifest-generated operation definition. */ definition(): QueryDefinition { return GetWorkItemDefinition; }
  /** Validate the work item ID and read its metadata. */
  async invoke(call: Call, input: GetWorkItemInput): Promise<QueryAttempt<WorkItemMetadata>> {
    if (!isValidWorkItemID(input)) return terminal('getWorkItem', 'defect', 'VALIDATION', 'work item ID must be a positive decimal identifier', emptyWorkItem());
    const result = await this.client.exchange(call, 'getWorkItem', `/_apis/wit/workitems/${input.workItemId}?api-version=7.1`, true);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyWorkItem(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document) || !isPositiveWorkItemID(document.id) || !isObject(document.fields) || typeof document.fields['System.Title'] !== 'string' || !document.fields['System.Title']) return invalid('getWorkItem', emptyWorkItem());
    return newQueryBranch('found', {
      id: document.id, title: document.fields['System.Title'],
      state: typeof document.fields['System.State'] === 'string' ? document.fields['System.State'] : '',
      workItemType: typeof document.fields['System.WorkItemType'] === 'string' ? document.fields['System.WorkItemType'] : '',
    });
  }
}

function isValidWorkItemID(input: GetWorkItemInput): boolean {
  if (!input || typeof input.workItemId !== 'string' || !/^[0-9]+$/.test(input.workItemId)) return false;
  const digits = input.workItemId.replace(/^0+/, '');
  return digits.length > 0 && digits.length <= 10 && BigInt(digits) <= 2147483647n;
}
function isPositiveWorkItemID(value: unknown): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value > 0 && value <= 2147483647; }
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'azure-devops', operation, kind, message }; }
function terminal<Output>(operation: string, branch: string, kind: Failure['kind'], message: string, empty: Output): QueryAttempt<Output> { return newQueryBranch(branch, empty, {}, failure(operation, kind, message)); }
function invalid<Output>(operation: string, empty: Output): QueryAttempt<Output> { return terminal(operation, 'invalidResponse', 'PROTOCOL', 'Azure DevOps returned invalid JSON', empty); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function headerValue(value: string | string[] | undefined): string { return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
