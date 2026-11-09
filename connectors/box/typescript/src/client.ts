// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomBytes } from 'node:crypto';
import {
  ReauthorizationRequiredError, SecretString, resolveCredential,
  type Call, type CredentialProvider, type CredentialRefreshDriver,
} from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import {
  BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter,
  sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse,
} from '@superdurable/connectors-sdk/providerhttp';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, Credentials } from './models.js';
import { DownloadFileOperation, UploadFileOperation } from './operations.js';

/** One bounded provider response or classified transport failure. */
export interface BoxExchange {
  /** HTTP status, or zero before response headers. */ status: number;
  /** Bounded provider body, never placed directly into Flow state. */ body: Uint8Array;
  /** Retry delay in milliseconds. */ retryAfterMs: number;
  /** Local failure reason. */ error?: 'credential' | 'refresh' | 'request' | 'read' | 'tooLarge' | 'invalidRedirect';
  /** Whether the request may have reached Box. */ requestSent: boolean;
  /** Redirect location, kept only in transport memory. */ location?: string;
}

/** Injectable HTTP and credential dependencies. */
export interface BoxClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** OAuth refresh driver; omitted uses the manifest declaration. */ refreshDriver?: CredentialRefreshDriver<Credentials>;
  /** Clock for Retry-After HTTP dates. */ now?: () => Date;
}

/** Box HTTP transport bound to a renewable credential source. */
export class BoxClient {
  /** Validated API endpoint. */ readonly baseURL: string;
  /** Validated upload endpoint. */ readonly uploadBaseURL: string;
  /** Timeout for one request. */ readonly timeoutMs: number;
  /** Error and metadata response limit. */ readonly maxResponseBytes: number;
  /** Download content limit. */ readonly maxDownloadBytes: number;
  /** Replacement content limit. */ readonly maxUploadBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly refreshDriver: CredentialRefreshDriver<Credentials>;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate configuration and inject dependencies without network access. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: BoxClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Box configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://api.box.com');
    this.uploadBaseURL = validateBaseURL(config.uploadBaseUrl || 'https://upload.box.com/api');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    this.maxDownloadBytes = config.maxDownloadBytes || 1_048_576;
    this.maxUploadBytes = config.maxUploadBytes || 1_048_576;
    if (![this.timeoutMs, this.maxResponseBytes, this.maxDownloadBytes, this.maxUploadBytes].every(value =>
      Number.isSafeInteger(value) && value > 0)) throw new TypeError('Box request limits must be positive integers');
    if (this.maxUploadBytes > 50 * 1024 * 1024) throw new TypeError('Box direct upload limit exceeds 50 MiB');
    this.credentials = credentials;
    this.refreshDriver = options.refreshDriver ?? new ManifestCredentialRefreshDriver<Credentials>({
      providerName: 'Box', auth: Authorization, credentialFields: CredentialFields,
    });
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the bounded download Query. */ downloadFile(): DownloadFileOperation { return new DownloadFileOperation(this); }
  /** Bind the bounded replacement Mutation. */ uploadFile(): UploadFileOperation { return new UploadFileOperation(this); }

  /** Download one file, following only Box's signed HTTPS content URL. */
  async download(call: Call, fileID: string): Promise<BoxExchange> {
    const token = await this.resolveToken(call);
    if (typeof token !== 'string') return token;
    const first = await this.request(call, 'GET', this.baseURL + `/2.0/files/${fileID}/content`,
      { Authorization: 'Bearer ' + token }, this.maxDownloadBytes, token);
    if (first.status !== 302 || first.error) return first;
    const location = first.location ?? '';
    if (!isBoxDownloadURL(location)) return { ...first, body: new Uint8Array(), error: 'invalidRedirect' };
    // The signed URL authorizes itself; the OAuth token must not leave the Box API host.
    const second = await this.request(call, 'GET', location, {}, this.maxDownloadBytes, token);
    if (second.status === 302 && !second.error) return { ...second, body: new Uint8Array(), error: 'invalidRedirect' };
    return second;
  }

  /** Read one file's current identity and digest. */
  async getFileMetadata(call: Call, fileID: string): Promise<BoxExchange> {
    const token = await this.resolveToken(call);
    if (typeof token !== 'string') return token;
    return this.request(call, 'GET', this.baseURL + `/2.0/files/${fileID}?fields=id,type,name,sha1,size`,
      { Authorization: 'Bearer ' + token, Accept: 'application/json' }, this.maxResponseBytes, token);
  }

  /** Replace current content with Box's attributes-first multipart request. */
  async upload(call: Call, fileID: string, name: string, content: Uint8Array): Promise<BoxExchange> {
    const token = await this.resolveToken(call);
    if (typeof token !== 'string') return token;
    const boundary = 'dexbox' + randomBytes(12).toString('hex');
    const escapedName = name.replace(/\\/g, '\\\\').replace(/"/g, '\\"').replace(/[\r\n]/g, '_');
    const body = Buffer.concat([
      Buffer.from(`--${boundary}\r\nContent-Disposition: form-data; name="attributes"\r\n\r\n${JSON.stringify({ name })}\r\n`),
      Buffer.from(`--${boundary}\r\nContent-Disposition: form-data; name="file"; filename="${escapedName}"\r\nContent-Type: application/octet-stream\r\n\r\n`),
      Buffer.from(content), Buffer.from(`\r\n--${boundary}--\r\n`),
    ]);
    return this.request(call, 'POST', this.uploadBaseURL + `/2.0/files/${fileID}/content`, {
      Authorization: 'Bearer ' + token, Accept: 'application/json', 'Content-Type': `multipart/form-data; boundary=${boundary}`,
    }, this.maxResponseBytes, token, body);
  }

  private async resolveToken(call: Call): Promise<string | BoxExchange> {
    let token: string;
    try {
      const credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      if (!(credentials.accessToken instanceof SecretString)) return emptyExchange('credential');
      token = credentials.accessToken.reveal();
    } catch (error) {
      return emptyExchange(error instanceof ReauthorizationRequiredError ? 'credential' : 'refresh');
    }
    return token && isHeaderSafeCredential(token) ? token : emptyExchange('credential');
  }

  private async request(call: Call, method: string, url: string, headers: Record<string, string>,
    successLimit: number, token: string, body?: Uint8Array): Promise<BoxExchange> {
    const request: ProviderRequest = {
      url, method, headers: { ...headers, 'User-Agent': 'Dex Connector (https://superdurable.io)' },
      maxResponseBytes: Math.max(successLimit, this.maxResponseBytes),
      addressPolicy: new URL(url).protocol === 'http:' ? 'privateNetworkHTTP' : 'publicHTTPS',
      timeoutMs: this.timeoutMs, signal: call.context.cancellationSignal,
      ...(body === undefined ? {} : { body }),
    };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError) return {
        ...emptyExchange('tooLarge'), status: error.status ?? 0, requestSent: true,
        retryAfterMs: parseRetryAfter(header(error.headers ?? {}, 'retry-after'), this.now()),
      };
      if (error instanceof ProviderTransportError) return {
        ...emptyExchange('request'), requestSent: !error.isProvablyNotSent,
      };
      return emptyExchange('request');
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'), this.now());
    const location = response.status === 302 ? header(response.headers, 'location') : '';
    if (location.includes(token) || Buffer.from(response.body).includes(token))
      return { status: response.status, body: new Uint8Array(), retryAfterMs, requestSent: true, error: 'credential' };
    const limit = response.status === 200 ? successLimit : this.maxResponseBytes;
    if (response.body.length > limit) return { status: response.status, body: new Uint8Array(), retryAfterMs, requestSent: true, error: 'tooLarge' };
    return { status: response.status, body: response.body, retryAfterMs, requestSent: true, ...(location ? { location } : {}) };
  }
}

function emptyExchange(error: NonNullable<BoxExchange['error']>): BoxExchange {
  return { status: 0, body: new Uint8Array(), retryAfterMs: 0, requestSent: false, error };
}

function isBoxDownloadURL(value: string): boolean {
  if (!value || value.length > 8192) return false;
  try {
    const url = new URL(value);
    return url.protocol === 'https:' && url.hostname.endsWith('.boxcloud.com')
      && !url.username && !url.password && !url.port && !url.hash && !!url.pathname;
  } catch { return false; }
}

/** Read one response header without depending on provider casing. */
export function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
