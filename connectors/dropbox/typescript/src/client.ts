// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  ReauthorizationRequiredError, SecretString, resolveCredential,
  type Call, type CredentialProvider, type CredentialRefreshDriver,
} from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import {
  BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL,
  type ProviderRequest, type ProviderResponse,
} from '@superdurable/connectors-sdk/providerhttp';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, Credentials } from './models.js';
import {
  ContinueFolderOperation, DownloadFileOperation, GetMetadataOperation, ListFolderOperation, UploadFileOperation,
} from './operations.js';

/** One response or sanitized credential and transport failure. */
export interface DropboxExchange {
  /** HTTP status, or zero before a response arrives. */ status: number;
  /** Bounded body; never returned directly to Flow state. */ body: Uint8Array;
  /** Download metadata header, if returned. */ metadata: string;
  /** Retry-After delay in milliseconds. */ retryAfterMs: number;
  /** Local failure classification. */ error?: 'credential' | 'refresh' | 'request' | 'read' | 'tooLarge';
}

/** Injectable provider sender and renewable credential driver. */
export interface DropboxClientOptions {
  /** Omit for the SDK's redirect-safe transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Omit for the manifest-generated OAuth refresh driver. */ refreshDriver?: CredentialRefreshDriver<Credentials>;
  /** Clock for Retry-After HTTP dates. */ now?: () => Date;
}

/** Dropbox RPC and content transport bound to one named credential source. */
export class DropboxClient {
  /** Validated RPC endpoint root. */ readonly baseURL: string;
  /** Validated content endpoint root. */ readonly contentBaseURL: string;
  /** Per-request timeout in milliseconds. */ readonly timeoutMs: number;
  /** Maximum JSON response bytes. */ readonly maxResponseBytes: number;
  /** Maximum downloaded file bytes. */ readonly maxDownloadBytes: number;
  /** Maximum decoded upload file bytes. */ readonly maxUploadBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly refreshDriver: CredentialRefreshDriver<Credentials>;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate settings and inject provider dependencies before any network call. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: DropboxClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Dropbox configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://api.dropboxapi.com');
    this.contentBaseURL = validateBaseURL(config.contentBaseUrl || 'https://content.dropboxapi.com');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    this.maxDownloadBytes = config.maxDownloadBytes || 1_048_576;
    this.maxUploadBytes = config.maxUploadBytes || 1_048_576;
    if (![this.timeoutMs, this.maxResponseBytes, this.maxDownloadBytes, this.maxUploadBytes].every(value =>
      Number.isSafeInteger(value) && value > 0)) throw new TypeError('Dropbox request limits must be positive integers');
    if (this.maxUploadBytes > 150 * 1024 * 1024) throw new TypeError('Dropbox upload limit exceeds 150 MiB');
    this.credentials = credentials;
    this.refreshDriver = options.refreshDriver ?? new ManifestCredentialRefreshDriver<Credentials>({
      providerName: 'Dropbox', auth: Authorization, credentialFields: CredentialFields,
    });
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the folder listing Query. */ listFolder(): ListFolderOperation { return new ListFolderOperation(this); }
  /** Bind the metadata Query. */ getMetadata(): GetMetadataOperation { return new GetMetadataOperation(this); }
  /** Bind the cursor continuation Query. */ continueFolder(): ContinueFolderOperation { return new ContinueFolderOperation(this); }
  /** Bind the bounded download Query. */ downloadFile(): DownloadFileOperation { return new DownloadFileOperation(this); }
  /** Bind the bounded replacement Mutation. */ uploadFile(): UploadFileOperation { return new UploadFileOperation(this); }

  /** Post one JSON request to the Dropbox RPC API. */
  postJSON(call: Call, path: string, payload: object): Promise<DropboxExchange> {
    return this.post(call, this.baseURL + path, Buffer.from(JSON.stringify(payload)), {
      Accept: 'application/json', 'Content-Type': 'application/json',
    }, this.maxResponseBytes);
  }

  /** Download one file using Dropbox's content endpoint. */
  download(call: Call, path: string): Promise<DropboxExchange> {
    return this.post(call, this.contentBaseURL + '/2/files/download', undefined, {
      'Dropbox-API-Arg': asciiJSON({ path }),
    }, this.maxDownloadBytes, true);
  }

  /** Overwrite one file using Dropbox's content endpoint. */
  upload(call: Call, path: string, content: Uint8Array): Promise<DropboxExchange> {
    return this.post(call, this.contentBaseURL + '/2/files/upload', content, {
      'Dropbox-API-Arg': asciiJSON({ path, mode: 'overwrite', autorename: false }),
      'Content-Type': 'application/octet-stream',
    }, this.maxResponseBytes);
  }

  private async post(
    call: Call, url: string, body: Uint8Array | undefined, headers: Record<string, string>,
    successLimit: number, isDownload = false,
  ): Promise<DropboxExchange> {
    let token: string;
    try {
      const credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      if (!(credentials.accessToken instanceof SecretString)) return { status: 0, body: new Uint8Array(), metadata: '', retryAfterMs: 0, error: 'credential' };
      token = credentials.accessToken.reveal();
    } catch (error) {
      return { status: 0, body: new Uint8Array(), metadata: '', retryAfterMs: 0,
        error: error instanceof ReauthorizationRequiredError ? 'credential' : 'refresh' };
    }
    if (!token || !isHeaderSafeCredential(token)) return { status: 0, body: new Uint8Array(), metadata: '', retryAfterMs: 0, error: 'credential' };
    const endpoint = new URL(url);
    const request: ProviderRequest = {
      url, method: 'POST', headers: { ...headers, Authorization: 'Bearer ' + token,
        'User-Agent': 'Dex Connector (https://superdurable.io)' },
      maxResponseBytes: Math.max(successLimit, this.maxResponseBytes),
      addressPolicy: endpoint.protocol === 'http:' ? 'privateNetworkHTTP' : 'publicHTTPS',
      timeoutMs: this.timeoutMs, signal: call.context.cancellationSignal,
      ...(body === undefined ? {} : { body }),
    };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError) {
        const status = error.status ?? 0;
        const responseHeaders = error.headers ?? {};
        return { status, body: new Uint8Array(), metadata: '',
          retryAfterMs: parseRetryAfter(header(responseHeaders, 'retry-after'), this.now()), error: 'tooLarge' };
      }
      return { status: 0, body: new Uint8Array(), metadata: '', retryAfterMs: 0, error: 'request' };
    }
    const metadata = isDownload ? header(response.headers, 'dropbox-api-result') : '';
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'), this.now());
    if (metadata.length > 65_536 || metadata.includes(token) || Buffer.from(response.body).includes(token)) {
      return { status: response.status, body: new Uint8Array(), metadata: '', retryAfterMs, error: 'credential' };
    }
    const limit = response.status === 200 ? successLimit : this.maxResponseBytes;
    if (response.body.length > limit) return { status: response.status, body: new Uint8Array(), metadata: '', retryAfterMs, error: 'tooLarge' };
    return { status: response.status, body: response.body, metadata, retryAfterMs };
  }
}

/** Read a case-insensitive response header. */
export function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}

function asciiJSON(value: object): string {
  return JSON.stringify(value).replace(/[\u007f-\uffff]/g, character =>
    '\\u' + character.charCodeAt(0).toString(16).padStart(4, '0'));
}
