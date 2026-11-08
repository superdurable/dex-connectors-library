// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, ReauthorizationRequiredError, resolveCredential, type Call, type CredentialProvider, type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, Credentials } from './generated/models.js';
import { CreateContactOperation, GetContactOperation, ListContactsOperation } from './operations.js';

/** Injectable provider transport and OAuth refresh dependencies. */
export interface ContactsClientOptions {
  /** Provider sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Refresh driver; omitted uses the manifest declaration. */ refreshDriver?: CredentialRefreshDriver<Credentials>;
  /** Clock used to interpret HTTP Retry-After dates. */ now?: () => Date;
}

/** One bounded People API response or a classified transport failure. */
export interface ContactsExchange {
  /** Provider status, or zero before response headers. */ status: number;
  /** Bounded response bytes retained only in process memory. */ body: Uint8Array;
  /** Retry-After delay in milliseconds. */ retryAfterMs: number;
  /** Safe local error class. */ error?: 'credential' | 'refresh' | 'request' | 'tooLarge';
  /** Positive evidence that no provider HTTP bytes could be sent. */ isProvablyNotSent?: boolean;
}

/** Google Contacts client that resolves credentials for each call. */
export class ContactsClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly refreshDriver: CredentialRefreshDriver<Credentials>;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate configuration without contacting Google. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ContactsClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Google Contacts configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://people.googleapis.com');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Google Contacts request limits are invalid');
    this.refreshDriver = options.refreshDriver ?? new ManifestCredentialRefreshDriver<Credentials>({
      providerName: 'Google Contacts', auth: Authorization, credentialFields: CredentialFields,
    });
    this.sendRequest = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the contact page Query. */ listContacts(): ListContactsOperation { return new ListContactsOperation(this); }
  /** Bind the contact Query. */ getContact(): GetContactOperation { return new GetContactOperation(this); }
  /** Bind the contact-creation Mutation. */ createContact(): CreateContactOperation { return new CreateContactOperation(this); }

  /** Resolve one safe bearer token without persisting it in Flow state. */
  async resolveToken(call: Call): Promise<{ token: string; error?: ContactsExchange['error'] }> {
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
  async request(call: Call, path: string, options: { method?: 'GET' | 'POST'; body?: Uint8Array; token?: string; signal?: AbortSignal } = {}): Promise<ContactsExchange> {
    const authorization = options.token === undefined ? await this.resolveToken(call) : { token: options.token };
    if (authorization.error) return failedExchange(authorization.error);
    const token = authorization.token;
    const request: ProviderRequest = {
      url: this.baseURL + path, method: options.method ?? 'GET',
      headers: {
        Accept: 'application/json', Authorization: `Bearer ${token}`, 'User-Agent': 'Dex Connector (https://superdurable.io)',
        ...(options.body ? { 'Content-Type': 'application/json' } : {}),
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
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'), this.now());
    if (Buffer.from(response.body).includes(token)) return { ...failedExchange('credential'), status: response.status, retryAfterMs };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

function failedExchange(error: NonNullable<ContactsExchange['error']>): ContactsExchange {
  return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error };
}
function header(headers: ProviderResponse['headers'], name: string): string {
  const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}
