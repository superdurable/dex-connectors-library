// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetBlobDefinition } from './generated/definitions.js';
import type { Config, Credentials, BlobDownload, GetBlobInput } from './generated/models.js';

const emptyDownload = (): BlobDownload => ({ dataBase64: '', contentType: '', etag: '' });
const accountPattern = /^[a-z0-9]{3,24}$/;
const containerPattern = /^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$/;
const storageVersion = '2023-11-03';
/** Injectable transport and clock for tests and local applications. */
export interface MicrosoftAzureBlobClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Signing clock; omitted uses the current time. */ now?: () => Date;
}
interface Exchange { status: number; body: Uint8Array; contentType: string; etag: string; retryAfterMs: number; error?: 'credential' | 'request' | 'tooLarge' }

/** Authenticated Azure Storage client for bounded blob downloads. */
export class MicrosoftAzureBlobClient {
  private readonly endpoint: string;
  private readonly accountName: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate the account, endpoint, and limits without contacting Azure. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: MicrosoftAzureBlobClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Azure Blob configuration and credentials are required');
    if (!accountPattern.test(config.accountName)) throw new TypeError('Azure storage account name is invalid');
    this.accountName = config.accountName;
    this.endpoint = validateBaseURL(config.endpoint || `https://${config.accountName}.blob.core.windows.net`).replace(/\/+$/, '');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Azure Blob request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the getBlob Query. */ getBlob(): GetBlobOperation { return new GetBlobOperation(this); }

  /** Sign and send one bounded Get Blob request. */
  async downloadBlob(call: Call, container: string, blobName: string): Promise<Exchange> {
    let accountKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      accountKey = credentials.accountKey instanceof SecretString ? credentials.accountKey.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!accountKey || !isHeaderSafeCredential(accountKey) || !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(accountKey)) return emptyExchange('credential');
    const signingKey = Buffer.from(accountKey, 'base64');
    if (!signingKey.length) return emptyExchange('credential');
    const path = `/${container}/${blobName.split('/').map(encodeURIComponent).join('/')}`;
    const url = `${this.endpoint}${path}`;
    const timestamp = this.now().toUTCString();
    const canonicalResource = `/${this.accountName}/${container}/${blobName}`;
    const signedValue = `GET${'\n'.repeat(12)}x-ms-date:${timestamp}\nx-ms-version:${storageVersion}\n${canonicalResource}`;
    const signature = createHmac('sha256', signingKey).update(signedValue).digest('base64');
    const headers = {
      'x-ms-date': timestamp, 'x-ms-version': storageVersion,
      Authorization: `SharedKey ${this.accountName}:${signature}`,
    };
    const request: ProviderRequest = { url, method: 'GET', headers,
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.endpoint.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    let isTooLarge = false;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        isTooLarge = true;
      } else return emptyExchange('request');
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'));
    if (isTooLarge || response.body.length > this.maxResponseBytes)
      return { status: response.status, body: new Uint8Array(), contentType: '', etag: '', retryAfterMs, error: 'tooLarge' };
    if (Buffer.from(response.body).includes(accountKey))
      return { ...emptyExchange('credential'), status: response.status, retryAfterMs };
    return { status: response.status, body: response.body,
      contentType: header(response.headers, 'content-type'), etag: header(response.headers, 'etag'), retryAfterMs };
  }
}

/** Download one complete blob by container and path. */
export class GetBlobOperation implements Query<GetBlobInput, BlobDownload> {
  /** Bind the provider client. */ constructor(private readonly client: MicrosoftAzureBlobClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetBlobDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetBlobInput): Promise<QueryAttempt<BlobDownload>> {
    if (!input || typeof input.container !== 'string' || !containerPattern.test(input.container) || input.container.includes('--') || !validBlobName(input.blobName))
      return terminal('defect', 'VALIDATION', 'Azure Blob container or blob name is invalid');
    const exchange = await this.client.downloadBlob(call, input.container, input.blobName);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Azure Blob account key is unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Azure Blob request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Azure Blob rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Azure Blob is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return newQueryBranch('missing', emptyDownload());
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Azure Blob rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Azure Blob body exceeded the configured limit');
    if (!safeHeader(exchange.contentType) || !safeHeader(exchange.etag)) return terminal('invalidResponse', 'PROTOCOL', 'Azure Blob returned invalid response headers');
    return newQueryBranch('downloaded', { dataBase64: Buffer.from(exchange.body).toString('base64'), contentType: exchange.contentType, etag: exchange.etag });
  }
}
function validBlobName(name: unknown): name is string {
  return typeof name === 'string' && Buffer.byteLength(name) > 0 && Buffer.byteLength(name) <= 1024 &&
    ![...name].some(character => character.charCodeAt(0) === 0 || character === '\r' || character === '\n') &&
    name.split('/').every(part => part !== '' && part !== '.' && part !== '..');
}
function safeHeader(value: string): boolean { return value.length <= 255 && ![...value].some(character => character.charCodeAt(0) === 0 || character === '\r' || character === '\n'); }
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), contentType: '', etag: '', retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'microsoft-azure-blob', operation: 'getBlob', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<BlobDownload> { return newQueryBranch(branch, emptyDownload(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<BlobDownload> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
