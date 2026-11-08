// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  ReauthorizationRequiredError, SecretString, newQueryBranch, newQueryRetry, resolveCredential,
  type Call, type CredentialProvider, type CredentialRefreshDriver, type Failure,
  type Query, type QueryAttempt, type QueryDefinition,
} from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import {
  BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter,
  sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse,
} from '@superdurable/connectors-sdk/providerhttp';
import { GetContactDefinition } from './generated/definitions.js';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, ContactMetadata, Credentials, GetContactInput } from './generated/models.js';

const defaultBaseURL = 'https://api.cc.email/v3';
const contactIDPattern = /^[a-fA-F0-9]{8}-(?:[a-fA-F0-9]{4}-){3}[a-fA-F0-9]{12}$/;
const emptyContact = (): ContactMetadata => ({ contactId: '' });

/** Injectable provider transport and token refresh dependencies. */
export interface ConstantContactClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** OAuth refresh driver; omitted uses the manifest declaration. */ refreshDriver?: CredentialRefreshDriver<Credentials>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'refresh' | 'request' | 'read' | 'tooLarge';
}

/** Authenticated Constant Contact V3 client with renewable credentials. */
export class ConstantContactClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly refreshDriver: CredentialRefreshDriver<Credentials>;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate configuration without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ConstantContactClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Constant Contact configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Constant Contact request limits must be positive integers');
    this.refreshDriver = options.refreshDriver ?? new ManifestCredentialRefreshDriver<Credentials>({
      providerName: 'Constant Contact', auth: Authorization, credentialFields: CredentialFields,
    });
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getContact Query. */ getContact(): GetContactOperation { return new GetContactOperation(this); }

  /** Resolve a token and send one bounded contact read. */
  async get(call: Call, contactID: string): Promise<Exchange> {
    let token: string;
    try {
      const credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      token = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    } catch (error) {
      return emptyExchange(error instanceof ReauthorizationRequiredError ? 'credential' : 'refresh');
    }
    if (!token || !isHeaderSafeCredential(token)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/contacts/${contactID}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${token}` },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseURL === defaultBaseURL ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    let isTooLarge = false;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        isTooLarge = true;
      } else if (error instanceof ProviderTransportError) return emptyExchange('request');
      else return emptyExchange('request');
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'));
    if (isTooLarge || response.body.length > this.maxResponseBytes)
      return { status: response.status, body: new Uint8Array(), retryAfterMs, error: 'tooLarge' };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

/** Read one contact's selected identity and email fields. */
export class GetContactOperation implements Query<GetContactInput, ContactMetadata> {
  /** Bind the provider client. */ constructor(private readonly client: ConstantContactClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetContactDefinition; }
  /** Classify the provider outcome exactly as the Go implementation does. */
  async invoke(call: Call, input: GetContactInput): Promise<QueryAttempt<ContactMetadata>> {
    if (!input || !contactIDPattern.test(input.contactId))
      return terminal('defect', 'VALIDATION', 'contact ID must be a UUID');
    const exchange = await this.client.get(call, input.contactId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Constant Contact credentials are unavailable or invalid');
    if (exchange.error === 'refresh') return retry('AVAILABILITY', 'Constant Contact credentials could not be refreshed');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Constant Contact request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Constant Contact rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500)
      return retry('AVAILABILITY', 'Constant Contact is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Constant Contact did not find the contact');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Constant Contact rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Constant Contact response exceeded the configured limit');
    if (exchange.error) return retry('TRANSPORT', 'Constant Contact response was interrupted');
    const contact = decodeContact(exchange.body);
    if (!contact) return terminal('invalidResponse', 'PROTOCOL', 'Constant Contact returned invalid JSON');
    return newQueryBranch('found', contact);
  }
}

function emptyExchange(error: NonNullable<Exchange['error']>): Exchange {
  return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error };
}
function failure(kind: Failure['kind'], message: string): Failure {
  return { provider: 'constantcontact', operation: 'getContact', kind, message };
}
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<ContactMetadata> {
  return newQueryBranch(branch, emptyContact(), {}, failure(kind, message));
}
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<ContactMetadata> {
  return newQueryRetry(failure(kind, message), delay);
}
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
function optionalString(value: unknown): string | undefined {
  return value === undefined || value === null || value === '' ? undefined : typeof value === 'string' ? value : undefined;
}
function decodeContact(body: Uint8Array): ContactMetadata | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || typeof raw.contact_id !== 'string' || !contactIDPattern.test(raw.contact_id)) return undefined;
  const email: unknown = raw.email_address ?? {};
  if (!isObject(email)) return undefined;
  const fields = [raw.first_name, raw.last_name, email.address, email.permission_to_send];
  if (fields.some(value => value !== undefined && value !== null && typeof value !== 'string')) return undefined;
  const result: ContactMetadata = { contactId: raw.contact_id };
  const address = optionalString(email.address);
  const firstName = optionalString(raw.first_name);
  const lastName = optionalString(raw.last_name);
  const permission = optionalString(email.permission_to_send);
  if (address) result.emailAddress = address;
  if (firstName) result.firstName = firstName;
  if (lastName) result.lastName = lastName;
  if (permission) result.emailPermission = permission;
  return result;
}
