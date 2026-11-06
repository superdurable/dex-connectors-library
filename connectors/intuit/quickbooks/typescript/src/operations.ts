// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { isIP } from 'node:net';
import type { QuickBooksClient, QuickBooksExchange } from './client.js';
import { CreateCustomerDefinition, CreateInvoiceDefinition, FindCustomerDefinition, GetInvoiceDefinition, ListInvoicesDefinition, RecordPaymentDefinition, SendInvoiceDefinition } from './generated/definitions.js';
import type { CreateCustomerInput, CreateCustomerOutput, CreateInvoiceInput, CreateInvoiceOutput, Customer, FindCustomerInput, FindCustomerOutput, GetInvoiceInput, Invoice, InvoiceLine, InvoiceLineInput, InvoicePage, ListInvoicesInput, Payment, RecordPaymentInput, SendInvoiceInput } from './generated/models.js';

const entityID = /^[0-9]{1,20}$/;
const currencyCode = /^[A-Z]{3}$/;
const zeroCustomer: Customer = { customerId: '', displayName: '', isActive: false };
const zeroInvoice: Invoice = { invoiceId: '', customerId: '', lines: [], totalAmount: '', balance: '' };
const zeroPayment: Payment = { paymentId: '', customerId: '', totalAmount: '' };

function object(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('response is not a JSON object'); return value as Record<string, unknown>; }
function string(value: unknown): string { if (value === undefined || value === null) return ''; if (typeof value !== 'string') throw new TypeError('provider field is not a string'); return value; }
function array(value: unknown): unknown[] { if (value == null) return []; if (!Array.isArray(value)) throw new TypeError('provider field is not a list'); return value; }
function reference(value: unknown): string { return value == null ? '' : string(object(value).value); }
function optional<T>(condition: boolean, key: string, value: T): Record<string, T> { return condition ? { [key]: value } : {}; }
function parse(body: Uint8Array): Record<string, unknown> {
  const parseWithSource = JSON.parse as (text: string, reviver: (key: string, value: unknown, context: { source?: string }) => unknown) => unknown;
  return object(parseWithSource(Buffer.from(body).toString('utf8'), (_key, value, context) => typeof value === 'number' ? context.source ?? String(value) : value));
}
function entity(body: Uint8Array, name: string): Record<string, unknown> { const response = parse(body); if (!(name in response)) throw new TypeError(`response has no ${name}`); return object(response[name]); }
function entities(body: Uint8Array, name: string): Record<string, unknown>[] { const response = parse(body); const query = object(response.QueryResponse); if (!(name in query)) return []; return array(query[name]).map(object); }
function validID(name: string, value: string): void { if (!entityID.test(value)) throw new TypeError(`${name} must be a QuickBooks Id, a decimal string such as 145`); }
function validText(name: string, value: string, maximum: number, multiline = false): void { if ([...value].length > maximum) throw new TypeError(`${name} can be at most ${maximum} characters`); if ([...value].some(char => { const code = char.codePointAt(0)!; return (code < 32 || code === 127) && !(multiline && '\n\r\t'.includes(char)); })) throw new TypeError(`${name} must not contain control characters`); }
function validQuery(name: string, value: string, maximum: number): void { validText(name, value, maximum); if (value.includes('\\')) throw new TypeError(`${name} cannot contain a backslash`); }
function quote(value: string): string { return `'${value.replaceAll("'", "\\'")}'`; }
function validDate(name: string, value: string): void {
  const parts = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  const year = Number(parts?.[1]), month = Number(parts?.[2]), day = Number(parts?.[3]);
  const boundary = new Date(0); boundary.setUTCFullYear(year, month, 0);
  const lastDay = boundary.getUTCDate();
  if (!parts || month < 1 || month > 12 || day < 1 || day > lastDay) throw new TypeError(`${name} must be a calendar date written YYYY-MM-DD, such as 2026-10-01`);
}
function validCurrency(name: string, value: string): void { if (value && !currencyCode.test(value)) throw new TypeError(`${name} must be a three-letter ISO 4217 code such as USD`); }
function validEmail(name: string, value: string): void {
  const invalid = (): never => { throw new TypeError(`${name} must be one bare email address such as jane@example.com`); };
  if (!value || Buffer.byteLength(value, 'utf8') > 254 || /[ "\\'()<>,;]/.test(value)) invalid();
  const at = value.lastIndexOf('@');
  if (at < 1 || at === value.length - 1 || value.indexOf('@') !== at) invalid();
  const local = value.slice(0, at), domain = value.slice(at + 1);
  const localAtom = /^[\p{L}\p{N}\p{M}!#$%&*+\-/=?^_`{|}~]+$/u;
  if (local.split('.').some(part => !localAtom.test(part))) invalid();
  if (domain.startsWith('[') && domain.endsWith(']')) {
    const contents = domain.slice(1, -1);
    if (!isIP(contents)) invalid();
  } else {
    const domainAtom = /^[\p{L}\p{N}\p{M}_-]+$/u;
    if (domain.split('.').some(part => !domainAtom.test(part))) invalid();
  }
}
function metadata(wire: Record<string, unknown>): Record<string, string> { const raw = wire.MetaData; if (raw == null) return {}; const meta = object(raw); const result: Record<string, string> = {}; for (const [wireName, outputName] of [['CreateTime', 'createdAt'], ['LastUpdatedTime', 'updatedAt']] as const) { const value = string(meta[wireName]); if (!value) continue; try { result[outputName] = parseRFC3339(value); } catch { throw new TypeError(`invalid MetaData.${wireName}`); } } return result; }
function decimal(value: unknown, required = false): string {
  if (value === undefined || value === null) { if (required) throw new TypeError('missing decimal'); return ''; }
  if (typeof value !== 'number' && typeof value !== 'string') throw new TypeError('invalid decimal');
  const text = String(value).trim();
  if (!/^-?(?:0|[1-9][0-9]{0,17})(?:\.[0-9]{1,18})?(?:[eE][+-]?[0-9]{1,2})?$/.test(text)) throw new TypeError('invalid decimal');
  const match = /^(-?)(\d+)(?:\.(\d+))?(?:[eE]([+-]?\d+))?$/.exec(text)!;
  let digits = match[2]! + (match[3] ?? '');
  let point = match[2]!.length + Number(match[4] ?? 0);
  if (point <= 0) { digits = '0'.repeat(1 - point) + digits; point = 1; }
  else if (point > digits.length) digits += '0'.repeat(point - digits.length);
  const whole = digits.slice(0, point).replace(/^0+/, '') || '0';
  const fraction = digits.slice(point);
  const result = `${match[1]}${whole}${fraction ? `.${fraction}` : ''}`;
  const canonical = /[1-9]/.test(result) ? result : result.replace(/^-/, '');
  if (!/^-?(?:0|[1-9][0-9]{0,17})(?:\.[0-9]{1,10})?$/.test(canonical)) throw new TypeError('invalid decimal');
  return canonical;
}
function parseRFC3339(value: string, includeFraction = true): string {
  const parts = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
  if (!parts) throw new TypeError('invalid RFC 3339 timestamp');
  const [year, month, day, hour, minute, second] = parts.slice(1, 7).map(Number);
  validDate('timestamp', `${parts[1]}-${parts[2]}-${parts[3]}`);
  if (hour! > 23 || minute! > 59 || second! > 59) throw new TypeError('invalid RFC 3339 timestamp');
  const zone = parts[8]!;
  const offsetHours = zone === 'Z' ? 0 : Number(zone.slice(1, 3));
  const offsetMinutes = zone === 'Z' ? 0 : Number(zone.slice(4, 6));
  if (offsetHours > 24 || offsetMinutes > 59) throw new TypeError('invalid RFC 3339 timestamp');
  const date = new Date(0);
  date.setUTCFullYear(year!, month! - 1, day!);
  date.setUTCHours(hour!, minute!, second!, 0);
  const sign = zone.startsWith('-') ? -1 : 1;
  const utc = new Date(date.getTime() - sign * (offsetHours * 60 + offsetMinutes) * 60_000);
  const seconds = utc.toISOString().slice(0, 19);
  const fraction = includeFraction ? (parts[7] ?? '').slice(0, 9).replace(/0+$/, '') : '';
  return `${seconds}${fraction ? `.${fraction}` : ''}Z`;
}
function omitEmptyOptionalFields<T>(value: T): T {
  return Object.fromEntries(Object.entries(value as Record<string, unknown>).filter(([name, item]) =>
    item !== '' && (!Array.isArray(item) || item.length > 0 || name === 'lines'))) as T;
}
function decodeCustomer(wire: Record<string, unknown>): Customer {
  const id = string(wire.Id), displayName = string(wire.DisplayName);
  validID('customer Id', id); if (!displayName.trim()) throw new TypeError('customer has no DisplayName');
  const email = wire.PrimaryEmailAddr == null ? '' : string(object(wire.PrimaryEmailAddr).Address).trim();
  const phone = wire.PrimaryPhone == null ? '' : string(object(wire.PrimaryPhone).FreeFormNumber);
  if (wire.Active != null && typeof wire.Active !== 'boolean') throw new TypeError('invalid Active');
  return omitEmptyOptionalFields({ customerId: id, displayName, isActive: wire.Active == null || wire.Active === true,
    ...optional(!!wire.SyncToken, 'syncToken', string(wire.SyncToken)), ...optional(!!wire.GivenName, 'givenName', string(wire.GivenName)), ...optional(!!wire.FamilyName, 'familyName', string(wire.FamilyName)), ...optional(!!wire.CompanyName, 'companyName', string(wire.CompanyName)), ...optional(!!email, 'emailAddress', email), ...optional(!!phone, 'phone', phone), ...optional(wire.Balance != null, 'balance', decimal(wire.Balance)), ...optional(wire.CurrencyRef != null, 'currencyCode', reference(wire.CurrencyRef)), ...metadata(wire) });
}
function decodeInvoiceLine(wire: Record<string, unknown>): InvoiceLine {
  const detailType = string(wire.DetailType); if (!detailType) throw new TypeError('line has no DetailType');
  const detail = wire.SalesItemLineDetail == null ? null : object(wire.SalesItemLineDetail);
  return omitEmptyOptionalFields({ detailType, ...optional(!!wire.Id, 'lineId', string(wire.Id)), ...optional(!!wire.Description, 'description', string(wire.Description)), ...optional(wire.Amount != null, 'amount', decimal(wire.Amount)),
    ...(detail ? { ...optional(detail.ItemRef != null, 'itemId', reference(detail.ItemRef)), ...optional(detail.ItemRef != null && !!object(detail.ItemRef).name, 'itemName', string(detail.ItemRef == null ? '' : object(detail.ItemRef).name)), ...optional(detail.Qty != null, 'quantity', decimal(detail.Qty)), ...optional(detail.UnitPrice != null, 'unitPrice', decimal(detail.UnitPrice)), ...optional(detail.TaxCodeRef != null, 'taxCodeId', reference(detail.TaxCodeRef)), ...optional(!!detail.ServiceDate, 'serviceDate', string(detail.ServiceDate)) } : {}) });
}
function decodeInvoice(wire: Record<string, unknown>): Invoice {
  const id = string(wire.Id), customer = reference(wire.CustomerRef); validID('invoice Id', id); validID('CustomerRef', customer);
  const total = decimal(wire.TotalAmt, true), balance = decimal(wire.Balance, true);
  const lines = array(wire.Line).map(object).map(decodeInvoiceLine);
  const linked = array(wire.LinkedTxn).map(object).filter(item => item.TxnType === 'Payment' && entityID.test(string(item.TxnId))).map(item => string(item.TxnId));
  const delivered = wire.DeliveryInfo == null ? '' : string(object(wire.DeliveryInfo).DeliveryTime);
  if (delivered) try { parseRFC3339(delivered); } catch { throw new TypeError('invoice has an invalid DeliveryInfo.DeliveryTime'); }
  for (const key of ['TxnDate', 'DueDate']) if (wire[key]) validDate(key, string(wire[key]));
  return omitEmptyOptionalFields({ invoiceId: id, customerId: customer, lines, totalAmount: total, balance,
    ...optional(!!wire.SyncToken, 'syncToken', string(wire.SyncToken)), ...optional(!!wire.DocNumber, 'docNumber', string(wire.DocNumber)), ...optional(wire.CustomerRef != null && !!object(wire.CustomerRef).name, 'customerName', wire.CustomerRef == null ? '' : string(object(wire.CustomerRef).name)), ...optional(!!wire.TxnDate, 'txnDate', string(wire.TxnDate)), ...optional(!!wire.DueDate, 'dueDate', string(wire.DueDate)), ...optional(wire.CurrencyRef != null, 'currencyCode', reference(wire.CurrencyRef)), ...optional(wire.ExchangeRate != null, 'exchangeRate', decimal(wire.ExchangeRate)), ...optional(wire.TxnTaxDetail != null, 'totalTax', decimal(wire.TxnTaxDetail == null ? undefined : object(wire.TxnTaxDetail).TotalTax)), ...optional(!!wire.EmailStatus, 'emailStatus', string(wire.EmailStatus)), ...optional(wire.BillEmail != null, 'billEmail', wire.BillEmail == null ? '' : string(object(wire.BillEmail).Address).trim()), ...optional(!!delivered, 'deliveredAt', delivered ? parseRFC3339(delivered) : ''), ...optional(wire.CustomerMemo != null, 'customerMemo', wire.CustomerMemo == null ? '' : string(object(wire.CustomerMemo).value)), ...optional(!!wire.PrivateNote, 'privateNote', string(wire.PrivateNote)), ...optional(linked.length > 0, 'linkedPaymentIds', linked), ...metadata(wire) });
}
function decodePayment(wire: Record<string, unknown>): Payment {
  const id = string(wire.Id), customer = reference(wire.CustomerRef); validID('payment Id', id); validID('CustomerRef', customer);
  const applications: { invoiceId: string; amount: string }[] = [];
  if (wire.TxnDate) validDate('TxnDate', string(wire.TxnDate));
  for (const line of array(wire.Line).map(object)) for (const linked of array(line.LinkedTxn).map(object)) if (linked.TxnType === 'Invoice') { const invoiceID = string(linked.TxnId); validID('payment TxnId', invoiceID); applications.push({ invoiceId: invoiceID, amount: decimal(line.Amount, true) }); }
  return omitEmptyOptionalFields({ paymentId: id, customerId: customer, totalAmount: decimal(wire.TotalAmt, true), ...optional(!!wire.SyncToken, 'syncToken', string(wire.SyncToken)), ...optional(!!wire.TxnDate, 'txnDate', string(wire.TxnDate)), ...optional(wire.UnappliedAmt != null, 'unappliedAmount', decimal(wire.UnappliedAmt)), ...optional(wire.CurrencyRef != null, 'currencyCode', reference(wire.CurrencyRef)), ...optional(!!wire.PaymentRefNum, 'paymentReferenceNumber', string(wire.PaymentRefNum)), ...optional(wire.DepositToAccountRef != null, 'depositAccountId', reference(wire.DepositToAccountRef)), ...optional(applications.length > 0, 'appliedInvoices', applications), ...optional(!!wire.PrivateNote, 'privateNote', string(wire.PrivateNote)), ...metadata(wire) });
}
function queryOutcome<T>(result: QuickBooksExchange, output: T, notFound = false): QueryAttempt<T> | undefined {
  if (result.disposition === 'succeeded') return undefined;
  if (result.disposition === 'retry' || result.disposition === 'interrupted') return newQueryRetry(result.failure!, result.retryAfterMs);
  const branch = result.disposition === 'defect' ? 'defect' : result.disposition === 'invalid' ? 'invalidResponse' : result.disposition === 'notFound' && notFound ? 'notFound' : 'providerRejected';
  return newQueryBranch(branch, output, result.receipt, result.failure);
}
function mutationOutcome<T>(result: QuickBooksExchange, output: T, nameConflict = false): MutationAttempt<T> | undefined {
  if (result.disposition === 'succeeded' || result.disposition === 'invalid' || result.disposition === 'interrupted') return undefined;
  if (result.disposition === 'retry') return newMutationRetry(result.failure!, result.retryAfterMs);
  return newMutationBranch(result.disposition === 'defect' ? 'defect' : result.disposition === 'nameConflict' && nameConflict ? 'nameConflict' : 'providerRejected', output, result.receipt, result.failure);
}
function defect(client: QuickBooksClient, operation: string, error: unknown): Failure { return client.failure('VALIDATION', operation, error instanceof Error ? error.message : 'invalid input'); }

/** Resolve one customer by exact email or display name. */
export class FindCustomerOperation implements Query<FindCustomerInput, FindCustomerOutput> {
  /** Bind one client. */ constructor(private readonly client: QuickBooksClient) {}
  /** Return the manifest definition. */ definition() { return FindCustomerDefinition; }
  /** Execute one bounded QuickBooks customer query. */
  async invoke(call: Call, input: FindCustomerInput): Promise<QueryAttempt<FindCustomerOutput>> {
    const requested: FindCustomerOutput = { ...optional(!!input.emailAddress?.trim(), 'emailAddress', input.emailAddress?.trim() ?? ''), ...optional(!!input.displayName?.trim(), 'displayName', input.displayName?.trim() ?? '') };
    let statement: string;
    try {
      const email = input.emailAddress?.trim() ?? '', displayName = input.displayName?.trim() ?? '';
      if (email && displayName) throw new TypeError('give either emailAddress or displayName, not both');
      if (email) { validEmail('emailAddress', email); statement = `PrimaryEmailAddr = ${quote(email)}`; }
      else if (displayName) { validQuery('displayName', displayName, 500); statement = `DisplayName = ${quote(displayName)}`; }
      else throw new TypeError('emailAddress or displayName is required');
      if (input.includesInactive) statement += ' AND Active IN (true, false)';
      statement = `select * from Customer where ${statement} MAXRESULTS 10`;
    } catch (error) { return newQueryBranch('defect', requested, {}, defect(this.client, 'findCustomer', error)); }
    const result = await this.client.exchange(call, 'findCustomer', { method: 'GET', path: '/query', query: { query: statement } });
    const outcome = queryOutcome(result, requested); if (outcome) return outcome;
    try {
      const matches = entities(result.body!, 'Customer').map(decodeCustomer).filter(customer => (requested.emailAddress ? customer.emailAddress?.toLowerCase() === requested.emailAddress.toLowerCase() : customer.displayName.toLowerCase() === requested.displayName?.toLowerCase()) && (input.includesInactive || customer.isActive));
      if (matches.length > 10) throw new TypeError('response has more customers than requested');
      if (!matches.length) return newQueryBranch('notFound', requested, result.receipt, this.client.failure('NOT_FOUND', 'findCustomer', 'no QuickBooks customer matches'));
      if (matches.length > 1) return newQueryBranch('ambiguous', { ...requested, matches }, result.receipt, this.client.failure('CONFLICT', 'findCustomer', 'several QuickBooks customers match'));
      result.receipt.providerObjectId = matches[0]!.customerId;
      return newQueryBranch('found', { ...requested, customer: matches[0]! }, result.receipt);
    } catch (error) { return newQueryBranch('invalidResponse', requested, result.receipt, this.client.failure('PROTOCOL', 'findCustomer', `QuickBooks returned an invalid customer list: ${error instanceof Error ? error.message : 'invalid response'}`)); }
  }
}

/** Read one invoice by its QuickBooks ID. */
export class GetInvoiceOperation implements Query<GetInvoiceInput, Invoice> {
  /** Bind one client. */ constructor(private readonly client: QuickBooksClient) {}
  /** Return the manifest definition. */ definition() { return GetInvoiceDefinition; }
  /** Validate and read one invoice. */
  async invoke(call: Call, input: GetInvoiceInput): Promise<QueryAttempt<Invoice>> {
    const invoiceID = input.invoiceId.trim(), requested = { ...zeroInvoice, invoiceId: invoiceID };
    try { validID('invoiceId', invoiceID); } catch (error) { return newQueryBranch('defect', requested, {}, defect(this.client, 'getInvoice', error)); }
    const result = await this.client.exchange(call, 'getInvoice', { method: 'GET', path: `/invoice/${invoiceID}` }, invoiceID);
    const outcome = queryOutcome(result, requested, true); if (outcome) return outcome;
    try { const invoice = decodeInvoice(entity(result.body!, 'Invoice')); if (invoice.invoiceId !== invoiceID) throw new TypeError('QuickBooks returned another invoice'); return newQueryBranch('found', invoice, result.receipt); }
    catch (error) { return newQueryBranch('invalidResponse', requested, result.receipt, this.client.failure('PROTOCOL', 'getInvoice', `QuickBooks returned an invalid invoice: ${error instanceof Error ? error.message : 'invalid response'}`)); }
  }
}

/** Read one bounded page of invoices. */
export class ListInvoicesOperation implements Query<ListInvoicesInput, InvoicePage> {
  /** Bind one client. */ constructor(private readonly client: QuickBooksClient) {}
  /** Return the manifest definition. */ definition() { return ListInvoicesDefinition; }
  /** Build and execute the invoice page query. */
  async invoke(call: Call, input: ListInvoicesInput): Promise<QueryAttempt<InvoicePage>> {
    const empty: InvoicePage = { invoices: [], startPosition: 0, maxResults: 0, hasMorePages: false };
    let statement: string, startPosition: number, maxResults: number;
    try {
      startPosition = input.startPosition || 1; maxResults = input.maxResults || 25;
      if (startPosition < 1) throw new TypeError('startPosition must be 1 or greater');
      if (maxResults < 1 || maxResults > 100) throw new TypeError('maxResults must be between 1 and 100');
      const conditions: string[] = [];
      if (input.customerId?.trim()) { validID('customerId', input.customerId.trim()); conditions.push(`CustomerRef = ${quote(input.customerId.trim())}`); }
      if (input.docNumber?.trim()) { validQuery('docNumber', input.docNumber.trim(), 21); conditions.push(`DocNumber = ${quote(input.docNumber.trim())}`); }
      if (input.isOpenOnly) conditions.push("Balance > '0'");
      for (const [name, operator] of [['txnDateFrom', '>='], ['txnDateTo', '<=']]) { const value = (input[name as 'txnDateFrom' | 'txnDateTo'] ?? '').trim(); if (value) { validDate(name!, value); conditions.push(`TxnDate ${operator} ${quote(value)}`); } }
      if (input.updatedSince) conditions.push(`MetaData.LastUpdatedTime >= ${quote(parseRFC3339(input.updatedSince, false))}`);
      statement = `select * from Invoice${conditions.length ? ` where ${conditions.join(' AND ')}` : ''} ORDERBY MetaData.CreateTime STARTPOSITION ${startPosition} MAXRESULTS ${maxResults}`;
    } catch (error) { return newQueryBranch('defect', empty, {}, defect(this.client, 'listInvoices', error)); }
    const result = await this.client.exchange(call, 'listInvoices', { method: 'GET', path: '/query', query: { query: statement } });
    const outcome = queryOutcome(result, empty); if (outcome) return outcome;
    try {
      const invoices = entities(result.body!, 'Invoice').map(decodeInvoice);
      if (invoices.length > maxResults) throw new TypeError('response has more invoices than requested');
      const page: InvoicePage = { invoices, startPosition, maxResults, hasMorePages: invoices.length === maxResults, ...(invoices.length === maxResults ? { nextStartPosition: startPosition + maxResults } : {}) };
      return newQueryBranch('listed', page, result.receipt);
    } catch (error) { return newQueryBranch('invalidResponse', empty, result.receipt, this.client.failure('PROTOCOL', 'listInvoices', `QuickBooks returned an invalid invoice page: ${error instanceof Error ? error.message : 'invalid response'}`)); }
  }
}

function encoded(value: unknown): Uint8Array {
  const serialize = (part: unknown): string => {
    if (part && typeof part === 'object' && '__quickbooksRawNumber' in part) return String(part.__quickbooksRawNumber);
    if (Array.isArray(part)) return `[${part.map(serialize).join(',')}]`;
    if (part && typeof part === 'object') return `{${Object.entries(part).filter(([, item]) => item !== undefined).map(([key, item]) => `${JSON.stringify(key)}:${serialize(item)}`).join(',')}}`;
    return JSON.stringify(part);
  };
  return Buffer.from(serialize(value), 'utf8');
}
function validDecimal(name: string, value: string, fraction: number, integer: number, negative: boolean, zero: boolean): void {
  if (!/^-?(?:0|[1-9][0-9]{0,17})(?:\.[0-9]{1,10})?$/.test(value)) throw new TypeError(`${name} must be an exact decimal number such as 1800.00, without an exponent or leading zeros`);
  const unsigned = value.replace(/^-/, ''), [whole, fractional = ''] = unsigned.split('.');
  if (fractional.length > fraction) throw new TypeError(`${name} can have at most ${fraction} decimal places`);
  if (whole!.length > integer) throw new TypeError(`${name} can have at most ${integer} digits before the decimal point`);
  if (!negative && value.startsWith('-') && /[1-9]/.test(value)) throw new TypeError(`${name} cannot be negative`);
  if (!zero && !/[1-9]/.test(value)) throw new TypeError(`${name} must be greater than zero`);
}
function rawNumber(value: string): unknown { return { __quickbooksRawNumber: value }; }
function acceptedID(body: Uint8Array | undefined, name: string): string { if (!body) return ''; try { const found = parse(body)[name]; const id = string(object(found).Id); return entityID.test(id) ? id : ''; } catch { return ''; } }
async function readAccepted(client: QuickBooksClient, call: Call, operation: string, name: string, id: string): Promise<Record<string, unknown> | undefined> {
  if (!id) return undefined;
  const result = await client.exchange(call, operation, { method: 'GET', path: `/${name.toLowerCase()}/${id}` });
  if (result.disposition !== 'succeeded') return undefined;
  try { return entity(result.body!, name); } catch { return undefined; }
}

/** Create one QuickBooks customer under a stable requestid. */
export class CreateCustomerOperation implements Mutation<CreateCustomerInput, CreateCustomerOutput> {
  /** Bind one client. */ constructor(private readonly client: QuickBooksClient) {}
  /** Return the manifest definition. */ definition() { return CreateCustomerDefinition; }
  /** Use Dex Call ID as QuickBooks requestid. */ idempotencyKey(callID: string): string { return callID; }
  /** Validate, create, and confirm an accepted write if necessary. */
  async invoke(call: Call, input: CreateCustomerInput): Promise<MutationAttempt<CreateCustomerOutput>> {
    const requested: CreateCustomerOutput = { customer: zeroCustomer, displayName: input.displayName.trim() };
    let payload: Record<string, unknown>;
    try {
      const displayName = input.displayName.trim(), givenName = input.givenName?.trim() ?? '', familyName = input.familyName?.trim() ?? '', companyName = input.companyName?.trim() ?? '', email = input.emailAddress?.trim() ?? '', phone = input.phone?.trim() ?? '', currency = input.currencyCode?.trim() ?? '';
      if (!displayName) throw new TypeError('displayName is required');
      for (const [name, value, max] of [['displayName', displayName, 500], ['givenName', givenName, 100], ['familyName', familyName, 100], ['companyName', companyName, 100]] as const) validText(name, value, max);
      if (displayName.includes(':')) throw new TypeError('displayName cannot contain a colon, which QuickBooks reserves for sub-customers');
      if (email) { validEmail('emailAddress', email); validText('emailAddress', email, 100); }
      if (phone) validText('phone', phone, 30);
      validCurrency('currencyCode', currency);
      payload = { DisplayName: displayName, ...optional(!!email, 'PrimaryEmailAddr', { Address: email }), ...optional(!!givenName, 'GivenName', givenName), ...optional(!!familyName, 'FamilyName', familyName), ...optional(!!companyName, 'CompanyName', companyName), ...optional(!!phone, 'PrimaryPhone', { FreeFormNumber: phone }), ...optional(!!currency, 'CurrencyRef', { value: currency }) };
    } catch (error) { return newMutationBranch('defect', requested, {}, defect(this.client, 'createCustomer', error)); }
    const result = await this.client.exchange(call, 'createCustomer', { method: 'POST', path: '/customer', body: encoded(payload), mutation: true });
    const outcome = mutationOutcome(result, requested, true); if (outcome) return outcome;
    let cause = 'invalid customer';
    try {
      const customer = decodeCustomer(entity(result.body!, 'Customer'));
      if (!customerMatches(customer, payload)) throw new TypeError('QuickBooks returned another customer');
      result.receipt.providerObjectId = customer.customerId;
      return newMutationBranch('created', { customer, displayName: requested.displayName }, result.receipt);
    } catch (error) { cause = error instanceof Error ? error.message : cause; }
    const id = acceptedID(result.body, 'Customer');
    const read = await readAccepted(this.client, call, 'createCustomer', 'Customer', id);
    if (read) try { const customer = decodeCustomer(read); if (customer.customerId === id && customerMatches(customer, payload)) { result.receipt.providerObjectId = id; return newMutationBranch('created', { customer, displayName: requested.displayName }, result.receipt); } } catch { /* An unusable read-back leaves the accepted write uncertain. */ }
    return newMutationUncertain(requested, this.client.failure('PROTOCOL', 'createCustomer', `QuickBooks accepted the customer but returned an unusable customer: ${cause}`), result.receipt);
  }
}
function customerMatches(customer: Customer, payload: Record<string, unknown>): boolean {
  if (customer.displayName !== payload.DisplayName) return false;
  for (const [requested, actual] of [['GivenName', customer.givenName], ['FamilyName', customer.familyName], ['CompanyName', customer.companyName]] as const) if (payload[requested] && actual !== payload[requested]) return false;
  return !payload.PrimaryEmailAddr || (customer.emailAddress ?? '').toLowerCase() === string(object(payload.PrimaryEmailAddr).Address).toLowerCase();
}

/** Create one invoice with exact decimal line amounts. */
export class CreateInvoiceOperation implements Mutation<CreateInvoiceInput, CreateInvoiceOutput> {
  /** Bind one client. */ constructor(private readonly client: QuickBooksClient) {}
  /** Return the manifest definition. */ definition() { return CreateInvoiceDefinition; }
  /** Use Dex Call ID as QuickBooks requestid. */ idempotencyKey(callID: string): string { return callID; }
  /** Validate, create, and read back an accepted invoice if necessary. */
  async invoke(call: Call, input: CreateInvoiceInput): Promise<MutationAttempt<CreateInvoiceOutput>> {
    const requested: CreateInvoiceOutput = { invoice: zeroInvoice, ...optional(!!input.docNumber?.trim(), 'docNumber', input.docNumber?.trim() ?? '') };
    let payload: Record<string, unknown>;
    try { payload = buildInvoicePayload(input); }
    catch (error) { return newMutationBranch('defect', requested, {}, defect(this.client, 'createInvoice', error)); }
    const result = await this.client.exchange(call, 'createInvoice', { method: 'POST', path: '/invoice', body: encoded(payload), mutation: true });
    const outcome = mutationOutcome(result, requested); if (outcome) return outcome;
    let cause = 'invalid invoice';
    try { const invoice = decodeInvoice(entity(result.body!, 'Invoice')); if (invoice.customerId !== input.customerId.trim()) throw new TypeError('invoice belongs to another customer'); result.receipt.providerObjectId = invoice.invoiceId; return newMutationBranch('created', { invoice, ...optional(!!requested.docNumber, 'docNumber', requested.docNumber ?? '') }, result.receipt); }
    catch (error) { cause = error instanceof Error ? error.message : cause; }
    const id = acceptedID(result.body, 'Invoice'), read = await readAccepted(this.client, call, 'createInvoice', 'Invoice', id);
    if (read) try { const invoice = decodeInvoice(read); if (invoice.invoiceId === id && invoice.customerId === input.customerId.trim() && (!requested.docNumber || invoice.docNumber === requested.docNumber)) { result.receipt.providerObjectId = id; return newMutationBranch('created', { invoice, ...optional(!!requested.docNumber, 'docNumber', requested.docNumber ?? '') }, result.receipt); } } catch { /* An unusable read-back leaves the accepted write uncertain. */ }
    return newMutationUncertain(requested, this.client.failure('PROTOCOL', 'createInvoice', `QuickBooks accepted the invoice but returned an unusable invoice: ${cause}`), result.receipt);
  }
}
function buildInvoicePayload(input: CreateInvoiceInput): Record<string, unknown> {
  const customer = input.customerId.trim(), txnDate = input.txnDate?.trim() ?? '', dueDate = input.dueDate?.trim() ?? '', docNumber = input.docNumber?.trim() ?? '', billEmail = input.billEmail?.trim() ?? '', currency = input.currencyCode?.trim() ?? '';
  validID('customerId', customer); if (txnDate) validDate('txnDate', txnDate); if (dueDate) validDate('dueDate', dueDate); if (txnDate && dueDate && dueDate < txnDate) throw new TypeError('dueDate cannot be before txnDate'); validQuery('docNumber', docNumber, 21); if (billEmail) validEmail('billEmail', billEmail); validCurrency('currencyCode', currency);
  validText('customerMemo', input.customerMemo ?? '', 1000, true); validText('privateNote', input.privateNote ?? '', 4000, true);
  if (!input.lines || input.lines.length < 1 || input.lines.length > 100) throw new TypeError('lines must hold 1 to 100 lines');
  return { CustomerRef: { value: customer }, Line: input.lines.map((line, index) => buildInvoiceLine(line, index)), ...optional(!!txnDate, 'TxnDate', txnDate), ...optional(!!dueDate, 'DueDate', dueDate), ...optional(!!docNumber, 'DocNumber', docNumber), ...optional(!!billEmail, 'BillEmail', { Address: billEmail }), ...optional(!!currency, 'CurrencyRef', { value: currency }), ...optional(!!input.customerMemo, 'CustomerMemo', { value: input.customerMemo ?? '' }), ...optional(!!input.privateNote, 'PrivateNote', input.privateNote ?? '') };
}
function buildInvoiceLine(line: InvoiceLineInput, index: number): Record<string, unknown> {
  const prefix = `lines[${index}]`, itemID = line.itemId.trim(), serviceDate = line.serviceDate?.trim() ?? '', taxCode = line.taxCodeId?.trim() ?? '';
  validID(`${prefix}.itemId`, itemID); validText(`${prefix}.description`, line.description ?? '', 4000, true); if (serviceDate) validDate(`${prefix}.serviceDate`, serviceDate); if (taxCode && !/^[A-Za-z0-9]{1,20}$/.test(taxCode)) throw new TypeError(`${prefix}.taxCodeId must be a QuickBooks tax code such as TAX, NON, or 3`);
  if (line.quantity) validDecimal(`${prefix}.quantity`, line.quantity, 5, 12, false, true);
  if (line.unitPrice) validDecimal(`${prefix}.unitPrice`, line.unitPrice, 5, 12, true, true);
  let amount = line.amount ?? '';
  if (amount) validDecimal(`${prefix}.amount`, amount, 2, 10, true, true);
  else { if (!line.quantity || !line.unitPrice) throw new TypeError(`${prefix} needs an amount, or both quantity and unitPrice`); amount = multiplyRoundedToCents(line.quantity, line.unitPrice); validDecimal(`${prefix}.amount`, amount, 2, 10, true, true); }
  return { DetailType: 'SalesItemLineDetail', Amount: rawNumber(amount), ...optional(!!line.description, 'Description', line.description ?? ''), SalesItemLineDetail: { ItemRef: { value: itemID }, ...optional(!!line.quantity, 'Qty', rawNumber(line.quantity ?? '')), ...optional(!!line.unitPrice, 'UnitPrice', rawNumber(line.unitPrice ?? '')), ...optional(!!taxCode, 'TaxCodeRef', { value: taxCode }), ...optional(!!serviceDate, 'ServiceDate', serviceDate) } };
}
function multiplyRoundedToCents(quantity: string, unitPrice: string): string {
  const scaled = (value: string): { digits: bigint; scale: number } => {
    const sign = value.startsWith('-') ? -1n : 1n;
    const unsigned = value.replace(/^-/, '');
    const [whole, fraction = ''] = unsigned.split('.');
    return { digits: sign * BigInt(whole! + fraction), scale: fraction.length };
  };
  const left = scaled(quantity), right = scaled(unitPrice);
  const product = left.digits * right.digits;
  const scale = left.scale + right.scale;
  const divisor = 10n ** BigInt(Math.max(scale - 2, 0));
  const magnitude = product < 0n ? -product : product;
  const cents = scale > 2 ? (magnitude + divisor / 2n) / divisor : magnitude * 10n ** BigInt(2 - scale);
  return `${product < 0n && cents !== 0n ? '-' : ''}${cents / 100n}.${String(cents % 100n).padStart(2, '0')}`;
}

/** Email one invoice under the Step requestid. */
export class SendInvoiceOperation implements Mutation<SendInvoiceInput, Invoice> {
  /** Bind one client. */ constructor(private readonly client: QuickBooksClient) {}
  /** Return the manifest definition. */ definition() { return SendInvoiceDefinition; }
  /** Use Dex Call ID as QuickBooks requestid. */ idempotencyKey(callID: string): string { return callID; }
  /** Send and confirm one invoice delivery. */
  async invoke(call: Call, input: SendInvoiceInput): Promise<MutationAttempt<Invoice>> {
    const invoiceID = input.invoiceId.trim(), sendTo = input.sendTo?.trim() ?? '', requested: Invoice = { ...zeroInvoice, invoiceId: invoiceID };
    try { validID('invoiceId', invoiceID); if (sendTo) validEmail('sendTo', sendTo); }
    catch (error) { return newMutationBranch('defect', requested, {}, defect(this.client, 'sendInvoice', error)); }
    const result = await this.client.exchange(call, 'sendInvoice', { method: 'POST', path: `/invoice/${invoiceID}/send`, query: sendTo ? { sendTo } : {}, mutation: true }, invoiceID);
    const outcome = mutationOutcome(result, requested); if (outcome) return outcome;
    let cause = 'invalid invoice';
    try { const invoice = decodeInvoice(entity(result.body!, 'Invoice')); verifySent(invoice, invoiceID, sendTo); return newMutationBranch('sent', invoice, result.receipt); }
    catch (error) { cause = error instanceof Error ? error.message : cause; }
    const read = await readAccepted(this.client, call, 'sendInvoice', 'Invoice', invoiceID);
    if (read) try { const invoice = decodeInvoice(read); verifySent(invoice, invoiceID, sendTo); return newMutationBranch('sent', invoice, result.receipt); } catch { /* An unusable read-back leaves delivery uncertain. */ }
    return newMutationUncertain(requested, this.client.failure('PROTOCOL', 'sendInvoice', `QuickBooks accepted the delivery but returned an unusable invoice: ${cause}`), result.receipt);
  }
}
function verifySent(invoice: Invoice, invoiceID: string, sendTo: string): void { if (invoice.invoiceId !== invoiceID) throw new TypeError('QuickBooks returned another invoice'); if (invoice.emailStatus !== 'EmailSent') throw new TypeError('the returned invoice is not marked EmailSent'); if (sendTo && invoice.billEmail?.toLowerCase() !== sendTo.toLowerCase()) throw new TypeError('the returned invoice has another billing address'); }

/** Record one invoice payment under the Step requestid. */
export class RecordPaymentOperation implements Mutation<RecordPaymentInput, Payment> {
  /** Bind one client. */ constructor(private readonly client: QuickBooksClient) {}
  /** Return the manifest definition. */ definition() { return RecordPaymentDefinition; }
  /** Use Dex Call ID as QuickBooks requestid. */ idempotencyKey(callID: string): string { return callID; }
  /** Record and confirm one payment. */
  async invoke(call: Call, input: RecordPaymentInput): Promise<MutationAttempt<Payment>> {
    const requested: Payment = { ...zeroPayment, customerId: input.customerId.trim(), totalAmount: input.amount };
    let payload: Record<string, unknown>;
    try { payload = buildPaymentPayload(input); }
    catch (error) { return newMutationBranch('defect', requested, {}, defect(this.client, 'recordPayment', error)); }
    const result = await this.client.exchange(call, 'recordPayment', { method: 'POST', path: '/payment', body: encoded(payload), mutation: true });
    const outcome = mutationOutcome(result, requested); if (outcome) return outcome;
    let cause = 'invalid payment';
    try { const payment = decodePayment(entity(result.body!, 'Payment')); verifyPayment(payment, input); result.receipt.providerObjectId = payment.paymentId; return newMutationBranch('recorded', payment, result.receipt); }
    catch (error) { cause = error instanceof Error ? error.message : cause; }
    const id = acceptedID(result.body, 'Payment'), read = await readAccepted(this.client, call, 'recordPayment', 'Payment', id);
    if (read) try { const payment = decodePayment(read); verifyPayment(payment, input); if (payment.paymentId !== id) throw new TypeError('another payment'); result.receipt.providerObjectId = id; return newMutationBranch('recorded', payment, result.receipt); } catch { /* An unusable read-back leaves the accepted payment uncertain. */ }
    return newMutationUncertain(requested, this.client.failure('PROTOCOL', 'recordPayment', `QuickBooks accepted the payment but returned an unusable payment: ${cause}`), result.receipt);
  }
}
function buildPaymentPayload(input: RecordPaymentInput): Record<string, unknown> {
  const customer = input.customerId.trim(), invoice = input.invoiceId.trim(), txnDate = input.txnDate?.trim() ?? '', referenceNumber = input.paymentReferenceNumber?.trim() ?? '', deposit = input.depositAccountId?.trim() ?? '', method = input.paymentMethodId?.trim() ?? '', currency = input.currencyCode?.trim() ?? '';
  validID('customerId', customer); validID('invoiceId', invoice); validDecimal('amount', input.amount, 2, 10, false, false);
  if (txnDate) validDate('txnDate', txnDate); validText('paymentReferenceNumber', referenceNumber, 21); validText('privateNote', input.privateNote ?? '', 4000, true); if (deposit) validID('depositAccountId', deposit); if (method) validID('paymentMethodId', method); validCurrency('currencyCode', currency);
  return { CustomerRef: { value: customer }, TotalAmt: rawNumber(input.amount), ...optional(!!txnDate, 'TxnDate', txnDate), Line: [{ Amount: rawNumber(input.amount), LinkedTxn: [{ TxnId: invoice, TxnType: 'Invoice' }] }], ...optional(!!referenceNumber, 'PaymentRefNum', referenceNumber), ...optional(!!deposit, 'DepositToAccountRef', { value: deposit }), ...optional(!!method, 'PaymentMethodRef', { value: method }), ...optional(!!currency, 'CurrencyRef', { value: currency }), ...optional(!!input.privateNote, 'PrivateNote', input.privateNote ?? '') };
}
function verifyPayment(payment: Payment, input: RecordPaymentInput): void { if (payment.customerId !== input.customerId.trim() || !payment.appliedInvoices?.some(item => item.invoiceId === input.invoiceId.trim())) throw new TypeError('the returned payment does not apply to the invoice'); }
