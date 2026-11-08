// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, ReauthorizationRequiredError, resolveCredential, type Call, type CredentialProvider, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, Credentials } from './generated/models.js';
import { UploadObjectOperation, DownloadObjectOperation, ListObjectsOperation } from './operations.js';

/** Injectable provider transport and OAuth refresh dependencies. */
export interface CloudStorageClientOptions {
  /** Provider sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Refresh driver; omitted uses the manifest declaration. */ refreshDriver?: CredentialRefreshDriver<Credentials>;
  /** Clock used to interpret HTTP Retry-After dates. */ now?: () => Date;
}

/** One bounded Cloud Storage JSON API response or a classified transport failure. */
export interface StorageExchange {
  /** Provider status, or zero before response headers. */ status: number;
  /** Bounded response bytes retained only in process memory. */ body: Uint8Array;
  /** Retry-After delay in milliseconds. */ retryAfterMs: number;
  /** Response media type, when supplied. */ contentType: string;
  /** Safe local error class. */ error?: 'credential' | 'refresh' | 'request' | 'tooLarge' | 'redirect';
  /** Positive evidence that no provider HTTP bytes could be sent. */ isProvablyNotSent?: boolean;
}

/** Google Cloud Storage client that resolves credentials for each call. */
export class CloudStorageClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  /** Maximum decoded upload size in bytes. */ readonly maxUploadBytes: number;
  private readonly refreshDriver: CredentialRefreshDriver<Credentials>;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate configuration without contacting Google. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: CloudStorageClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Google Cloud Storage configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://storage.googleapis.com');
    this.timeoutMs = config.timeoutMs || 30_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    this.maxUploadBytes = config.maxUploadBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 ||
        !Number.isSafeInteger(this.maxUploadBytes) || this.maxUploadBytes <= 0 || this.maxUploadBytes > 64 * 1_048_576 || this.maxResponseBytes < this.maxUploadBytes)
      throw new TypeError('Google Cloud Storage request limits are invalid');
    this.refreshDriver = options.refreshDriver ?? new ManifestCredentialRefreshDriver<Credentials>({
      providerName: 'Google Cloud Storage', auth: Authorization, credentialFields: CredentialFields,
    });
    this.sendRequest = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the object page Query. */ listObjects(): ListObjectsOperation { return new ListObjectsOperation(this); }
  /** Bind the object download Query. */ downloadObject(): DownloadObjectOperation { return new DownloadObjectOperation(this); }
  /** Bind the conditional object upload Mutation. */ uploadObject(): UploadObjectOperation { return new UploadObjectOperation(this); }

  /** Resolve one safe bearer token without persisting it in Flow state. */
  async resolveToken(call: Call): Promise<{ token: string; error?: StorageExchange['error'] }> {
    try {
      const resolved = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      const token = resolved.accessToken instanceof SecretString ? resolved.accessToken.reveal() : '';
      if (!token || !isHeaderSafeCredential(token)) return { token: '', error: 'credential' };
      return { token };
    } catch (error) {
      return { token: '', error: error instanceof ReauthorizationRequiredError ? 'credential' : 'refresh' };
    }
  }

  /** Send one bounded provider request without leaking credentials or response bodies. */
  async request(call: Call, path: string, options: { method?: 'GET' | 'POST'; body?: Uint8Array; contentType?: string; media?: boolean; token?: string; signal?: AbortSignal } = {}): Promise<StorageExchange> {
    const authorization = options.token === undefined ? await this.resolveToken(call) : { token: options.token };
    if (authorization.error) return failedExchange(authorization.error);
    const token = authorization.token;
    const request: ProviderRequest = {
      url: this.baseURL + path, method: options.method ?? 'GET',
      headers: {
        Accept: options.media ? '*/*' : 'application/json', Authorization: `Bearer ${token}`, 'User-Agent': 'Dex Connector (https://superdurable.io)',
        ...(options.contentType ? { 'Content-Type': options.contentType } : {}),
      },
      ...(options.body ? { body: options.body } : {}),
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: options.signal ?? call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined)
        return { ...failedExchange('tooLarge'), status: error.status, retryAfterMs: parseRetryAfter(header(error.headers ?? {}, 'retry-after'), this.now()) };
      if (error instanceof ProviderTransportError)
        return { ...failedExchange('request'), isProvablyNotSent: error.isProvablyNotSent };
      return failedExchange('request');
    }
    if (options.media && (response.status === 302 || response.status === 307)) {
      let location: URL;
      try { location = new URL(header(response.headers, 'location')); }
      catch { return failedExchange('redirect'); }
      if (location.protocol !== 'https:' || location.username || location.password || location.port || !location.hostname.endsWith('.googleusercontent.com'))
        return failedExchange('redirect');
      try {
        response = await this.sendRequest({
          url: location.toString(), method: 'GET', headers: { 'User-Agent': 'Dex Connector (https://superdurable.io)' },
          maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs, addressPolicy: 'publicHTTPS', signal: request.signal,
        });
      } catch (error) {
        if (error instanceof BodyTooLargeError && error.status !== undefined)
          return { ...failedExchange('tooLarge'), status: error.status, retryAfterMs: parseRetryAfter(header(error.headers ?? {}, 'retry-after'), this.now()) };
        return failedExchange('request');
      }
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'), this.now());
    if (Buffer.from(response.body).includes(token)) return { ...failedExchange('credential'), status: response.status, retryAfterMs };
    return { status: response.status, body: response.body, contentType: header(response.headers, 'content-type'), retryAfterMs };
  }
}

function failedExchange(error: NonNullable<StorageExchange['error']>): StorageExchange {
  return { status: 0, body: new Uint8Array(), contentType: '', retryAfterMs: 0, error };
}
function header(headers: ProviderResponse['headers'], name: string): string {
  const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}
