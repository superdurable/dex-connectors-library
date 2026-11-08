// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetAttendeeDefinition, ListEventsDefinition, ListOrganizationsDefinition } from './generated/definitions.js';
import type { AttendeeMetadata, Config, Credentials, EventMetadata, EventsPage, GetAttendeeInput, ListEventsInput, ListOrganizationsInput, OrganizationMetadata, OrganizationsPage } from './generated/models.js';

const defaultBaseUrl = 'https://www.eventbriteapi.com/v3';
const numericId = /^[0-9]{1,24}$/;
const emptyOrganizations = (): OrganizationsPage => ({ organizations: [], pageNumber: 0, pageCount: 0 });
const emptyEvents = (): EventsPage => ({ events: [], pageNumber: 0, pageCount: 0 });
const emptyAttendee = (): AttendeeMetadata => ({ id: '', eventId: '', status: '', createdAt: '' });

/** Optional transport dependency for provider tests and local Dex examples. */
export interface EventbriteClientOptions {
  /** One bounded provider request sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface ProviderOutcome {
  body?: Uint8Array;
  branch?: string;
  failure?: Failure;
  retry?: boolean;
  retryAfterMs?: number;
}

/** Authenticated Eventbrite API v3 client with call-time Private Token resolution. */
export class EventbriteClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate configuration without contacting Eventbrite. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: EventbriteClientOptions = {}) {
    if (!credentials) throw new TypeError('Eventbrite credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || defaultBaseUrl);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Eventbrite timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Eventbrite response limit is invalid');
    this.sendRequest = options.send ?? sendProviderRequest;
  }

  /** Bind the organization page Query. */ listOrganizations(): ListOrganizationsOperation { return new ListOrganizationsOperation(this); }
  /** Bind the event page Query. */ listEvents(): ListEventsOperation { return new ListEventsOperation(this); }
  /** Bind the attendee metadata Query. */ getAttendee(): GetAttendeeOperation { return new GetAttendeeOperation(this); }

  /** Send one read and classify transport, HTTP, and response-size outcomes. */
  async exchange(call: Call, operation: string, path: string, hasNotFound: boolean): Promise<ProviderOutcome> {
    let token: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = resolved.privateToken instanceof SecretString ? resolved.privateToken.reveal() : '';
    } catch { return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') }; }
    if (!token || !isHeaderSafeCredential(token)) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    const request: ProviderRequest = {
      url: `${this.baseUrl}${path}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${token}` },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl === defaultBaseUrl ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        if (response.status === 200) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Eventbrite response exceeded the configured limit') };
      } else return { retry: true, failure: failure(operation, 'TRANSPORT', 'Eventbrite request failed') };
    }
    const status = response.status;
    const retryAfter = headerValue(response.headers['retry-after']);
    if (status === 429) return { retry: true, retryAfterMs: parseRetryAfter(retryAfter), failure: failure(operation, 'RATE_LIMIT', 'Eventbrite rate limited the request') };
    if (status === 408 || status >= 500) return { retry: true, retryAfterMs: parseRetryAfter(retryAfter), failure: failure(operation, 'AVAILABILITY', 'Eventbrite is temporarily unavailable') };
    if (status === 404 && hasNotFound) return { branch: 'notFound', failure: failure(operation, 'NOT_FOUND', 'Eventbrite did not find the resource') };
    if (status !== 200) return { branch: 'providerRejected', failure: failure(operation, 'PROVIDER_REJECTION', `Eventbrite rejected HTTP ${status}`) };
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Eventbrite response exceeded the configured limit') };
    return { body: response.body };
  }
}

/** List organizations accessible to the connected account. */
export class ListOrganizationsOperation implements Query<ListOrganizationsInput, OrganizationsPage> {
  /** Bind the provider client. */ constructor(private readonly client: EventbriteClient) {}
  /** Return the generated operation definition. */ definition(): QueryDefinition { return ListOrganizationsDefinition; }
  /** Validate the one-based page and read its organization identities. */
  async invoke(call: Call, input: ListOrganizationsInput): Promise<QueryAttempt<OrganizationsPage>> {
    const page = pageNumber(input.page);
    if (page === undefined) return terminal('listOrganizations', 'defect', 'VALIDATION', 'page must be between 1 and 10000', emptyOrganizations());
    const result = await this.client.exchange(call, 'listOrganizations', `/users/me/organizations/?page=${page}`, false);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyOrganizations(), {}, result.failure);
    const document = parseJSON(result.body!);
    const pagination = parsePagination(document);
    if (!isObject(document) || !Array.isArray(document.organizations) || !pagination) return invalid('listOrganizations', emptyOrganizations());
    const organizations: OrganizationMetadata[] = [];
    for (const item of document.organizations) {
      if (!isObject(item) || !isNumericId(item.id) || (item.name !== undefined && typeof item.name !== 'string')) return invalid('listOrganizations', emptyOrganizations());
      organizations.push({ id: item.id, name: typeof item.name === 'string' ? item.name : '' });
    }
    return newQueryBranch('listed', { organizations, ...pagination });
  }
}

/** List one organization's events. */
export class ListEventsOperation implements Query<ListEventsInput, EventsPage> {
  /** Bind the provider client. */ constructor(private readonly client: EventbriteClient) {}
  /** Return the generated operation definition. */ definition(): QueryDefinition { return ListEventsDefinition; }
  /** Validate organization and page selection, then list event metadata. */
  async invoke(call: Call, input: ListEventsInput): Promise<QueryAttempt<EventsPage>> {
    const page = pageNumber(input.page);
    if (!isNumericId(input.organizationId) || page === undefined) return terminal('listEvents', 'defect', 'VALIDATION', 'organization ID or page is invalid', emptyEvents());
    const result = await this.client.exchange(call, 'listEvents', `/organizations/${input.organizationId}/events/?page=${page}`, true);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyEvents(), {}, result.failure);
    const document = parseJSON(result.body!);
    const pagination = parsePagination(document);
    if (!isObject(document) || !Array.isArray(document.events) || !pagination) return invalid('listEvents', emptyEvents());
    const events: EventMetadata[] = [];
    for (const item of document.events) {
      if (!isObject(item) || !isNumericId(item.id) || (item.name !== undefined && !isObject(item.name)) || (isObject(item.name) && item.name.text !== undefined && typeof item.name.text !== 'string') || (item.status !== undefined && typeof item.status !== 'string')) return invalid('listEvents', emptyEvents());
      events.push({ id: item.id, name: isObject(item.name) && typeof item.name.text === 'string' ? item.name.text : '', status: typeof item.status === 'string' ? item.status : '' });
    }
    return newQueryBranch('listed', { events, ...pagination });
  }
}

/** Read attendee identity and status without profile, answers, or barcode. */
export class GetAttendeeOperation implements Query<GetAttendeeInput, AttendeeMetadata> {
  /** Bind the provider client. */ constructor(private readonly client: EventbriteClient) {}
  /** Return the generated operation definition. */ definition(): QueryDefinition { return GetAttendeeDefinition; }
  /** Validate both numeric IDs and read one attendee's metadata. */
  async invoke(call: Call, input: GetAttendeeInput): Promise<QueryAttempt<AttendeeMetadata>> {
    if (!isNumericId(input.eventId) || !isNumericId(input.attendeeId)) return terminal('getAttendee', 'defect', 'VALIDATION', 'event ID or attendee ID is invalid', emptyAttendee());
    const result = await this.client.exchange(call, 'getAttendee', `/events/${input.eventId}/attendees/${input.attendeeId}/`, true);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyAttendee(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document) || !isNumericId(document.id) || !isNumericId(document.event_id) || (document.status !== undefined && typeof document.status !== 'string') || (document.created !== undefined && typeof document.created !== 'string')) return invalid('getAttendee', emptyAttendee());
    return newQueryBranch('found', { id: document.id, eventId: document.event_id, status: typeof document.status === 'string' ? document.status : '', createdAt: typeof document.created === 'string' ? document.created : '' });
  }
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'eventbrite', operation, kind, message }; }
function terminal<Output>(operation: string, branch: string, kind: Failure['kind'], message: string, empty: Output): QueryAttempt<Output> { return newQueryBranch(branch, empty, {}, failure(operation, kind, message)); }
function invalid<Output>(operation: string, empty: Output): QueryAttempt<Output> { return terminal(operation, 'invalidResponse', 'PROTOCOL', 'Eventbrite returned invalid JSON', empty); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isNumericId(value: unknown): value is string { return typeof value === 'string' && numericId.test(value); }
function pageNumber(value: unknown): number | undefined {
  if (value === undefined || value === 0) return 1;
  return typeof value === 'number' && Number.isInteger(value) && value >= 1 && value <= 10_000 ? value : undefined;
}
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function parsePagination(document: unknown): { pageNumber: number; pageCount: number } | undefined {
  if (!isObject(document) || !isObject(document.pagination)) return undefined;
  const pageNumber = document.pagination.page_number;
  const pageCount = document.pagination.page_count;
  if (typeof pageNumber !== 'number' || !Number.isInteger(pageNumber) || pageNumber <= 0 || typeof pageCount !== 'number' || !Number.isInteger(pageCount) || pageCount < 0) return undefined;
  return { pageNumber, pageCount };
}
function headerValue(value: string | string[] | undefined): string { return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
