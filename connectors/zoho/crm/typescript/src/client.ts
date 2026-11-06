// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { Buffer } from 'node:buffer';
import { buildFindRecordsQuery, buildListModifiedRecordsQuery, isFieldName, validateFields, validateModule, validateRecordId } from './coql.js';
import { FindRecordsDefinition, GetRecordDefinition, ListModifiedRecordsDefinition, UpsertRecordDefinition, UpdateRecordDefinition, ListModuleFieldsDefinition } from './definitions.js';
import { decodeModuleFields, decodeRecordPage, decodeWriteResult, parseZohoTime, validateWriteFields } from './records.js';
import { ZohoHTTPClient, crmFailure, describeError, isMissingRecord, providerErrors, type Exchange, type ZohoClientOptions, type ZohoSession } from './http.js';
import type { Config, CredentialSource, FindRecordsInput, FindRecordsOutput, GetRecordInput, ListModifiedRecordsInput, ListModifiedRecordsOutput, ListModuleFieldsInput, ListModuleFieldsOutput, Record as ZohoRecord, UpdateRecordInput, UpdateRecordOutput, UpsertRecordInput, UpsertRecordOutput } from './models.js';
import type { Operations } from './generated/connection.js';

/** Encode query parameters exactly as Go url.Values.Encode, including sorted keys. */
export function encodeGoQuery(values: Record<string, string[]>): string {
  const escape = (value: string): string => Array.from(new TextEncoder().encode(value)).map(byte => {
    if ((byte >= 65 && byte <= 90) || (byte >= 97 && byte <= 122) || (byte >= 48 && byte <= 57) || [45, 46, 95, 126].includes(byte)) return String.fromCharCode(byte);
    return byte === 32 ? '+' : `%${byte.toString(16).toUpperCase().padStart(2, '0')}`;
  }).join('');
  return Object.keys(values).sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right))).flatMap(key => (values[key] ?? []).map(value => `${escape(key)}=${escape(value)}`)).join('&');
}

/** Escape path segments accepted by Go url.PathEscape. */
export function escapeGoPath(value: string): string {
  return Array.from(new TextEncoder().encode(value)).map(byte => {
    if ((byte >= 65 && byte <= 90) || (byte >= 97 && byte <= 122) || (byte >= 48 && byte <= 57) || '-._~$&+,;=:@'.includes(String.fromCharCode(byte))) return String.fromCharCode(byte);
    return `%${byte.toString(16).toUpperCase().padStart(2, '0')}`;
  }).join('');
}

function emptyRecord(): ZohoRecord { return { module: '', id: '', fields: {} }; }
function emptyFind(): FindRecordsOutput { return { module: '', records: [] }; }
function emptyList(): ListModifiedRecordsOutput { return { module: '', records: [], cursor: '' }; }
function emptyFields(): ListModuleFieldsOutput { return { module: '', fields: [] }; }
function emptyUpsert(): UpsertRecordOutput { return { module: '' }; }
function emptyUpdate(): UpdateRecordOutput { return { module: '' }; }

function queryFailure<Output>(http: ZohoHTTPClient, call: Call, result: Exchange, operation: string, zero: Output, hasNotFound: boolean, isSession = false): QueryAttempt<Output> | undefined {
  if (result.outcome === 'success') return undefined;
  if (result.outcome === 'retry') return newQueryRetry(result.failure!, result.retryAfterMs);
  const branch = result.outcome === 'invalid' ? 'invalidResponse' : result.outcome === 'defect' ? 'defect' : hasNotFound && isMissingRecord(result.summary) ? 'notFound' : 'providerRejected';
  return newQueryBranch(branch, zero, isSession ? zeroReceipt() : http.receipt(call, result), result.failure);
}

function mutationFailure<Output>(http: ZohoHTTPClient, call: Call, result: Exchange, operation: string, value: Output, recordId: string, hasNotFound: boolean, isSession = false): MutationAttempt<Output> | undefined {
  if (result.outcome === 'success') return undefined;
  if (result.outcome === 'retry') return newMutationRetry(result.failure!, result.retryAfterMs);
  let branch: string;
  if (result.outcome === 'invalid') branch = 'invalidResponse';
  else if (result.outcome === 'defect') branch = 'defect';
  else if (result.summary.code === 'DUPLICATE_DATA') branch = 'conflict';
  else if (hasNotFound && isMissingRecord(result.summary)) branch = 'notFound';
  else if (['OAUTH_SCOPE_MISMATCH', 'NO_PERMISSION', 'AUTHORIZATION_FAILED', 'INVALID_MODULE', 'NOT_SUPPORTED', 'INVALID_URL_PATTERN', 'INVALID_REQUEST_METHOD'].includes(result.summary.code) || (result.status >= 300 && result.status !== 400 && result.status !== 422)) branch = 'providerRejected';
  else branch = 'recordRejected';
  return newMutationBranch(branch, value, isSession ? zeroReceipt() : http.receipt(call, result, recordId), result.failure);
}

function validationAttempt<Output>(operation: string, zero: Output, error: unknown): QueryAttempt<Output> {
  return newQueryBranch('defect', zero, zeroReceipt(), crmFailure('VALIDATION', operation, error instanceof Error ? error.message : 'invalid input'));
}

function validateTriggers(triggers: string[] | undefined, shouldSkipAutomation: boolean | undefined): string[] | undefined {
  if (shouldSkipAutomation) {
    if (triggers?.length) throw new TypeError('set triggers or shouldSkipAutomation, not both');
    return [];
  }
  if (!triggers?.length) return undefined;
  const seen = new Set<string>();
  for (const trigger of triggers) {
    if (!['workflow', 'approval', 'blueprint'].includes(trigger)) throw new TypeError('each trigger must be workflow, approval, or blueprint');
    if (seen.has(trigger)) throw new TypeError('triggers must not repeat');
    seen.add(trigger);
  }
  return triggers;
}

async function exchangeWrite(http: ZohoHTTPClient, call: Call, operation: string, session: ZohoSession, method: string, path: string, payload: unknown): Promise<{ exchange: Exchange; written?: ReturnType<typeof decodeWriteResult> }> {
  const exchange = await http.exchange(call, operation, session, method, path, payload);
  if (exchange.outcome !== 'success') return { exchange };
  let written: ReturnType<typeof decodeWriteResult>;
  try { written = decodeWriteResult(exchange.body); }
  catch (error) { return { exchange: { ...exchange, outcome: 'invalid', failure: crmFailure('PROTOCOL', operation, `Zoho CRM returned an invalid write result: ${error instanceof Error ? error.message : 'invalid response'}`) } }; }
  if (written.error) return { exchange: http.rejectRecord({ ...exchange, summary: describeError(exchange.body) }, operation) };
  return { exchange, written };
}

/** Six typed Zoho CRM operations sharing one bounded, renewable provider client. */
export class ZohoClient implements Operations {
  readonly #http: ZohoHTTPClient;

  /** Validate configuration and inject host-managed credential storage. */
  constructor(config: Config, credentials: CredentialSource, options: ZohoClientOptions = {}) { this.#http = new ZohoHTTPClient(config, credentials, options); }

  /** Find records matching all COQL conditions. */
  findRecords(): Query<FindRecordsInput, FindRecordsOutput> {
    const http = this.#http;
    return { definition: () => FindRecordsDefinition, invoke: async (call, input) => {
      let query: string;
      try { query = buildFindRecordsQuery(input); } catch (error) { return validationAttempt('findRecords', emptyFind(), error); }
      const session = await http.session(call, 'findRecords');
      if ('outcome' in session) return queryFailure(http, call, session, 'findRecords', emptyFind(), false, true)!;
      const result = await http.exchange(call, 'findRecords', session, 'POST', '/coql', { select_query: query });
      const failed = queryFailure(http, call, result, 'findRecords', emptyFind(), false);
      if (failed) return failed;
      const receipt = http.receipt(call, result);
      let page: ReturnType<typeof decodeRecordPage>;
      try { page = decodeRecordPage(input.module, result.body); }
      catch (error) { return newQueryBranch('invalidResponse', emptyFind(), receipt, crmFailure('PROTOCOL', 'findRecords', `Zoho CRM returned an invalid page: ${String(error)}`)); }
      if (page.records.length > (input.limit || 10)) return newQueryBranch('invalidResponse', emptyFind(), receipt, crmFailure('PROTOCOL', 'findRecords', 'Zoho CRM returned more records than the page size'));
      const nextOffset = (input.offset ?? 0) + page.records.length;
      const output: FindRecordsOutput = { module: input.module, records: page.records, ...(page.hasMore && page.records.length && nextOffset < 100_000 ? { nextOffset } : {}) };
      return newQueryBranch(page.records.length ? 'found' : 'notFound', output, receipt);
    } };
  }

  /** Read one CRM record by module and ID. */
  getRecord(): Query<GetRecordInput, ZohoRecord> {
    const http = this.#http;
    return { definition: () => GetRecordDefinition, invoke: async (call, input) => {
      try {
        validateModule(input.module); validateRecordId(input.recordId); validateFields(input.fields ?? [], 0);
        if (input.fields?.some(field => !isFieldName(field))) throw new TypeError('getRecord fields must be field API names without a lookup path; use findRecords for Account_Name.Account_Name');
      } catch (error) { return validationAttempt('getRecord', emptyRecord(), error); }
      const session = await http.session(call, 'getRecord');
      if ('outcome' in session) return queryFailure(http, call, session, 'getRecord', emptyRecord(), true, true)!;
      const query = input.fields?.length ? `?${encodeGoQuery({ fields: [input.fields.join(',')] })}` : '';
      const result = await http.exchange(call, 'getRecord', session, 'GET', `/${escapeGoPath(input.module)}/${escapeGoPath(input.recordId)}${query}`);
      const failed = queryFailure(http, call, result, 'getRecord', emptyRecord(), true);
      if (failed) return failed;
      const receipt = http.receipt(call, result, input.recordId);
      if (result.status === 204 || !new TextDecoder().decode(result.body).trim()) return newQueryBranch('notFound', emptyRecord(), receipt, crmFailure('NOT_FOUND', 'getRecord', 'Zoho CRM found no record with the ID (HTTP 204)'));
      let page: ReturnType<typeof decodeRecordPage>;
      try { page = decodeRecordPage(input.module, result.body); }
      catch (error) { return newQueryBranch('invalidResponse', emptyRecord(), receipt, crmFailure('PROTOCOL', 'getRecord', `Zoho CRM returned an invalid record: ${String(error)}`)); }
      if (!page.records.length) return newQueryBranch('notFound', emptyRecord(), receipt, crmFailure('NOT_FOUND', 'getRecord', 'Zoho CRM returned no record with the ID'));
      if (page.records.length !== 1 || page.records[0]!.id !== input.recordId) return newQueryBranch('invalidResponse', emptyRecord(), receipt, crmFailure('PROTOCOL', 'getRecord', 'Zoho CRM returned a record other than the one requested'));
      return newQueryBranch('found', page.records[0]!, receipt);
    } };
  }

  /** Read modified records after a stable watermark. */
  listModifiedRecords(): Query<ListModifiedRecordsInput, ListModifiedRecordsOutput> {
    const http = this.#http;
    return { definition: () => ListModifiedRecordsDefinition, invoke: async (call, input) => {
      let built: ReturnType<typeof buildListModifiedRecordsQuery>;
      try { built = buildListModifiedRecordsQuery(input); } catch (error) { return validationAttempt('listModifiedRecords', emptyList(), error); }
      const session = await http.session(call, 'listModifiedRecords');
      if ('outcome' in session) return queryFailure(http, call, session, 'listModifiedRecords', emptyList(), false, true)!;
      const result = await http.exchange(call, 'listModifiedRecords', session, 'POST', '/coql', { select_query: built.query });
      const failed = queryFailure(http, call, result, 'listModifiedRecords', emptyList(), false);
      if (failed) return failed;
      const receipt = http.receipt(call, result);
      try {
        const page = decodeRecordPage(input.module, result.body);
        if (page.records.length > (input.limit || 100)) throw new TypeError('Zoho CRM returned more records than the page size');
        let cursor = built.cursor;
        for (const record of page.records) {
          const modified = parseZohoTime(record.fields?.Modified_Time);
          if (!modified) throw new TypeError('a record has no valid Modified_Time');
          const at = new Date(modified).toISOString().replace(/\.\d{3}Z$/, 'Z');
          const id = record.id.replace(/^0+/, '') || '0';
          const [oldAt, oldId] = cursor.split('/');
          if (at < oldAt! || (at === oldAt && oldId !== '0' && BigInt(id) <= BigInt(oldId!))) throw new TypeError('records are not in ascending Modified_Time and id order after the cursor');
          cursor = `${at}/${id}`;
        }
        return newQueryBranch('listed', { module: input.module, records: page.records, ...(page.hasMore && page.records.length ? { hasMore: true } : {}), cursor }, receipt);
      } catch (error) { return newQueryBranch('invalidResponse', emptyList(), receipt, crmFailure('PROTOCOL', 'listModifiedRecords', `Zoho CRM returned an invalid page: ${String(error)}`)); }
    } };
  }

  /** Upsert one record by configured duplicate-check fields. */
  upsertRecord(): Mutation<UpsertRecordInput, UpsertRecordOutput> {
    const http = this.#http;
    return { definition: () => UpsertRecordDefinition, idempotencyKey: callId => callId, invoke: async (call, input) => {
      let payload: unknown;
      try {
        validateModule(input.module); const fields = validateWriteFields(input.fields);
        if (!input.duplicateCheckFields?.length || input.duplicateCheckFields.length > 3) throw new TypeError('duplicateCheckFields must name 1 to 3 fields');
        const seen = new Set<string>();
        for (const field of input.duplicateCheckFields) {
          if (!isFieldName(field) || seen.has(field)) throw new TypeError('duplicateCheckFields must be distinct Zoho CRM field API names such as Email');
          seen.add(field);
          const value = fields[field];
          if (value === undefined || value === null || value === '' || (typeof value === 'string' && !value.trim()) || (typeof value === 'object' && !Object.keys(value).length)) throw new TypeError(`fields must set duplicate-check field ${field} to a non-empty value, or every attempt would insert a new record`);
        }
        const triggers = validateTriggers(input.triggers, input.shouldSkipAutomation);
        payload = { data: [fields], duplicate_check_fields: input.duplicateCheckFields, ...(triggers ? { trigger: triggers } : {}) };
      } catch (error) { return newMutationBranch('defect', emptyUpsert(), zeroReceipt(), crmFailure('VALIDATION', 'upsertRecord', error instanceof Error ? error.message : 'invalid input')); }
      const session = await http.session(call, 'upsertRecord');
      if ('outcome' in session) return mutationFailure(http, call, session, 'upsertRecord', emptyUpsert(), '', false, true)!;
      for (let send = 0; send < 2; send++) {
        const { exchange, written } = await exchangeWrite(http, call, 'upsertRecord', session, 'POST', `/${escapeGoPath(input.module)}/upsert`, payload);
        if (exchange.outcome === 'rejected' && exchange.summary.code === 'DUPLICATE_DATA' && send === 0) continue;
        const rejected: UpsertRecordOutput = { module: input.module, ...(providerErrors(exchange.summary) ? { providerErrors: providerErrors(exchange.summary) } : {}) };
        const failed = mutationFailure(http, call, exchange, 'upsertRecord', rejected, written?.id ?? '', false);
        if (failed) return failed;
        return newMutationBranch('upserted', { module: input.module, id: written!.id, ...(written!.action === 'insert' ? { isCreated: true } : {}), ...(written!.duplicateField ? { duplicateField: written!.duplicateField } : {}), ...(written!.createdAt ? { createdAt: written!.createdAt } : {}), ...(written!.modifiedAt ? { modifiedAt: written!.modifiedAt } : {}) }, http.receipt(call, exchange, written!.id));
      }
      throw new TypeError('unreachable duplicate resend');
    } };
  }

  /** Set absolute field values on one record. */
  updateRecord(): Mutation<UpdateRecordInput, UpdateRecordOutput> {
    const http = this.#http;
    return { definition: () => UpdateRecordDefinition, idempotencyKey: callId => callId, invoke: async (call, input) => {
      let payload: unknown;
      try {
        validateModule(input.module); validateRecordId(input.recordId); const fields = validateWriteFields(input.fields);
        const triggers = validateTriggers(input.triggers, input.shouldSkipAutomation);
        payload = { data: [fields], ...(triggers ? { trigger: triggers } : {}) };
      } catch (error) { return newMutationBranch('defect', emptyUpdate(), zeroReceipt(), crmFailure('VALIDATION', 'updateRecord', error instanceof Error ? error.message : 'invalid input')); }
      const session = await http.session(call, 'updateRecord');
      if ('outcome' in session) return mutationFailure(http, call, session, 'updateRecord', emptyUpdate(), input.recordId, true, true)!;
      const { exchange, written } = await exchangeWrite(http, call, 'updateRecord', session, 'PUT', `/${escapeGoPath(input.module)}/${escapeGoPath(input.recordId)}`, payload);
      const rejected: UpdateRecordOutput = { module: input.module, ...(providerErrors(exchange.summary) ? { providerErrors: providerErrors(exchange.summary) } : {}) };
      const failed = mutationFailure(http, call, exchange, 'updateRecord', rejected, input.recordId, true);
      if (failed) return failed;
      const receipt = http.receipt(call, exchange, input.recordId);
      if (written!.id !== input.recordId) return newMutationBranch('invalidResponse', { module: input.module }, receipt, crmFailure('PROTOCOL', 'updateRecord', 'Zoho CRM reported a write to a record other than the one requested'));
      return newMutationBranch('updated', { module: input.module, id: written!.id, ...(written!.modifiedAt ? { modifiedAt: written!.modifiedAt } : {}) }, receipt);
    } };
  }

  /** Read module field metadata and picklist values. */
  listModuleFields(): Query<ListModuleFieldsInput, ListModuleFieldsOutput> {
    const http = this.#http;
    return { definition: () => ListModuleFieldsDefinition, invoke: async (call, input) => {
      try { validateModule(input.module); } catch (error) { return validationAttempt('listModuleFields', emptyFields(), error); }
      const session = await http.session(call, 'listModuleFields');
      if ('outcome' in session) return queryFailure(http, call, session, 'listModuleFields', emptyFields(), false, true)!;
      const result = await http.exchange(call, 'listModuleFields', session, 'GET', `/settings/fields?${encodeGoQuery({ module: [input.module] })}`);
      const failed = queryFailure(http, call, result, 'listModuleFields', emptyFields(), false);
      if (failed) return failed;
      const receipt = http.receipt(call, result);
      try { return newQueryBranch('listed', { module: input.module, fields: decodeModuleFields(result.body) }, receipt); }
      catch (error) { return newQueryBranch('invalidResponse', emptyFields(), receipt, crmFailure('PROTOCOL', 'listModuleFields', `Zoho CRM returned an invalid field list: ${String(error)}`)); }
    } };
  }
}
