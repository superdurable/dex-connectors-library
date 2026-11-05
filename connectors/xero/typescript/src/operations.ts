// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { CreateInvoiceDefinition, FindContactByEmailDefinition, GetInvoiceDefinition, ListContactsDefinition, ListInvoicesDefinition, RecordPaymentDefinition } from './generated/definitions.js';
import type { Contact, ContactPage, CreateInvoiceInput, CreateInvoiceOutput, FindContactByEmailInput, FindContactByEmailOutput, GetInvoiceInput, Invoice, InvoicePage, ListContactsInput, ListInvoicesInput, Payment, RecordPaymentInput, RecordPaymentOutput } from './generated/models.js';
import { escapeGoPath, type XeroClient, type XeroExchange } from './client.js';
import emailAddresses from 'email-addresses';
import { isIP } from 'node:net';

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const accountingCodePattern = /^[A-Za-z0-9][A-Za-z0-9 ._/-]{0,49}$/;
const decimalPattern = /^-?(?:0|[1-9][0-9]{0,17})(?:\.[0-9]{1,10})?$/;
const zeroTime = '0001-01-01T00:00:00Z';
const zeroInvoice: Invoice = { invoiceId: '', type: '', status: '', contact: { contactId: '' }, updatedAt: zeroTime };
const zeroPayment: Payment = { paymentId: '', amount: '', status: '', invoice: { invoiceId: '' }, account: {}, updatedAt: zeroTime };
const zeroContactPage: ContactPage = { contacts: [], page: 0, pageSize: 0, hasMorePages: false };
const zeroInvoicePage: InvoicePage = { invoices: [], page: 0, pageSize: 0, hasMorePages: false };

/** Read one bounded Xero contact page. */
export class ListContactsOperation implements Query<ListContactsInput, ContactPage> {
  /** Bind the client. */ constructor(private readonly client: XeroClient) {}
  /** Return the manifest definition. */ definition() { return ListContactsDefinition; }
  /** Validate, send, and decode a page. */
  async invoke(call: Call, input: ListContactsInput): Promise<QueryAttempt<ContactPage>> {
    let bounds: Bounds; let query: Record<string, string>;
    try {
      bounds = pageBounds(input.page, input.pageSize);
      const term = (input.searchTerm ?? '').trim();
      validateSingleLine('searchTerm', term, 100);
      validateIDs('contactIds', input.contactIds);
      query = { page: String(bounds.page), pageSize: String(bounds.pageSize) };
      if (term) query.searchTerm = term;
      if (input.contactIds?.length) query.IDs = input.contactIds.join(',');
      if (input.includesArchived) query.includeArchived = 'true';
    } catch (error) { return defectQuery('listContacts', zeroContactPage, error); }
    const result = await this.client.exchange(call, 'listContacts', { method: 'GET', path: '/Contacts', query, ...(input.modifiedSince ? { modifiedSince: input.modifiedSince } : {}) });
    const receipt = this.client.receipt(call, result);
    if (result.outcome !== 'succeeded') return queryExchange(result, zeroContactPage, receipt);
    try { return newQueryBranch('listed', decodeContactPage(result.response!.body, bounds), receipt); }
    catch (error) { return invalidQuery('listContacts', zeroContactPage, receipt, 'Xero returned an invalid contact page: ', error); }
  }
}

/** Find one contact with an exact case-insensitive email match. */
export class FindContactByEmailOperation implements Query<FindContactByEmailInput, FindContactByEmailOutput> {
  /** Bind the client. */ constructor(private readonly client: XeroClient) {}
  /** Return the manifest definition. */ definition() { return FindContactByEmailDefinition; }
  /** Filter and check the bounded contact result. */
  async invoke(call: Call, input: FindContactByEmailInput): Promise<QueryAttempt<FindContactByEmailOutput>> {
    const email = input.emailAddress.trim();
    const requested: FindContactByEmailOutput = { emailAddress: email };
    try { validateEmail(email); } catch (error) { return defectQuery('findContactByEmail', requested, error); }
    const query: Record<string, string> = { page: '1', pageSize: '10', where: `EmailAddress=="${email}"` };
    if (input.includesArchived) query.includeArchived = 'true';
    const result = await this.client.exchange(call, 'findContactByEmail', { method: 'GET', path: '/Contacts', query });
    const receipt = this.client.receipt(call, result);
    if (result.outcome !== 'succeeded') return queryExchange(result, requested, receipt);
    let page: ContactPage;
    try { page = decodeContactPage(result.response!.body, { page: 1, pageSize: 10 }); }
    catch (error) { return invalidQuery('findContactByEmail', requested, receipt, 'Xero returned an invalid contact list: ', error); }
    const matches = page.contacts!.filter(contact => contact.emailAddress?.trim().toLowerCase() === email.toLowerCase() && (input.includesArchived || contact.status === 'ACTIVE' || !contact.status));
    if (!matches.length) return newQueryBranch('notFound', requested, receipt, failure('NOT_FOUND', 'findContactByEmail', 'no Xero contact has the email address'));
    if (matches.length === 1) return newQueryBranch('found', { ...requested, contact: matches[0]! }, { ...receipt, providerObjectId: matches[0]!.contactId });
    return newQueryBranch('ambiguous', { ...requested, matches }, receipt, failure('CONFLICT', 'findContactByEmail', 'several Xero contacts have the email address'));
  }
}

/** Read one invoice by Xero UUID or document number. */
export class GetInvoiceOperation implements Query<GetInvoiceInput, Invoice> {
  /** Bind the client. */ constructor(private readonly client: XeroClient) {}
  /** Return the manifest definition. */ definition() { return GetInvoiceDefinition; }
  /** Validate the identifier and decode the single invoice. */
  async invoke(call: Call, input: GetInvoiceInput): Promise<QueryAttempt<Invoice>> {
    let identifier: string;
    try { identifier = invoiceIdentifier(input); } catch (error) { return defectQuery('getInvoice', zeroInvoice, error); }
    const result = await this.client.exchange(call, 'getInvoice', { method: 'GET', path: `/Invoices/${escapeGoPath(identifier)}`, query: { unitdp: '4' } });
    const receipt = this.client.receipt(call, result);
    if (result.outcome !== 'succeeded') return queryExchange(result, zeroInvoice, receipt, true);
    try {
      const wire = singleResource(result.response!.body, 'Invoices');
      if (stringField(wire, 'InvoiceID').toLowerCase() !== identifier.toLowerCase() && stringField(wire, 'InvoiceNumber') !== identifier) throw new Error('response describes another invoice');
      const invoice = decodeInvoice(wire);
      return newQueryBranch('found', invoice, { ...receipt, providerObjectId: invoice.invoiceId });
    } catch (error) { return invalidQuery('getInvoice', zeroInvoice, receipt, 'Xero returned an invalid invoice: ', error); }
  }
}

/** Read one bounded Xero invoice page. */
export class ListInvoicesOperation implements Query<ListInvoicesInput, InvoicePage> {
  /** Bind the client. */ constructor(private readonly client: XeroClient) {}
  /** Return the manifest definition. */ definition() { return ListInvoicesDefinition; }
  /** Validate filters and decode the page. */
  async invoke(call: Call, input: ListInvoicesInput): Promise<QueryAttempt<InvoicePage>> {
    let bounds: Bounds; let query: Record<string, string>;
    try {
      bounds = pageBounds(input.page, input.pageSize);
      for (const status of input.statuses ?? []) if (!['DRAFT', 'SUBMITTED', 'DELETED', 'AUTHORISED', 'PAID', 'VOIDED'].includes(status)) throw new Error('statuses must contain Xero statuses: DRAFT, SUBMITTED, DELETED, AUTHORISED, PAID, or VOIDED');
      validateIDs('contactIds', input.contactIds);
      if ((input.invoiceNumbers?.length ?? 0) > 40) throw new Error('invoiceNumbers can list at most 40 numbers');
      for (const number of input.invoiceNumbers ?? []) { if (!number || number.includes(',')) throw new Error('invoiceNumbers cannot contain a blank number or a comma'); validateInvoiceNumber('invoiceNumbers', number); }
      if (input.type) validateInvoiceType(input.type);
      const reference = (input.reference ?? '').trim(); validateSingleLine('reference', reference, 255);
      if (/["\\]/.test(reference)) throw new Error('reference cannot contain a double quote or a backslash');
      query = { page: String(bounds.page), pageSize: String(bounds.pageSize), unitdp: '4' };
      if (input.statuses?.length) query.Statuses = input.statuses.join(',');
      if (input.contactIds?.length) query.ContactIDs = input.contactIds.join(',');
      if (input.invoiceNumbers?.length) query.InvoiceNumbers = input.invoiceNumbers.join(',');
      const where = [input.type ? `Type=="${input.type}"` : '', reference ? `Reference=="${reference}"` : ''].filter(Boolean).join(' AND ');
      if (where) query.where = where;
    } catch (error) { return defectQuery('listInvoices', zeroInvoicePage, error); }
    const result = await this.client.exchange(call, 'listInvoices', { method: 'GET', path: '/Invoices', query, ...(input.modifiedSince ? { modifiedSince: input.modifiedSince } : {}) });
    const receipt = this.client.receipt(call, result);
    if (result.outcome !== 'succeeded') return queryExchange(result, zeroInvoicePage, receipt);
    try { return newQueryBranch('listed', decodeInvoicePage(result.response!.body, bounds), receipt); }
    catch (error) { return invalidQuery('listInvoices', zeroInvoicePage, receipt, 'Xero returned an invalid invoice page: ', error); }
  }
}

/** Create one invoice under the Dex-derived Xero idempotency key. */
export class CreateInvoiceOperation implements Mutation<CreateInvoiceInput, CreateInvoiceOutput> {
  /** Bind the client. */ constructor(private readonly client: XeroClient) {}
  /** Return the manifest definition. */ definition() { return CreateInvoiceDefinition; }
  /** Reuse one Call ID across every retry. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate, send, and decode one invoice create. */
  async invoke(call: Call, input: CreateInvoiceInput): Promise<MutationAttempt<CreateInvoiceOutput>> {
    const requested: CreateInvoiceOutput = { invoice: zeroInvoice, ...(input.reference?.trim() ? { reference: input.reference.trim() } : {}) };
    let payload: Uint8Array;
    try { payload = createInvoicePayload(input); } catch (error) { return defectMutation('createInvoice', requested, error); }
    const result = await this.client.exchange(call, 'createInvoice', { method: 'PUT', path: '/Invoices', query: { unitdp: '4' }, payload, isMutation: true });
    const receipt = this.client.receipt(call, result);
    if (result.outcome !== 'succeeded') return mutationExchange(result, requested, receipt);
    let wire: Record<string, unknown>;
    try { wire = singleResource(result.response!.body, 'Invoices'); }
    catch (error) { return uncertainMutation('createInvoice', requested, receipt, 'Xero accepted the invoice but returned an unusable invoice: ', error); }
    let validationCount: number;
    let hasErrors: boolean;
    try { hasErrors = boolField(wire, 'HasErrors'); validationCount = arrayField(wire, 'ValidationErrors').length; arrayField(wire, 'Warnings'); }
    catch (error) { return uncertainMutation('createInvoice', requested, receipt, 'Xero accepted the invoice but returned an unusable invoice: ', error); }
    if (hasErrors || validationCount) return newMutationBranch('providerRejected', requested, receipt, failure('VALIDATION', 'createInvoice', `Xero rejected the invoice${validationCount ? ` [${validationCount} validation error${validationCount === 1 ? '' : 's'}]` : ''}`));
    try {
      const invoice = decodeInvoice(wire);
      const output: CreateInvoiceOutput = { invoice, ...requested.reference ? { reference: requested.reference } : {}, ...arrayField(wire, 'Warnings').length ? { warningCount: arrayField(wire, 'Warnings').length } : {} };
      return newMutationBranch('created', output, { ...receipt, providerObjectId: invoice.invoiceId });
    } catch (error) { return uncertainMutation('createInvoice', requested, receipt, 'Xero accepted the invoice but returned an unusable invoice: ', error); }
  }
}

/** Record one payment under the Dex-derived Xero idempotency key. */
export class RecordPaymentOperation implements Mutation<RecordPaymentInput, RecordPaymentOutput> {
  /** Bind the client. */ constructor(private readonly client: XeroClient) {}
  /** Return the manifest definition. */ definition() { return RecordPaymentDefinition; }
  /** Reuse one Call ID across every retry. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate, send, and decode one payment. */
  async invoke(call: Call, input: RecordPaymentInput): Promise<MutationAttempt<RecordPaymentOutput>> {
    const requested: RecordPaymentOutput = { payment: zeroPayment, invoiceId: input.invoiceId.trim().toLowerCase() };
    let payload: Uint8Array;
    try { payload = recordPaymentPayload(input); } catch (error) { return defectMutation('recordPayment', requested, error); }
    const result = await this.client.exchange(call, 'recordPayment', { method: 'PUT', path: '/Payments', payload, isMutation: true });
    const receipt = this.client.receipt(call, result);
    if (result.outcome !== 'succeeded') return mutationExchange(result, requested, receipt);
    let wire: Record<string, unknown>;
    try { wire = singleResource(result.response!.body, 'Payments'); }
    catch (error) { return uncertainMutation('recordPayment', requested, receipt, 'Xero accepted the payment but returned an unusable payment: ', error); }
    let validationCount: number;
    let hasErrors: boolean;
    try { hasErrors = boolField(wire, 'HasValidationErrors'); validationCount = arrayField(wire, 'ValidationErrors').length; arrayField(wire, 'Warnings'); }
    catch (error) { return uncertainMutation('recordPayment', requested, receipt, 'Xero accepted the payment but returned an unusable payment: ', error); }
    if (hasErrors || validationCount) return newMutationBranch('providerRejected', requested, receipt, failure('VALIDATION', 'recordPayment', `Xero rejected the payment${validationCount ? ` [${validationCount} validation error${validationCount === 1 ? '' : 's'}]` : ''}`));
    try {
      const payment = decodePayment(wire);
      if (payment.invoice.invoiceId && payment.invoice.invoiceId !== requested.invoiceId) throw new Error('payment applies to another invoice');
      const output: RecordPaymentOutput = { payment, invoiceId: requested.invoiceId, ...arrayField(wire, 'Warnings').length ? { warningCount: arrayField(wire, 'Warnings').length } : {} };
      return newMutationBranch('recorded', output, { ...receipt, providerObjectId: payment.paymentId });
    } catch (error) { return uncertainMutation('recordPayment', requested, receipt, 'Xero accepted the payment but returned an unusable payment: ', error); }
  }
}

interface Bounds { page: number; pageSize: number }
function pageBounds(page = 0, pageSize = 0): Bounds {
  const resolved = { page: page || 1, pageSize: pageSize || 25 };
  if (!Number.isSafeInteger(resolved.page) || resolved.page < 1) throw new Error('page must be 1 or greater');
  if (!Number.isSafeInteger(resolved.pageSize) || resolved.pageSize < 1 || resolved.pageSize > 100) throw new Error('pageSize must be between 1 and 100');
  return resolved;
}
function validateIDs(field: string, ids?: string[]): void {
  if ((ids?.length ?? 0) > 40) throw new Error(`${field} can list at most 40 IDs`);
  if (ids?.some(id => !uuidPattern.test(id))) throw new Error(`${field} must contain Xero IDs, which are UUIDs such as 4f2a3169-8454-4012-a642-05a88ef32982`);
}
function validateSingleLine(field: string, value: string, max: number): void {
  if ([...value].length > max) throw new Error(`${field} can be at most ${max} characters`);
  if ([...value].some(character => character.codePointAt(0)! < 32 || character.codePointAt(0) === 127)) throw new Error(`${field} must be one line without control characters`);
}
function validateInvoiceNumber(field: string, value: string): void {
  if (Buffer.byteLength(value) > 255) throw new Error(`${field} can be at most 255 characters`);
  if (/[^\x20-\x7e]|["\\]/.test(value)) throw new Error(`${field} must be printable ASCII without double quotes or backslashes`);
}
function invoiceIdentifier(input: GetInvoiceInput): string {
  const id = (input.invoiceId ?? '').trim(); const number = (input.invoiceNumber ?? '').trim();
  if (id && number) throw new Error('set either invoiceId or invoiceNumber, not both');
  if (id) { if (!uuidPattern.test(id)) throw new Error('invoiceId must be a Xero invoice UUID such as 243216c5-369e-4056-ac67-05388f86dc81'); return id.toLowerCase(); }
  if (number) { validateInvoiceNumber('invoiceNumber', number); return number; }
  throw new Error('invoiceId or invoiceNumber is required');
}
function validateInvoiceType(type: string): void { if (type !== 'ACCREC' && type !== 'ACCPAY') throw new Error('type must be ACCREC for a sales invoice or ACCPAY for a bill'); }
function validateEmail(value: string): void {
  if (!value) throw new Error('emailAddress is required');
  if (Buffer.byteLength(value) > 254) throw new Error('emailAddress is longer than 254 bytes');
  const address = emailAddresses.parseOneAddress({ input: value, rfc6532: true });
  const domain = address?.type === 'mailbox' ? address.domain : '';
  if (/[ "\\'()<>,;]/.test(value) || address?.type !== 'mailbox' || address.name !== null || address.address !== value || (domain?.startsWith('[') && isIP(domain.slice(1, -1)) === 0)) throw new Error('emailAddress must be one bare email address such as jane@example.com');
}
function failure(kind: Failure['kind'], operation: string, message: string): Failure { return { kind, provider: 'xero', operation, message }; }
function errorMessage(error: unknown): string { return error instanceof Error ? error.message : 'invalid value'; }
function defectQuery<T>(operation: string, zero: T, error: unknown): QueryAttempt<T> { return newQueryBranch('defect', zero, {}, failure('VALIDATION', operation, errorMessage(error))); }
function defectMutation<T>(operation: string, zero: T, error: unknown): MutationAttempt<T> { return newMutationBranch('defect', zero, {}, failure('VALIDATION', operation, errorMessage(error))); }
function invalidQuery<T>(operation: string, zero: T, receipt: ReceiptDraft, prefix: string, error: unknown): QueryAttempt<T> { return newQueryBranch('invalidResponse', zero, receipt, failure('PROTOCOL', operation, prefix + errorMessage(error))); }
function uncertainMutation<T>(operation: string, zero: T, receipt: ReceiptDraft, prefix: string, error: unknown): MutationAttempt<T> { return newMutationUncertain(zero, failure('PROTOCOL', operation, prefix + errorMessage(error)), receipt); }
function queryExchange<T>(result: XeroExchange, zero: T, receipt: ReceiptDraft, hasNotFound = false): QueryAttempt<T> {
  if (result.outcome === 'retry' || result.outcome === 'serverError') return newQueryRetry(result.failure!, result.retryAfterMs);
  const branch = result.outcome === 'notFound' ? (hasNotFound ? 'notFound' : 'providerRejected') : result.outcome === 'dailyLimit' ? 'dailyLimitReached' : result.outcome === 'invalid' ? 'invalidResponse' : result.outcome === 'defect' ? 'defect' : 'providerRejected';
  return newQueryBranch(branch, zero, receipt, result.failure);
}
function mutationExchange<T>(result: XeroExchange, zero: T, receipt: ReceiptDraft): MutationAttempt<T> {
  if (result.outcome === 'retry') return newMutationRetry(result.failure!, result.retryAfterMs);
  if (result.outcome === 'serverError' || result.outcome === 'invalid') return newMutationUncertain(zero, result.failure!, receipt);
  return newMutationBranch(result.outcome === 'dailyLimit' ? 'dailyLimitReached' : result.outcome === 'defect' ? 'defect' : 'providerRejected', zero, receipt, result.failure);
}

function object(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('response is not a JSON object'); return value as Record<string, unknown>; }
function stringField(record: Record<string, unknown>, key: string): string { const value = record[key]; if (value === undefined || value === null) return ''; if (typeof value !== 'string') throw new Error(`invalid ${key}`); return value; }
function boolField(record: Record<string, unknown>, key: string): boolean { const value = record[key]; if (value === undefined || value === null) return false; if (typeof value === 'boolean') return value; if (typeof value === 'string' && value.toLowerCase() === 'true') return true; if (typeof value === 'string' && value.toLowerCase() === 'false') return false; throw new Error(`invalid ${key}`); }
function arrayField(record: Record<string, unknown>, key: string): unknown[] { const value = record[key]; if (value === undefined || value === null) return []; if (!Array.isArray(value)) throw new Error(`invalid ${key}`); return value; }
function integerField(record: Record<string, unknown>, key: string): number { const value = record[key]; if (value === undefined || value === null) return 0; const raw = rawNumber(value); if (raw === undefined || !/^-?(0|[1-9][0-9]*)$/.test(raw)) throw new Error(`invalid ${key}`); const decoded = Number(raw); if (!Number.isSafeInteger(decoded)) throw new Error(`invalid ${key}`); return decoded; }
function decimalField(record: Record<string, unknown>, key: string): string { const value = record[key]; if (value === undefined || value === null) return ''; const decoded = rawNumber(value) ?? value; if (typeof decoded !== 'number' && typeof decoded !== 'string') throw new Error(`invalid ${key}`); const text = String(decoded).trim(); if (!text) return ''; if (!decimalPattern.test(text)) throw new Error(`invalid ${key}`); return text; }
function optionalString(record: Record<string, unknown>, key: string, output: string): Record<string, string> { const value = stringField(record, key); return value ? { [output]: value } : {}; }
function optionalDecimal(record: Record<string, unknown>, key: string, output: string): Record<string, string> { const value = decimalField(record, key); return value ? { [output]: value } : {}; }
function optionalBool(record: Record<string, unknown>, key: string, output: string): Record<string, boolean> { const value = boolField(record, key); return value ? { [output]: value } : {}; }
function rawNumber(value: unknown): string | undefined { return value && typeof value === 'object' && '$rawNumber' in value && typeof value.$rawNumber === 'string' ? value.$rawNumber : undefined; }
function parseBody(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString(), ((_key: string, value: unknown, context?: { source?: string }) => typeof value === 'number' ? { $rawNumber: context?.source ?? String(value) } : value) as (key: string, value: unknown) => unknown); } catch { throw new Error('response is not a JSON object'); } }
function singleResource(body: Uint8Array, collection: string): Record<string, unknown> {
  const envelope = object(parseBody(body)); const list = envelope[collection];
  if (!Array.isArray(list)) throw new Error(`response has no ${collection} list`);
  if (list.length !== 1) throw new Error(`response has ${list.length} ${collection} instead of one`);
  return object(list[0]);
}
function dotNetDate(value: string): Date {
  const match = /^\/Date\((-?[0-9]{1,15})([+-][0-9]{4})?\)\/$/.exec(value);
  if (!match) throw new Error('value is not a Xero date');
  const instant = new Date(Number(match[1]));
  if (Number.isNaN(instant.getTime())) throw new Error('value is not a Xero date');
  return instant;
}
function timestamp(value: string): string { return value ? dotNetDate(value).toISOString().replace(/(\.\d*?)0+Z$/, '$1Z').replace(/\.Z$/, 'Z') : zeroTime; }
function validCalendarDate(value: string): boolean {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  if (!match) return false;
  const date = new Date(`${value}T00:00:00Z`);
  return !Number.isNaN(date.getTime()) && date.toISOString().slice(0, 10) === value;
}
function calendarDate(dateString: string, dotNet: string): string {
  if (dateString) { const date = dateString.slice(0, 10); if (dateString.length < 10 || !validCalendarDate(date)) throw new Error('value is not a Xero date'); return date; }
  return dotNet ? dotNetDate(dotNet).toISOString().slice(0, 10) : '';
}
function decodeContact(wire: Record<string, unknown>): Contact {
  const id = stringField(wire, 'ContactID'); if (!uuidPattern.test(id)) throw new Error('contact has no ContactID');
  let updatedAt: string; try { updatedAt = timestamp(stringField(wire, 'UpdatedDateUTC')); } catch { throw new Error('contact has an invalid UpdatedDateUTC'); }
  return { contactId: id.toLowerCase(), name: stringField(wire, 'Name'), status: stringField(wire, 'ContactStatus'), updatedAt,
    ...optionalString(wire, 'FirstName', 'firstName'), ...optionalString(wire, 'LastName', 'lastName'), ...optionalString(wire, 'EmailAddress', 'emailAddress'),
    ...optionalString(wire, 'ContactNumber', 'contactNumber'), ...optionalString(wire, 'AccountNumber', 'accountNumber'), ...optionalString(wire, 'DefaultCurrency', 'defaultCurrency'),
    ...optionalBool(wire, 'IsCustomer', 'isCustomer'), ...optionalBool(wire, 'IsSupplier', 'isSupplier') };
}
function decodePage<T>(body: Uint8Array, key: string, bounds: Bounds, decode: (wire: Record<string, unknown>) => T): { values: T[]; page: number; pageSize: number; pageCount?: number; itemCount?: number; hasMorePages: boolean } {
  const envelope = object(parseBody(body));
  if (!Array.isArray(envelope[key])) throw new Error(`response has no ${key} list`);
  const values = envelope[key].map((item: unknown) => decode(object(item)));
  const pagination = envelope.pagination == null ? undefined : object(envelope.pagination);
  const pageCount = pagination ? integerField(pagination, 'pageCount') : 0;
  const itemCount = pagination ? integerField(pagination, 'itemCount') : 0;
  if (pagination) { integerField(pagination, 'page'); integerField(pagination, 'pageSize'); }
  return { values, page: bounds.page, pageSize: bounds.pageSize, ...(pageCount ? { pageCount } : {}), ...(itemCount ? { itemCount } : {}), hasMorePages: pageCount > 0 ? bounds.page < pageCount : values.length >= bounds.pageSize };
}
function decodeContactPage(body: Uint8Array, bounds: Bounds): ContactPage { const page = decodePage(body, 'Contacts', bounds, decodeContact); return { contacts: page.values, page: page.page, pageSize: page.pageSize, ...(page.pageCount ? { pageCount: page.pageCount } : {}), ...(page.itemCount ? { itemCount: page.itemCount } : {}), hasMorePages: page.hasMorePages }; }
function decodeInvoicePage(body: Uint8Array, bounds: Bounds): InvoicePage { const page = decodePage(body, 'Invoices', bounds, decodeInvoice); return { invoices: page.values, page: page.page, pageSize: page.pageSize, ...(page.pageCount ? { pageCount: page.pageCount } : {}), ...(page.itemCount ? { itemCount: page.itemCount } : {}), hasMorePages: page.hasMorePages }; }
function decodeInvoice(wire: Record<string, unknown>): Invoice {
  boolField(wire, 'HasErrors'); arrayField(wire, 'ValidationErrors'); arrayField(wire, 'Warnings');
  const id = stringField(wire, 'InvoiceID'); if (!uuidPattern.test(id)) throw new Error('invoice has no InvoiceID');
  const type = stringField(wire, 'Type'); const status = stringField(wire, 'Status'); if (!type || !status) throw new Error('invoice has no Type or Status');
  let date: string, dueDate: string, fullyPaidOnDate: string, updatedAt: string;
  try { date = calendarDate(stringField(wire, 'DateString'), stringField(wire, 'Date')); } catch { throw new Error('invoice has an invalid Date'); }
  try { dueDate = calendarDate(stringField(wire, 'DueDateString'), stringField(wire, 'DueDate')); } catch { throw new Error('invoice has an invalid DueDate'); }
  try { fullyPaidOnDate = calendarDate('', stringField(wire, 'FullyPaidOnDate')); } catch { throw new Error('invoice has an invalid FullyPaidOnDate'); }
  try { updatedAt = timestamp(stringField(wire, 'UpdatedDateUTC')); } catch { throw new Error('invoice has an invalid UpdatedDateUTC'); }
  const contact = object(wire.Contact ?? {});
  const lineItems = arrayField(wire, 'LineItems').map(item => { const line = object(item); return { ...optionalString(line, 'LineItemID', 'lineItemId'), ...optionalString(line, 'Description', 'description'), ...optionalDecimal(line, 'Quantity', 'quantity'), ...optionalDecimal(line, 'UnitAmount', 'unitAmount'), ...optionalString(line, 'ItemCode', 'itemCode'), ...optionalString(line, 'AccountCode', 'accountCode'), ...optionalString(line, 'TaxType', 'taxType'), ...optionalDecimal(line, 'TaxAmount', 'taxAmount'), ...optionalDecimal(line, 'LineAmount', 'lineAmount'), ...optionalDecimal(line, 'DiscountRate', 'discountRate') }; });
  const payments = arrayField(wire, 'Payments').map((item, index) => { const payment = object(item); const paymentID = stringField(payment, 'PaymentID'); if (!uuidPattern.test(paymentID)) throw new Error(`invoice payment ${index} is invalid`); let paymentDate: string; try { paymentDate = calendarDate(stringField(payment, 'DateString'), stringField(payment, 'Date')); } catch { throw new Error(`invoice payment ${index} is invalid`); } return { paymentId: paymentID.toLowerCase(), ...paymentDate ? { date: paymentDate } : {}, ...optionalDecimal(payment, 'Amount', 'amount') }; });
  return { invoiceId: id.toLowerCase(), type, status, contact: { contactId: stringField(contact, 'ContactID').toLowerCase(), ...optionalString(contact, 'Name', 'name') }, updatedAt,
    ...optionalString(wire, 'InvoiceNumber', 'invoiceNumber'), ...date ? { date } : {}, ...dueDate ? { dueDate } : {},
    ...optionalString(wire, 'Reference', 'reference'), ...optionalString(wire, 'CurrencyCode', 'currencyCode'), ...optionalDecimal(wire, 'CurrencyRate', 'currencyRate'), ...optionalString(wire, 'LineAmountTypes', 'lineAmountTypes'),
    ...lineItems.length ? { lineItems } : {}, ...optionalDecimal(wire, 'SubTotal', 'subTotal'), ...optionalDecimal(wire, 'TotalTax', 'totalTax'), ...optionalDecimal(wire, 'Total', 'total'),
    ...optionalDecimal(wire, 'AmountDue', 'amountDue'), ...optionalDecimal(wire, 'AmountPaid', 'amountPaid'), ...optionalDecimal(wire, 'AmountCredited', 'amountCredited'), ...payments.length ? { payments } : {},
    ...fullyPaidOnDate ? { fullyPaidOnDate } : {}, ...optionalBool(wire, 'SentToContact', 'isSentToContact') };
}
function decodePayment(wire: Record<string, unknown>): Payment {
  const id = stringField(wire, 'PaymentID'); if (!uuidPattern.test(id)) throw new Error('payment has no PaymentID');
  if (!decimalField(wire, 'Amount')) throw new Error('payment has no Amount');
  let date: string, updatedAt: string;
  try { date = calendarDate(stringField(wire, 'DateString'), stringField(wire, 'Date')); } catch { throw new Error('payment has an invalid Date'); }
  try { updatedAt = timestamp(stringField(wire, 'UpdatedDateUTC')); } catch { throw new Error('payment has an invalid UpdatedDateUTC'); }
  const invoice = object(wire.Invoice ?? {}), account = object(wire.Account ?? {});
  return { paymentId: id.toLowerCase(), amount: decimalField(wire, 'Amount'), status: stringField(wire, 'Status'), invoice: { invoiceId: stringField(invoice, 'InvoiceID').toLowerCase(), ...optionalString(invoice, 'InvoiceNumber', 'invoiceNumber'), ...optionalString(invoice, 'Type', 'type'), ...optionalString(invoice, 'Status', 'status'), ...optionalDecimal(invoice, 'AmountDue', 'amountDue'), ...optionalDecimal(invoice, 'AmountPaid', 'amountPaid'), ...optionalString(invoice, 'CurrencyCode', 'currencyCode') }, account: { ...stringField(account, 'AccountID') ? { accountId: stringField(account, 'AccountID').toLowerCase() } : {}, ...optionalString(account, 'Code', 'code') }, updatedAt,
    ...date ? { date } : {}, ...optionalDecimal(wire, 'BankAmount', 'bankAmount'), ...optionalDecimal(wire, 'CurrencyRate', 'currencyRate'), ...optionalString(wire, 'Reference', 'reference'), ...optionalString(wire, 'PaymentType', 'paymentType'), ...optionalBool(wire, 'IsReconciled', 'isReconciled') };
}
function validateInputDecimal(field: string, value: string, fraction: number, integer: number, negative: boolean, zero: boolean): void {
  if (!decimalPattern.test(value)) throw new Error(`${field} must be an exact decimal number such as 1800.00, without an exponent or leading zeros`);
  const [whole, decimals = ''] = value.replace(/^-/, '').split('.');
  if (decimals.length > fraction) throw new Error(`${field} can have at most ${fraction} decimal places`);
  if (whole!.length > integer) throw new Error(`${field} can have at most ${integer} digits before the decimal point`);
  const isZero = !/[1-9]/.test(value);
  if (value.startsWith('-') && !isZero && !negative) throw new Error(`${field} cannot be negative`);
  if (isZero && !zero) throw new Error(`${field} must be greater than zero`);
}
function putString(target: Record<string, unknown>, key: string, value?: string): void { if (value?.trim()) target[key] = value.trim(); }
function putDecimal(target: Record<string, unknown>, key: string, value: string | undefined, field: string, fraction: number, integer: number, negative: boolean, zero: boolean): void { if (value) { validateInputDecimal(field, value, fraction, integer, negative, zero); target[key] = { $decimal: value }; } }
function jsonWithDecimals(value: unknown): Uint8Array { return Buffer.from(JSON.stringify(value).replace(/\{"\$decimal":"(-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?)"\}/g, '$1')); }
function createInvoicePayload(input: CreateInvoiceInput): Uint8Array {
  validateInvoiceType(input.type); const contact = input.contactId.trim(); if (!uuidPattern.test(contact)) throw new Error('contactId must be a Xero contact UUID such as eaa28f49-6028-4b6e-bb12-d8f6278073fc');
  const payload: Record<string, unknown> = { Type: input.type, Contact: { ContactID: contact.toLowerCase() }, LineItems: [] };
  for (const [field, value] of [['date', input.date], ['dueDate', input.dueDate]] as const) if (value?.trim() && !validCalendarDate(value.trim())) throw new Error(`${field} must be a calendar date written YYYY-MM-DD, such as 2026-10-01`);
  putString(payload, 'Date', input.date); putString(payload, 'DueDate', input.dueDate);
  if (input.lineAmountTypes && !['Exclusive', 'Inclusive', 'NoTax'].includes(input.lineAmountTypes)) throw new Error('lineAmountTypes must be Exclusive, Inclusive, or NoTax');
  putString(payload, 'LineAmountTypes', input.lineAmountTypes);
  if (input.status && !['DRAFT', 'SUBMITTED', 'AUTHORISED'].includes(input.status)) throw new Error('status of a new invoice must be DRAFT, SUBMITTED, or AUTHORISED');
  putString(payload, 'Status', input.status);
  if (input.currencyCode?.trim() && !/^[A-Z]{3}$/.test(input.currencyCode.trim())) throw new Error('currencyCode must be a three-letter ISO 4217 code such as USD');
  putString(payload, 'CurrencyCode', input.currencyCode);
  validateInvoiceNumber('invoiceNumber', input.invoiceNumber?.trim() ?? ''); putString(payload, 'InvoiceNumber', input.invoiceNumber);
  validateSingleLine('reference', input.reference?.trim() ?? '', 255); putString(payload, 'Reference', input.reference);
  if (input.brandingThemeId?.trim() && !uuidPattern.test(input.brandingThemeId.trim())) throw new Error('brandingThemeId must be a Xero branding theme UUID');
  if (input.brandingThemeId?.trim()) payload.BrandingThemeID = input.brandingThemeId.trim().toLowerCase();
  if (!input.lineItems?.length || input.lineItems.length > 100) throw new Error('lineItems must hold 1 to 100 lines');
  payload.LineItems = input.lineItems.map((line, index) => {
    const field = `lineItems[${index}]`; const description = line.description.trim();
    if (!description) throw new Error(`${field}.description is required`);
    if ([...description].length > 4000) throw new Error(`${field}.description must be valid UTF-8 of at most 4000 characters`);
    const result: Record<string, unknown> = { Description: description };
    for (const [name, value] of [['accountCode', line.accountCode], ['itemCode', line.itemCode], ['taxType', line.taxType]] as const) {
      if (value?.trim() && !accountingCodePattern.test(value.trim())) throw new Error(`${field}.${name} must be a Xero code of at most 50 letters, digits, spaces, or . _ / -`);
      putString(result, name === 'accountCode' ? 'AccountCode' : name === 'itemCode' ? 'ItemCode' : 'TaxType', value);
    }
    putDecimal(result, 'Quantity', line.quantity, `${field}.quantity`, 4, 12, true, true);
    putDecimal(result, 'UnitAmount', line.unitAmount, `${field}.unitAmount`, 4, 12, true, true);
    putDecimal(result, 'DiscountRate', line.discountRate, `${field}.discountRate`, 4, 3, false, true);
    if (line.discountRate) { if (input.type !== 'ACCREC') throw new Error(`${field}.discountRate is supported only on ACCREC invoices`); if (Number(line.discountRate) > 100) throw new Error(`${field}.discountRate must be a percentage from 0 to 100`); }
    return result;
  });
  return jsonWithDecimals(payload);
}
function recordPaymentPayload(input: RecordPaymentInput): Uint8Array {
  const invoiceID = input.invoiceId.trim().toLowerCase(); if (!uuidPattern.test(invoiceID)) throw new Error('invoiceId must be a Xero invoice UUID such as 96df0dff-43ec-4899-a7d9-e9d63ef12b19');
  const accountID = input.accountId?.trim().toLowerCase() ?? '', code = input.accountCode?.trim() ?? '';
  if (Boolean(accountID) === Boolean(code)) throw new Error('set exactly one of accountId and accountCode');
  if (accountID && !uuidPattern.test(accountID)) throw new Error('accountId must be a Xero account UUID');
  if (code && !accountingCodePattern.test(code)) throw new Error('accountCode must be a Xero account code such as 090');
  const date = input.date.trim(); if (!validCalendarDate(date)) throw new Error('date must be a calendar date written YYYY-MM-DD, such as 2026-10-01');
  const payload: Record<string, unknown> = { Invoice: { InvoiceID: invoiceID }, Account: { ...(accountID ? { AccountID: accountID } : {}), ...(code ? { Code: code } : {}) }, Date: date };
  validateInputDecimal('amount', input.amount, 2, 12, false, false);
  putDecimal(payload, 'Amount', input.amount, 'amount', 2, 12, false, false);
  putDecimal(payload, 'BankAmount', input.bankAmount, 'bankAmount', 2, 12, false, false);
  validateSingleLine('reference', input.reference?.trim() ?? '', 255); putString(payload, 'Reference', input.reference);
  if (input.isReconciled) payload.IsReconciled = true;
  return jsonWithDecimals(payload);
}
