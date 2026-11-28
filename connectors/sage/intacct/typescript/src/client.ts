// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, ReauthorizationRequiredError, resolveCredential, type Call, type CredentialProvider, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, Credentials } from './generated/models.js';
import { GetInvoiceOperation, ListInvoicesOperation } from './operations.js';

/** Injectable Sage Intacct transport and OAuth refresh dependencies. */
export interface SageIntacctClientOptions {
  /** Provider sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Refresh driver; omitted uses the manifest declaration. */ refreshDriver?: CredentialRefreshDriver<Credentials>;
  /** Clock used for Retry-After dates. */ now?: () => Date;
}

/** One bounded provider response or a classified local failure. */
export interface SageIntacctExchange {
  /** HTTP status, or zero before response headers. */ status: number;
  /** Response bytes retained only during this call. */ body: Uint8Array;
  /** Retry-After delay in milliseconds. */ retryAfterMs: number;
  /** Safe local failure class. */ error?: 'credential' | 'refresh' | 'request' | 'tooLarge';
}

/** Sage Intacct client resolving renewable OAuth credentials for each call. */
export class SageIntacctClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly refreshDriver: CredentialRefreshDriver<Credentials>;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate settings without contacting Sage Intacct. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: SageIntacctClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Sage Intacct configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://api.intacct.com');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Sage Intacct request limits are invalid');
    this.refreshDriver = options.refreshDriver ?? new ManifestCredentialRefreshDriver<Credentials>({
      providerName: 'Sage Intacct', auth: Authorization, credentialFields: CredentialFields,
    });
    this.sendRequest = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the accounts receivable invoice Query. */ getInvoice(): GetInvoiceOperation { return new GetInvoiceOperation(this); }

  /** Bind the first-page invoice listing Query. */
  listInvoices(): ListInvoicesOperation { return new ListInvoicesOperation(this); }

  /** Send one bounded GET with a bearer token held outside Flow state. */
  async get(call: Call, path: string): Promise<SageIntacctExchange> {
    let token: string;
    try {
      const resolved = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      token = resolved.accessToken instanceof SecretString ? resolved.accessToken.reveal() : '';
      if (!token || !isHeaderSafeCredential(token)) return failedExchange('credential');
    } catch (error) {
      return failedExchange(error instanceof ReauthorizationRequiredError ? 'credential' : 'refresh');
    }
    const request: ProviderRequest = {
      url: this.baseURL + path, method: 'GET', headers: { Accept: 'application/json', Authorization: `Bearer ${token}` },
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
    if (containsAccessToken(response.body, token)) return { ...failedExchange('credential'), status: response.status, retryAfterMs };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

function failedExchange(error: NonNullable<SageIntacctExchange['error']>): SageIntacctExchange {
  return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error };
}
function header(headers: ProviderResponse['headers'], name: string): string {
  const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}


function containsAccessToken(body: Uint8Array, token: string): boolean {
  const bytes = Buffer.from(body);
  if (bytes.includes(token)) return true;
  if (!bytes.includes(0x5c)) return false;
  let document: unknown;
  try { document = JSON.parse(bytes.toString('utf8')); }
  catch { return false; }
  const pending: unknown[] = [document];
  while (pending.length) {
    const value = pending.pop();
    if (typeof value === 'string' && value.includes(token)) return true;
    if (value !== null && typeof value === 'object') {
      for (const [name, field] of Object.entries(value)) {
        if (name.includes(token)) return true;
        pending.push(field);
      }
    }
  }
  return false;
}
