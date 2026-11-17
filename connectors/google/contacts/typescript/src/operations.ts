// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { definitionMaxSends, newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type MutationDefinition, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { CheckpointError, InsufficientTimeError, newFence, type Fence } from '@superdurable/connectors-sdk/singledispatch';
import { CreateContactDefinition, GetContactDefinition, ListContactsDefinition } from './generated/definitions.js';
import type { Contact, ContactsPage, CreateContactInput, GetContactInput, ListContactsInput } from './generated/models.js';
import type { ContactsClient, ContactsExchange } from './client.js';

const resourcePattern = /^people\/[A-Za-z0-9_-]{1,128}$/;
const emptyContact = (): Contact => ({ resourceName: '', displayName: '', emailAddress: '' });
const emptyPage = (): ContactsPage => ({ contacts: [], nextPageToken: '' });

/** List one page of the connected user's contacts. */
export class ListContactsOperation implements Query<ListContactsInput, ContactsPage> {
  /** Bind the People API client. */ constructor(private readonly client: ContactsClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListContactsDefinition; }
  /** Send one bounded page request with a fixed field mask. */
  async invoke(call: Call, input: ListContactsInput): Promise<QueryAttempt<ContactsPage>> {
    const empty = emptyPage();
    const requestedPageSize = input?.pageSize ?? 0; const pageToken = input?.pageToken ?? '';
    if (!input || !Number.isSafeInteger(requestedPageSize) || requestedPageSize < 0 || requestedPageSize > 1000 || !validPageToken(pageToken))
      return queryBranch('listContacts', 'defect', empty, 'VALIDATION', 'contact page input is invalid');
    const pageSize = requestedPageSize || 100;
    const query: Record<string, readonly string[]> = { pageSize: [String(pageSize)], personFields: ['names,emailAddresses'] };
    if (pageToken) query.pageToken = [pageToken];
    const exchange = await this.client.request(call, `/v1/people/me/connections?${encodeQueryValues(query)}`);
    const failed = classifyRead('listContacts', exchange, empty);
    if (failed) return failed;
    const document = parseJSON(exchange.body);
    if (!isObject(document)) return queryBranch('listContacts', 'invalidResponse', empty, 'PROTOCOL', 'Google returned invalid contact page JSON');
    const entries = document.connections ?? [];
    const nextPageToken = document.nextPageToken ?? '';
    if (!Array.isArray(entries) || entries.length > pageSize || !validPageToken(nextPageToken))
      return queryBranch('listContacts', 'invalidResponse', empty, 'PROTOCOL', 'Google returned invalid contact page JSON');
    const contacts: Contact[] = [];
    for (const entry of entries) {
      const contact = projectContact(entry);
      if (!contact) return queryBranch('listContacts', 'invalidResponse', empty, 'PROTOCOL', 'Google returned an invalid contact resource');
      contacts.push(contact);
    }
    return newQueryBranch('listed', { contacts, nextPageToken });
  }
}

/** Get one contact by People API resource name. */
export class GetContactOperation implements Query<GetContactInput, Contact> {
  /** Bind the People API client. */ constructor(private readonly client: ContactsClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetContactDefinition; }
  /** Project only the first display name and email address. */
  async invoke(call: Call, input: GetContactInput): Promise<QueryAttempt<Contact>> {
    const empty = emptyContact();
    if (!input || !validResourceName(input.resourceName))
      return queryBranch('getContact', 'defect', empty, 'VALIDATION', 'contact resource name is invalid');
    const query = encodeQueryValues({ personFields: ['names,emailAddresses'] });
    const exchange = await this.client.request(call, `/v1/${input.resourceName}?${query}`);
    if (exchange.status === 404 && !exchange.error)
      return queryBranch('getContact', 'notFound', empty, 'PROVIDER_REJECTION', 'contact was not found');
    const failed = classifyRead('getContact', exchange, empty);
    if (failed) return failed;
    const contact = projectContact(parseJSON(exchange.body));
    if (!contact || contact.resourceName !== input.resourceName)
      return queryBranch('getContact', 'invalidResponse', empty, 'PROTOCOL', 'Google returned the wrong contact resource');
    return newQueryBranch('found', contact);
  }
}

/** Create one contact with a durable single-dispatch fence. */
export class CreateContactOperation implements Mutation<CreateContactInput, Contact> {
  /** Bind the People API client. */ constructor(private readonly client: ContactsClient) {}
  /** Return the generated Mutation contract. */ definition(): MutationDefinition { return CreateContactDefinition; }
  /** Record Call identity without sending it as a provider deduplication key. */ idempotencyKey(callId: string): string { return callId; }
  /**
   * Create one contact. A saved resource name is read back instead of resent, an unknown outcome is
   * resent while the send cap allows, and uncertain is selected only at the cap.
   */
  async invoke(call: Call, input: CreateContactInput): Promise<MutationAttempt<Contact>> {
    const empty = emptyContact();
    if (!validCreateInput(input)) return mutationBranch('defect', empty, 'VALIDATION', 'contact name or email is invalid');
    const person: { names: Array<{ givenName: string; familyName?: string }>; emailAddresses?: Array<{ value: string }> } = {
      names: [{ givenName: input.givenName, ...(input.familyName ? { familyName: input.familyName } : {}) }],
    };
    if (input.emailAddress) person.emailAddresses = [{ value: input.emailAddress }];
    const body = Buffer.from(JSON.stringify(person));
    const operationMaxSends = definitionMaxSends(CreateContactDefinition);
    let fence: Fence;
    try { fence = newFence(call, 30_000, { maxSends: operationMaxSends }); }
    catch { return mutationBranch('defect', empty, 'LOCAL_DEFECT', 'Google Contacts dispatch fence is unavailable'); }
    const decision = fence.decide();
    const savedResourceName = decision.marker?.providerId ?? '';
    if (decision.kind === 'failClosed')
      return mutationRetry('AVAILABILITY', 'contact dispatch checkpoint is unreadable; no contact was sent in this attempt');
    if (decision.kind === 'readBackRequired') {
      if (savedResourceName) return this.readBackCreatedContact(call, savedResourceName);
      return mutationUncertain('TRANSPORT', 'contact creation reached the send cap without confirmation; inspect Contacts before creating another');
    }
    // Google returns a resource name only for a contact it created, so a saved one is never resent.
    if (decision.kind === 'readBackThenResendAllowed' && savedResourceName) return this.readBackCreatedContact(call, savedResourceName);
    const priorSendUpperBound = decision.priorSendUpperBound ?? 0;
    // This attempt's send is the last one the cap allows when no further send remains.
    const canResendUnknownOutcome = priorSendUpperBound + 1 < effectiveMaxSends(call, operationMaxSends);
    const hasPossibleEarlierSend = priorSendUpperBound > 0;
    const credential = await this.client.resolveToken(call);
    if (credential.error === 'credential')
      return localFailure(hasPossibleEarlierSend, 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
    if (credential.error) {
      if (decision.kind === 'sendAllowed') {
        try { await fence.recordNotSent(); }
        catch { return mutationRetry('TRANSPORT', 'credential refresh checkpoint is unavailable'); }
      }
      return mutationRetry('TRANSPORT', 'connection credentials could not be refreshed');
    }
    const path = `/v1/people:createContact?${encodeQueryValues({ personFields: ['names,emailAddresses'] })}`;
    let exchange: ContactsExchange;
    try { exchange = await fence.dispatch(signal => this.client.request(call, path, { method: 'POST', body, token: credential.token, signal })); }
    catch (error) {
      if (error instanceof CheckpointError || error instanceof InsufficientTimeError)
        return mutationRetry('TRANSPORT', 'contact dispatch was not started');
      return resendOrSelectUncertain(canResendUnknownOutcome, 'TRANSPORT', 'contact creation outcome is unknown');
    }
    if (exchange.error === 'request') {
      if (exchange.isProvablyNotSent) {
        try { await fence.recordCanResend(); }
        catch { return resendOrSelectUncertain(canResendUnknownOutcome, 'TRANSPORT', 'contact creation outcome is unknown'); }
        return mutationRetry('TRANSPORT', 'Google could not be reached before sending');
      }
      return resendOrSelectUncertain(canResendUnknownOutcome, 'TRANSPORT', 'contact creation outcome is unknown');
    }
    if (exchange.status === 429 || exchange.status === 503) {
      try { await fence.recordCanResend(); }
      catch { return mutationRetry('AVAILABILITY', 'Google retry checkpoint is unavailable'); }
      return mutationRetry(exchange.status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', 'Google did not create the contact', exchange.retryAfterMs);
    }
    const contact = exchange.error ? undefined : projectContact(parseJSON(exchange.body));
    if (contact) {
      try { await fence.recordProviderId(contact.resourceName); }
      catch { /* A lost provider ID heartbeat counts this send, so the cap still holds. */ }
    }
    if (definiteRefusalStatuses.has(exchange.status) && !contact) return rejectOrSelectUncertain(fence, hasPossibleEarlierSend, exchange.status);
    if (exchange.status !== 200 && exchange.status !== 201)
      return resendOrSelectUncertain(canResendUnknownOutcome, 'AVAILABILITY', `contact creation outcome is unknown after HTTP ${exchange.status}`, exchange.retryAfterMs);
    if (exchange.error) return resendOrSelectUncertain(canResendUnknownOutcome, 'TRANSPORT', 'Google contact response could not be read safely');
    if (!contact) return resendOrSelectUncertain(canResendUnknownOutcome, 'PROTOCOL', 'Google returned no usable contact resource name');
    return newMutationBranch('created', contact);
  }

  /** Confirm a contact whose resource name an earlier attempt saved. */
  private async readBackCreatedContact(call: Call, resourceName: string): Promise<MutationAttempt<Contact>> {
    if (!validResourceName(resourceName))
      return mutationUncertain('PROTOCOL', 'the saved contact resource name is invalid; inspect Contacts before creating another');
    const exchange = await this.client.request(call, `/v1/${resourceName}?${encodeQueryValues({ personFields: ['names,emailAddresses'] })}`);
    if (exchange.error === 'credential')
      return mutationUncertain('AUTHENTICATION', 'contact read-back credentials are unavailable; reconnect the connection to confirm the contact');
    if (exchange.error === 'refresh' || exchange.error === 'request') return mutationRetry('TRANSPORT', 'contact read-back request failed');
    if (exchange.status === 429) return mutationRetry('RATE_LIMIT', 'Google rate limited the contact read-back', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500)
      return mutationRetry('AVAILABILITY', 'Google Contacts is temporarily unavailable for read-back', exchange.retryAfterMs);
    const contact = exchange.status !== 200 || exchange.error ? undefined : projectContact(parseJSON(exchange.body));
    if (!contact || contact.resourceName !== resourceName)
      return mutationUncertain('PROTOCOL', 'contact read-back could not confirm the contact; inspect Contacts before creating another');
    return newMutationBranch('created', contact);
  }
}

// Only these statuses prove Google refused a write; every other status is an unknown outcome.
const definiteRefusalStatuses = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);

/** Retry a possibly created contact while the send cap allows another send. */
function resendOrSelectUncertain(canResendUnknownOutcome: boolean, kind: Failure['kind'], message: string, delay = 0): MutationAttempt<Contact> {
  // The claim stays the last marker, so the next attempt counts this send.
  if (canResendUnknownOutcome) return mutationRetry(kind, message, delay);
  return mutationUncertain(kind, `${message}; the send cap is reached, so inspect Contacts before creating another`);
}
/** Select providerRejected only when no earlier send may have created the contact. */
async function rejectOrSelectUncertain(fence: Fence, hasPossibleEarlierSend: boolean, status: number): Promise<MutationAttempt<Contact>> {
  const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION';
  const message = `Google rejected HTTP ${status}`;
  if (hasPossibleEarlierSend) return mutationUncertain(kind, `${message} after an earlier unconfirmed send; inspect Contacts before creating another`);
  try { await fence.recordCanResend(); }
  catch { /* The refusal ends the Step, so a lost checkpoint cannot cause a resend. */ }
  return mutationBranch('providerRejected', emptyContact(), kind, message);
}
/** Select defect only when no earlier send of this call may have created the contact. */
function localFailure(hasPossibleEarlierSend: boolean, kind: Failure['kind'], message: string): MutationAttempt<Contact> {
  if (hasPossibleEarlierSend) return mutationUncertain(kind, `${message} after an earlier unconfirmed send; inspect Contacts before creating another`);
  return mutationBranch('defect', emptyContact(), kind, message);
}
function effectiveMaxSends(call: Call, operationMaxSends: number): number {
  return operationMaxSends >= 2 ? call.maxSendsOverride ?? operationMaxSends : 1;
}

function classifyRead<Output>(operation: string, exchange: ContactsExchange, empty: Output): QueryAttempt<Output> | undefined {
  if (exchange.error === 'credential') return queryBranch(operation, 'defect', empty, 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
  if (exchange.error === 'request' || exchange.error === 'refresh') return queryRetry(operation, 'TRANSPORT', 'Google Contacts request failed');
  if (exchange.status === 429) return queryRetry(operation, 'RATE_LIMIT', 'Google rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return queryRetry(operation, 'AVAILABILITY', 'Google Contacts is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.status !== 200) return queryBranch(operation, 'providerRejected', empty, 'PROVIDER_REJECTION', `Google rejected HTTP ${exchange.status}`);
  if (exchange.error === 'tooLarge') return queryBranch(operation, 'invalidResponse', empty, 'RESPONSE_TOO_LARGE', 'Google response exceeded the configured limit');
  return undefined;
}
function projectContact(value: unknown): Contact | undefined {
  if (!isObject(value) || !validResourceName(value.resourceName)) return undefined;
  const names = value.names ?? []; const emails = value.emailAddresses ?? [];
  if (!Array.isArray(names) || !Array.isArray(emails) ||
    names.some(name => !isObject(name) || typeof (name.displayName ?? '') !== 'string') ||
    emails.some(email => !isObject(email) || typeof (email.value ?? '') !== 'string')) return undefined;
  const firstName = names[0] as Record<string, unknown> | undefined;
  const firstEmail = emails[0] as Record<string, unknown> | undefined;
  return { resourceName: value.resourceName, displayName: firstName?.displayName as string ?? '', emailAddress: firstEmail?.value as string ?? '' };
}
function validResourceName(value: unknown): value is string { return typeof value === 'string' && resourcePattern.test(value); }
function validPageToken(value: unknown): value is string { return typeof value === 'string' && Buffer.byteLength(value) <= 4096 && !/[\r\n]/.test(value); }
function validCreateInput(input: CreateContactInput): boolean {
  if (!input) return false;
  const { givenName, familyName = '', emailAddress = '' } = input;
  if (typeof givenName !== 'string' || typeof familyName !== 'string' || typeof emailAddress !== 'string') return false;
  if (![givenName, familyName, emailAddress].every(value => Buffer.from(value).toString('utf8') === value)) return false;
  if (!givenName.trim() || Buffer.byteLength(givenName) > 256 || Buffer.byteLength(familyName) > 256 || Buffer.byteLength(emailAddress) > 320) return false;
  return !/[\r\n]/.test(emailAddress) && (!emailAddress || (emailAddress.includes('@') && !/[ \t]/.test(emailAddress)));
}
function queryBranch<Output>(operation: string, branch: string, empty: Output, kind: Failure['kind'], message: string): QueryAttempt<Output> {
  return newQueryBranch(branch, empty, {}, failure(operation, kind, message));
}
function queryRetry<Output>(operation: string, kind: Failure['kind'], message: string, delay = 0): QueryAttempt<Output> {
  return newQueryRetry(failure(operation, kind, message), delay);
}
function mutationBranch(branch: string, empty: Contact, kind: Failure['kind'], message: string): MutationAttempt<Contact> {
  return newMutationBranch(branch, empty, {}, failure('createContact', kind, message));
}
function mutationRetry(kind: Failure['kind'], message: string, delay = 0): MutationAttempt<Contact> {
  return newMutationRetry(failure('createContact', kind, message), delay);
}
function mutationUncertain(kind: Failure['kind'], message: string): MutationAttempt<Contact> {
  return newMutationUncertain(emptyContact(), failure('createContact', kind, message), {});
}
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'google-contacts', operation, message }; }
function encodeQueryValues(values: Record<string, readonly string[]>): string {
  const escape = (value: string): string => encodeURIComponent(value)
    .replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`)
    .replace(/%20/g, '+');
  const fields: string[] = [];
  for (const key of Object.keys(values).sort()) for (const value of values[key] ?? []) fields.push(`${escape(key)}=${escape(value)}`);
  return fields.join('&');
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
