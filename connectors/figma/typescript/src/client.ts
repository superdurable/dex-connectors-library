// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, SecretString, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetFileDefinition, GetFileCommentsDefinition } from './generated/definitions.js';
import type { Config, Credentials, File, FileComments, GetFileInput, GetFileCommentsInput } from './generated/models.js';
import { emptyFile, emptyFileComments } from './generated/mock.js';

const defaultBaseURL = 'https://api.figma.com/v1';
const defaultTimeoutMs = 30_000;
const defaultMaxResponseBytes = 8_388_608;
const safeFileKey = /^[A-Za-z0-9_-]{1,128}$/;

/** One injectable, bounded Figma HTTP sender. */
export type FigmaRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional HTTP and clock dependencies for provider tests. */
export interface FigmaClientOptions {
  /** Omit to use the SDK's bounded, redirect-safe transport. */ send?: FigmaRequestSender;
  /** Clock used for Retry-After dates. */ now?: () => Date;
}

/** Validated Figma configuration and call-time credentials. */
export class FigmaClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: FigmaRequestSender;
  private readonly now: () => Date;

  /** Construct a client without contacting Figma. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: FigmaClientOptions = {}) {
    if (!credentials) throw new TypeError('Figma credential provider is required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || defaultTimeoutMs;
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Figma timeout must be positive');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Figma maxResponseBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the file query. */ getFile(): GetFileOperation { return new GetFileOperation(this); }
  /** Bind the comments query. */ getFileComments(): GetFileCommentsOperation { return new GetFileCommentsOperation(this); }

  /** Resolve private credentials, read once, and classify the provider result. */
  async invokeGet<Output>(call: Call, operation: string, path: string, empty: Output, successBranch: string, decode: (body: Uint8Array) => Output): Promise<QueryAttempt<Output>> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.apiKey instanceof SecretString)) throw new TypeError('Figma API key is invalid');
      token = credentials.apiKey.reveal();
    } catch { return failed(operation, 'defect', 'AUTHENTICATION', 'connection credentials are unavailable', empty); }
    if (!token || !isHeaderSafeCredential(token)) return failed(operation, 'defect', 'AUTHENTICATION', 'connection credentials are invalid', empty);
    let response: ProviderResponse;
    try {
      response = await this.send({
        url: this.baseURL + path, method: 'GET', headers: { Accept: 'application/json', 'X-Figma-Token': token },
        maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
        addressPolicy: this.baseURL === defaultBaseURL ? 'publicHTTPS' : 'privateNetworkHTTP',
        signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return failed(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'provider response exceeded configured limit', empty);
      if (error instanceof BodyTooLargeError && error.status) return this.classifyStatus(operation, error.status, error.headers ?? {}, empty);
      return newQueryRetry(failure(operation, 'AVAILABILITY', 'provider request failed'));
    }
    if (response.status !== 200) return this.classifyStatus(operation, response.status, response.headers, empty);
    try { return newQueryBranch(successBranch, decode(response.body)); }
    catch { return failed(operation, 'invalidResponse', 'PROTOCOL', 'provider returned unusable JSON', empty); }
  }

  private classifyStatus<Output>(operation: string, status: number, headers: Record<string, string | string[] | undefined>, empty: Output): QueryAttempt<Output> {
    const retryValue = headers['retry-after'] ?? headers['Retry-After'];
    const retryAfterMs = parseRetryAfter(Array.isArray(retryValue) ? retryValue[0] ?? '' : retryValue ?? '', this.now());
    if (status === 429) return newQueryRetry(failure(operation, 'RATE_LIMIT', 'provider rate limit'), retryAfterMs);
    if (status === 408 || status === 409 || status >= 500) return newQueryRetry(failure(operation, 'AVAILABILITY', 'provider temporarily unavailable'), retryAfterMs);
    if (status === 404) return failed(operation, 'notFound', 'NOT_FOUND', 'provider resource was not found', empty);
    if (status === 401) return failed(operation, 'providerRejected', 'AUTHENTICATION', 'provider rejected the API key', empty);
    if (status === 403) return failed(operation, 'providerRejected', 'AUTHORIZATION', 'provider denied the request', empty);
    return failed(operation, 'providerRejected', 'PROVIDER_REJECTION', `provider rejected HTTP ${status}`, empty);
  }
}

/** One Figma file query. */
export class GetFileOperation implements Query<GetFileInput, File> {
  /** Bind a client. */ constructor(private readonly client: FigmaClient) {}
  /** Return the manifest contract. */ definition() { return GetFileDefinition; }
  /** Validate the key and read one file. */
  invoke(call: Call, input: GetFileInput): Promise<QueryAttempt<File>> {
    if (!safeFileKey.test(input.fileKey)) return Promise.resolve(failed('getFile', 'defect', 'VALIDATION', 'fileKey is invalid', emptyFile));
    return this.client.invokeGet(call, 'getFile', `/files/${input.fileKey}`, emptyFile, 'found', decodeFile);
  }
}

/** One Figma comments query. */
export class GetFileCommentsOperation implements Query<GetFileCommentsInput, FileComments> {
  /** Bind a client. */ constructor(private readonly client: FigmaClient) {}
  /** Return the manifest contract. */ definition() { return GetFileCommentsDefinition; }
  /** Validate the key and read file comments. */
  invoke(call: Call, input: GetFileCommentsInput): Promise<QueryAttempt<FileComments>> {
    if (!safeFileKey.test(input.fileKey)) return Promise.resolve(failed('getFileComments', 'defect', 'VALIDATION', 'fileKey is invalid', emptyFileComments));
    return this.client.invokeGet(call, 'getFileComments', `/files/${input.fileKey}/comments`, emptyFileComments, 'listed', decodeComments);
  }
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'figma', operation, message }; }
function failed<Output>(operation: string, branch: string, kind: Failure['kind'], message: string, empty: Output): QueryAttempt<Output> {
  return newQueryBranch(branch, empty, {}, failure(operation, kind, message));
}
function objectBody(body: Uint8Array): Record<string, unknown> {
  const value: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('invalid object');
  return value as Record<string, unknown>;
}
function decodeFile(body: Uint8Array): File {
  const value = objectBody(body);
  if (value.name != null && typeof value.name !== 'string' || value.lastModified != null && typeof value.lastModified !== 'string') throw new TypeError('invalid file');
  const name = typeof value.name === 'string' ? value.name : '';
  const lastModified = typeof value.lastModified === 'string' ? value.lastModified : '';
  if (!name || value.document === undefined || value.document === null) throw new TypeError('incomplete file');
  return { name, lastModified, document: value.document };
}
function decodeComments(body: Uint8Array): FileComments {
  const value = objectBody(body);
  if (!Array.isArray(value.comments)) throw new TypeError('missing comments');
  return { comments: value.comments.map(item => {
    if (item === null) return { id: '', message: '' };
    if (!item || typeof item !== 'object' || Array.isArray(item)) throw new TypeError('invalid comment');
    const comment = item as Record<string, unknown>;
    if (comment.id != null && typeof comment.id !== 'string' || comment.message != null && typeof comment.message !== 'string') throw new TypeError('invalid comment');
    return { id: comment.id as string ?? '', message: comment.message as string ?? '' };
  }) };
}
