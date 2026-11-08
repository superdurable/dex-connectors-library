// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type MutationDefinition, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { CheckpointError, InsufficientTimeError, newFence } from '@superdurable/connectors-sdk/singledispatch';
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
    if (!input || !Number.isSafeInteger(input.pageSize) || input.pageSize < 0 || input.pageSize > 1000 || !validPageToken(input.pageToken))
      return queryBranch('listContacts', 'defect', empty, 'VALIDATION', 'contact page input is invalid');
    const pageSize = input.pageSize || 100;
    const query: Record<string, readonly string[]> = { pageSize: [String(pageSize)], personFields: ['names,emailAddresses'] };
    if (input.pageToken) query.pageToken = [input.pageToken];
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
  /** Retry only a proven unsent request or Google's definite 429 and 503 responses. */
  async invoke(call: Call, input: CreateContactInput): Promise<MutationAttempt<Contact>> {
    const empty = emptyContact();
    let fence: ReturnType<typeof newFence>;
    try { fence = newFence(call, 30_000); }
    catch { return mutationBranch('defect', empty, 'LOCAL_DEFECT', 'Google Contacts dispatch fence is unavailable'); }
    if (fence.decide().kind !== 'sendAllowed')
      return mutationUncertain('TRANSPORT', 'contact creation outcome cannot be confirmed; inspect Contacts before creating another');
    if (!validCreateInput(input)) return mutationBranch('defect', empty, 'VALIDATION', 'contact name or email is invalid');
    const person: { names: Array<{ givenName: string; familyName?: string }>; emailAddresses?: Array<{ value: string }> } = {
      names: [{ givenName: input.givenName, ...(input.familyName ? { familyName: input.familyName } : {}) }],
    };
    if (input.emailAddress) person.emailAddresses = [{ value: input.emailAddress }];
    const body = Buffer.from(JSON.stringify(person));
    const credential = await this.client.resolveToken(call);
    if (credential.error === 'credential')
      return mutationBranch('defect', empty, 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
    if (credential.error) {
      try { await fence.recordNotSent(); }
      catch { return mutationRetry('TRANSPORT', 'credential refresh checkpoint is unavailable'); }
      return mutationRetry('TRANSPORT', 'connection credentials could not be refreshed');
    }
    const path = `/v1/people:createContact?${encodeQueryValues({ personFields: ['names,emailAddresses'] })}`;
    let exchange: ContactsExchange;
    try { exchange = await fence.dispatch(signal => this.client.request(call, path, { method: 'POST', body, token: credential.token, signal })); }
    catch (error) {
      if (error instanceof CheckpointError || error instanceof InsufficientTimeError)
        return mutationRetry('TRANSPORT', 'contact dispatch was not started');
      return mutationUncertain('TRANSPORT', 'contact creation outcome is unknown');
    }
    if (exchange.status === 429 || exchange.status === 503) {
      try { await fence.recordCanResend(); }
      catch { return mutationRetry('AVAILABILITY', 'Google retry checkpoint is unavailable'); }
      return mutationRetry(exchange.status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', 'Google did not create the contact', exchange.retryAfterMs);
    }
    if (exchange.status >= 500 || [408, 409].includes(exchange.status))
      return mutationUncertain('AVAILABILITY', 'contact creation outcome is unknown');
    if (exchange.error) {
      if (exchange.error === 'request' && exchange.isProvablyNotSent) {
        try { await fence.recordCanResend(); }
        catch { return mutationUncertain('TRANSPORT', 'contact creation outcome is unknown'); }
        return mutationRetry('TRANSPORT', 'Google could not be reached before sending');
      }
      return mutationUncertain('TRANSPORT', 'contact creation outcome is unknown');
    }
    if (exchange.status === 401) return mutationBranch('providerRejected', empty, 'AUTHENTICATION', 'Google rejected contact authorization');
    if (exchange.status === 403) return mutationBranch('providerRejected', empty, 'AUTHORIZATION', 'Google denied contact creation');
    if (exchange.status >= 400 && exchange.status < 500)
      return mutationBranch('providerRejected', empty, 'PROVIDER_REJECTION', `Google rejected HTTP ${exchange.status}`);
    if (exchange.status !== 200 && exchange.status !== 201)
      return mutationUncertain('PROTOCOL', 'Google returned an unexpected contact response');
    const contact = projectContact(parseJSON(exchange.body));
    if (!contact) return mutationUncertain('PROTOCOL', 'Google created a contact but returned no usable resource name');
    try { await fence.recordProviderId(contact.resourceName); }
    catch { /* A lost provider ID heartbeat still forbids another send. */ }
    return newMutationBranch('created', contact);
  }
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
  if (!input || typeof input.givenName !== 'string' || typeof input.familyName !== 'string' || typeof input.emailAddress !== 'string') return false;
  if (![input.givenName, input.familyName, input.emailAddress].every(value => Buffer.from(value).toString('utf8') === value)) return false;
  if (!input.givenName.trim() || Buffer.byteLength(input.givenName) > 256 || Buffer.byteLength(input.familyName) > 256 || Buffer.byteLength(input.emailAddress) > 320) return false;
  return !/[\r\n]/.test(input.emailAddress) && (!input.emailAddress || (input.emailAddress.includes('@') && !/[ \t]/.test(input.emailAddress)));
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
