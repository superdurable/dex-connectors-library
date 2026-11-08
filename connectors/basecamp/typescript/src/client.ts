// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, ReauthorizationRequiredError, resolveCredential, type Call, type CredentialProvider, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, Credentials } from './generated/models.js';
import { ListAccountsOperation } from './operations.js';

/** Injectable Basecamp transport and OAuth refresh dependencies. */
export interface BasecampClientOptions {
  /** Provider sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Refresh driver; omitted uses the manifest declaration. */ refreshDriver?: CredentialRefreshDriver<Credentials>;
  /** Clock used for Retry-After dates. */ now?: () => Date;
}

/** One bounded provider response or a classified local failure. */
export interface BasecampExchange {
  /** HTTP status, or zero before response headers. */ status: number;
  /** Response bytes retained only during this call. */ body: Uint8Array;
  /** Retry-After delay in milliseconds. */ retryAfterMs: number;
  /** Safe local failure class. */ error?: 'credential' | 'refresh' | 'request' | 'tooLarge';
}

/** Basecamp client resolving renewable OAuth credentials for each call. */
export class BasecampClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly refreshDriver: CredentialRefreshDriver<Credentials>;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate settings without contacting Basecamp. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: BasecampClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Basecamp configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://launchpad.37signals.com');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Basecamp request limits are invalid');
    this.refreshDriver = options.refreshDriver ?? new ManifestCredentialRefreshDriver<Credentials>({
      providerName: 'Basecamp', auth: Authorization, credentialFields: CredentialFields,
    });
    this.sendRequest = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the account page Query. */ listAccounts(): ListAccountsOperation { return new ListAccountsOperation(this); }

  /** Send one bounded GET with a bearer token held outside Flow state. */
  async get(call: Call, path: string): Promise<BasecampExchange> {
    let token: string;
    try {
      const resolved = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      token = resolved.accessToken instanceof SecretString ? resolved.accessToken.reveal() : '';
      if (!token || !isHeaderSafeCredential(token)) return failedExchange('credential');
    } catch (error) {
      return failedExchange(error instanceof ReauthorizationRequiredError ? 'credential' : 'refresh');
    }
    const request: ProviderRequest = {
      url: this.baseURL + path, method: 'GET', headers: { Accept: 'application/json', Authorization: `Bearer ${token}`, 'User-Agent': 'Dex Connector (support@superdurable.io)' },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined)
        return { ...failedExchange('tooLarge'), status: error.status, retryAfterMs: parseRetryAfter(header(error.headers ?? {}, 'retry-after'), this.now()) };
      if (error instanceof ProviderTransportError) return failedExchange('request');
      return failedExchange('request');
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'), this.now());
    if (Buffer.from(response.body).includes(token)) return { ...failedExchange('credential'), status: response.status, retryAfterMs };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

function failedExchange(error: NonNullable<BasecampExchange['error']>): BasecampExchange {
  return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error };
}
function header(headers: ProviderResponse['headers'], name: string): string {
  const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}
