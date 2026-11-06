// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { AddressRefusal } from '@superdurable/connectors-sdk/providerhttp';
import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { CreateEnvelopeFromTemplateDefinition, DownloadCombinedDocumentDefinition, GetEnvelopeDefinition, ListEnvelopeRecipientsDefinition, VoidEnvelopeDefinition } from './generated/definitions.js';
import type { CombinedDocument, CreatedEnvelope, CreateEnvelopeFromTemplateInput, DownloadCombinedDocumentInput, Envelope, EnvelopeRecipients, GetEnvelopeInput, ListEnvelopeRecipientsInput, Recipient, VoidEnvelopeInput, VoidedEnvelope } from './generated/models.js';
import { DocuSignClient, ResponseTooLargeError, SessionError, escapePath, failure, isGuid, type DocuSignOutcome, type DocuSignResponse, type DocuSignSession } from './client.js';

const zeroCreated: CreatedEnvelope = { envelopeId: '', status: '' };
const zeroEnvelope: Envelope = { envelopeId: '', status: '' };
const zeroRecipients: EnvelopeRecipients = { envelopeId: '', recipients: [] };
const zeroVoided: VoidedEnvelope = { envelopeId: '' };
const zeroDocument: CombinedDocument = { envelopeId: '', contentType: '', byteCount: 0, sha256: '' };
const malformed = 'DocuSign returned a malformed response';
const tooLarge = 'DocuSign response exceeds the configured maxResponseBytes';

/** Create a server-template envelope with read-back after an unconfirmed POST. */
export class CreateEnvelopeFromTemplateOperation implements Mutation<CreateEnvelopeFromTemplateInput, CreatedEnvelope> {
  /** Bind the authenticated DocuSign client. */
  constructor(private readonly client: DocuSignClient) {}
  /** Return manifest routes and Execute defaults. */
  definition() { return CreateEnvelopeFromTemplateDefinition; }
  /** Use the stable Dex Call ID as the hidden deduplication marker. */
  idempotencyKey(callId: string): string { return callId; }
  /** Invoke one safe-to-classify create attempt. */
  async invoke(call: Call, input: CreateEnvelopeFromTemplateInput): Promise<MutationAttempt<CreatedEnvelope>> {
    const operation = 'createEnvelopeFromTemplate';
    const marker = call.idempotencyKey || call.id;
    let body: Record<string, unknown>;
    try { body = buildCreateBody(input, marker); }
    catch (error) { return newMutationBranch('defect', zeroCreated, zeroReceipt(), failure(operation, 'VALIDATION', errorMessage(error))); }
    const prior = readCreateCheckpoint(call);
    let session: DocuSignSession;
    try { session = await this.client.openSession(call, operation); }
    catch (error) {
      if (!(error instanceof SessionError)) throw error;
      if (prior) {
        await recordCheckpoint(call, prior);
        return error.route === 'retry' ? newMutationRetry(error.failure, error.retryAfterMs) : newMutationUncertain(zeroCreated, { ...error.failure, message: `an earlier attempt sent the DocuSign create request and its outcome is unknown; ${error.failure.message}` }, this.client.receipt(''));
      }
      return sessionMutationFailure(error, zeroCreated, 'providerRejected', 'providerRejected');
    }
    if (prior) return recoverCreated(this.client, call, session, marker, prior, true, failure(operation, 'TRANSPORT', 'an earlier attempt sent the DocuSign create request and its outcome is unknown'));
    if (call.context.attempt > 1) {
      const found = await findBeforeResend(this.client, call, session, marker);
      if (found) return found;
    }
    const checkpoint = { isDispatched: true, dispatchedAt: goTimestamp(this.client.currentTime()) };
    try { await call.context.recordHeartbeat(checkpoint); }
    catch { return newMutationRetry(failure(operation, 'AVAILABILITY', 'the create checkpoint could not be recorded, so nothing was sent to DocuSign')); }
    let response: DocuSignResponse;
    try { response = await this.client.requestAPI(call, session, 'POST', '/envelopes', undefined, body, 'application/json', this.client.maxResponseBytes, 40_000); }
    catch (error) {
      if (isProvablyNotSent(error)) { await clearCheckpoint(call); return newMutationRetry(failure(operation, 'TRANSPORT', 'DocuSign could not be reached, so no envelope was created')); }
      return recoverCreated(this.client, call, session, marker, checkpoint, false, failure(operation, 'TRANSPORT', "DocuSign's answer to the create request was lost"));
    }
    if (response.status === 200 || response.status === 201) {
      try { const created = decodeCreated(response.body, input.isDraft ?? false); return newMutationBranch('created', created, this.client.receipt(created.envelopeId)); }
      catch { return recoverCreated(this.client, call, session, marker, checkpoint, false, failure(operation, 'PROTOCOL', 'DocuSign accepted the create request but returned an unusable envelope summary')); }
    }
    const outcome = this.client.classify(operation, response);
    if (outcome.retry && response.status < 500 && response.status !== 408) { await clearCheckpoint(call); return newMutationRetry(outcome.failure, outcome.retryAfterMs); }
    if (response.status >= 400 && response.status < 500 && response.status !== 408) return newMutationBranch('providerRejected', zeroCreated, this.client.receipt('', outcome.errorCode), outcome.failure);
    return recoverCreated(this.client, call, session, marker, checkpoint, false, outcome.failure);
  }
}

/** Read one envelope with custom fields. */
export class GetEnvelopeOperation implements Query<GetEnvelopeInput, Envelope> {
  /** Bind the authenticated DocuSign client. */
  constructor(private readonly client: DocuSignClient) {}
  /** Return manifest routes and Execute defaults. */
  definition() { return GetEnvelopeDefinition; }
  /** Invoke one envelope read. */
  async invoke(call: Call, input: GetEnvelopeInput): Promise<QueryAttempt<Envelope>> {
    const operation = 'getEnvelope';
    if (!isGuid(input.envelopeId)) return newQueryBranch('defect', zeroEnvelope, zeroReceipt(), failure(operation, 'VALIDATION', 'envelopeId must be a DocuSign envelope ID GUID, such as 00000000-0000-0000-0000-000000000000'));
    let session: DocuSignSession;
    try { session = await this.client.openSession(call, operation); }
    catch (error) { if (error instanceof SessionError) return sessionQueryFailure(error, zeroEnvelope); throw error; }
    const read = await readEnvelope(this.client, call, session, input.envelopeId, operation);
    if (read.error) return newQueryBranch('invalidResponse', zeroEnvelope, this.client.receipt(input.envelopeId), read.error);
    if (read.outcome) return queryOutcome(this.client, read.outcome, input.envelopeId, zeroEnvelope);
    if (!read.envelope) throw new Error('envelope read returned no value');
    return newQueryBranch('found', read.envelope, this.client.receipt(read.envelope.envelopeId));
  }
}

/** Flatten DocuSign recipient collections into routing order. */
export class ListEnvelopeRecipientsOperation implements Query<ListEnvelopeRecipientsInput, EnvelopeRecipients> {
  /** Bind the authenticated DocuSign client. */
  constructor(private readonly client: DocuSignClient) {}
  /** Return manifest routes and Execute defaults. */
  definition() { return ListEnvelopeRecipientsDefinition; }
  /** Invoke one recipients read. */
  async invoke(call: Call, input: ListEnvelopeRecipientsInput): Promise<QueryAttempt<EnvelopeRecipients>> {
    const operation = 'listEnvelopeRecipients';
    if (!isGuid(input.envelopeId)) return newQueryBranch('defect', zeroRecipients, zeroReceipt(), failure(operation, 'VALIDATION', 'envelopeId must be a DocuSign envelope ID GUID, such as 00000000-0000-0000-0000-000000000000'));
    let session: DocuSignSession;
    try { session = await this.client.openSession(call, operation); }
    catch (error) { if (error instanceof SessionError) return sessionQueryFailure(error, zeroRecipients); throw error; }
    const envelopeId = input.envelopeId.toLowerCase();
    let response: DocuSignResponse;
    try { response = await this.client.requestAPI(call, session, 'GET', `/envelopes/${escapePath(input.envelopeId)}/recipients`); }
    catch (error) { return error instanceof ResponseTooLargeError ? newQueryBranch('invalidResponse', zeroRecipients, this.client.receipt(envelopeId), failure(operation, 'RESPONSE_TOO_LARGE', tooLarge)) : newQueryRetry(failure(operation, 'TRANSPORT', 'DocuSign could not be reached to list the recipients')); }
    if (response.status !== 200) return queryOutcome(this.client, this.client.classify(operation, response), envelopeId, zeroRecipients);
    try { return newQueryBranch('listed', decodeRecipients(response.body, envelopeId), this.client.receipt(envelopeId)); }
    catch { return newQueryBranch('invalidResponse', zeroRecipients, this.client.receipt(envelopeId), failure(operation, 'PROTOCOL', malformed)); }
  }
}

/** Void an envelope or read back a status refusal. */
export class VoidEnvelopeOperation implements Mutation<VoidEnvelopeInput, VoidedEnvelope> {
  /** Bind the authenticated DocuSign client. */
  constructor(private readonly client: DocuSignClient) {}
  /** Return manifest routes and Execute defaults. */
  definition() { return VoidEnvelopeDefinition; }
  /** Use the stable Call ID for this safely repeatable PUT. */
  idempotencyKey(callId: string): string { return callId; }
  /** Invoke one void attempt. */
  async invoke(call: Call, input: VoidEnvelopeInput): Promise<MutationAttempt<VoidedEnvelope>> {
    const operation = 'voidEnvelope';
    if (!isGuid(input.envelopeId) || !input.voidedReason?.trim() || [...input.voidedReason].length > 200) return newMutationBranch('defect', zeroVoided, zeroReceipt(), failure(operation, 'VALIDATION', !isGuid(input.envelopeId) ? 'envelopeId must be a DocuSign envelope ID GUID, such as 00000000-0000-0000-0000-000000000000' : 'voidedReason must be 1 to 200 characters of text'));
    const envelopeId = input.envelopeId.toLowerCase();
    let session: DocuSignSession;
    try { session = await this.client.openSession(call, operation); }
    catch (error) { if (error instanceof SessionError) return sessionMutationFailure(error, zeroVoided, 'providerRejected', 'invalidResponse'); throw error; }
    let response: DocuSignResponse;
    try { response = await this.client.requestAPI(call, session, 'PUT', `/envelopes/${escapePath(envelopeId)}`, undefined, { status: 'voided', voidedReason: input.voidedReason }); }
    catch (error) { if (error instanceof ResponseTooLargeError) return newMutationBranch('voided', { envelopeId }, this.client.receipt(envelopeId)); return newMutationRetry(failure(operation, 'TRANSPORT', 'DocuSign could not be reached to void the envelope; voiding again is safe')); }
    if (response.status === 200) return newMutationBranch('voided', { envelopeId }, this.client.receipt(envelopeId));
    const outcome = this.client.classify(operation, response);
    const receipt = this.client.receipt(envelopeId, outcome.errorCode);
    if (outcome.retry) return newMutationRetry(outcome.failure, outcome.retryAfterMs);
    if (outcome.notFound) return newMutationBranch('notFound', zeroVoided, receipt, outcome.failure);
    if (!['ENVELOPE_CANNOT_VOID_INVALID_STATE', 'ENVELOPE_INVALID_STATUS'].includes(outcome.errorCode.toUpperCase())) return newMutationBranch('providerRejected', zeroVoided, receipt, outcome.failure);
    const read = await readEnvelope(this.client, call, session, envelopeId, operation);
    if (read.error) return newMutationBranch('invalidResponse', zeroVoided, receipt, read.error);
    if (read.outcome) return read.outcome.retry ? newMutationRetry(read.outcome.failure, read.outcome.retryAfterMs) : newMutationBranch(read.outcome.notFound ? 'notFound' : 'providerRejected', zeroVoided, this.client.receipt(envelopeId, read.outcome.errorCode), read.outcome.failure);
    const envelope = read.envelope;
    if (!envelope) throw new Error('envelope read returned no value');
    if (envelope.status === 'voided') return newMutationBranch('voided', { envelopeId, wasAlreadyVoided: true, status: envelope.status, ...(envelope.voidedReason ? { voidedReason: envelope.voidedReason } : {}) }, receipt);
    if (['completed', 'declined', 'created'].includes(envelope.status)) return newMutationBranch('notVoidable', { envelopeId, status: envelope.status }, receipt, failure(operation, 'CONFLICT', `DocuSign does not void an envelope whose status is ${envelope.status}`));
    return newMutationBranch('providerRejected', { envelopeId: '', status: envelope.status }, receipt, outcome.failure);
  }
}

/** Download and digest a combined PDF without persisting its bytes in Dex. */
export class DownloadCombinedDocumentOperation implements Query<DownloadCombinedDocumentInput, CombinedDocument> {
  /** Bind the authenticated DocuSign client. */
  constructor(private readonly client: DocuSignClient) {}
  /** Return manifest routes and Execute defaults. */
  definition() { return DownloadCombinedDocumentDefinition; }
  /** Invoke one combined PDF read. */
  async invoke(call: Call, input: DownloadCombinedDocumentInput): Promise<QueryAttempt<CombinedDocument>> {
    const operation = 'downloadCombinedDocument';
    if (!isGuid(input.envelopeId)) return newQueryBranch('defect', zeroDocument, zeroReceipt(), failure(operation, 'VALIDATION', 'envelopeId must be a DocuSign envelope ID GUID, such as 00000000-0000-0000-0000-000000000000'));
    let session: DocuSignSession;
    try { session = await this.client.openSession(call, operation); }
    catch (error) { if (error instanceof SessionError) return sessionQueryFailure(error, zeroDocument); throw error; }
    const envelopeId = input.envelopeId.toLowerCase();
    let pendingHeartbeat = Promise.resolve();
    const heartbeat = setInterval(() => {
      pendingHeartbeat = pendingHeartbeat.then(() => call.context.recordHeartbeat(undefined)).catch(() => { /* A missed liveness heartbeat is repeated on the next interval. */ });
    }, 5_000);
    try {
      let response: Awaited<ReturnType<DocuSignClient['openDocument']>>;
      try { response = await this.client.openDocument(call, session, envelopeId, input.includeCertificate ?? false); }
      catch (error) { if (error instanceof ResponseTooLargeError) return newQueryBranch('tooLarge', zeroDocument, this.client.receipt(envelopeId), failure(operation, 'RESPONSE_TOO_LARGE', 'the combined PDF exceeds the configured maxDocumentBytes')); return newQueryRetry(failure(operation, 'TRANSPORT', 'DocuSign could not be reached to download the combined PDF')); }
      if ('rejected' in response) return queryOutcome(this.client, this.client.classify(operation, response.rejected), envelopeId, zeroDocument);
      try {
        const mediaType = String(response.headers['content-type'] ?? '').split(';', 1)[0]?.trim().toLowerCase();
        if (mediaType !== 'application/pdf') return newQueryBranch('invalidResponse', zeroDocument, this.client.receipt(envelopeId), failure(operation, 'PROTOCOL', 'DocuSign returned something other than a PDF for the combined document'));
        const lengthHeader = String(response.headers['content-length'] ?? '');
        const contentLength = /^[0-9]+$/.test(lengthHeader) ? Number(lengthHeader) : 0;
        if (Number.isSafeInteger(contentLength) && contentLength > this.client.maxDocumentBytes) return newQueryBranch('tooLarge', zeroDocument, this.client.receipt(envelopeId), failure(operation, 'RESPONSE_TOO_LARGE', 'the combined PDF exceeds the configured maxDocumentBytes'));
        const digest = createHash('sha256');
        let byteCount = 0; let isComplete = false;
        const maxBytes = this.client.maxDocumentBytes;
        const content = (async function* () {
          let prefix = Buffer.alloc(0);
          try {
            for await (const chunk of response.stream) {
              const buffer = Buffer.from(chunk);
              if (byteCount + buffer.length > maxBytes) throw new ResponseTooLargeError();
              byteCount += buffer.length;
              digest.update(buffer);
              if (prefix.length < 5) {
                prefix = Buffer.concat([prefix, buffer]);
                if (prefix.length < 5) continue;
                if (!prefix.subarray(0, 5).equals(Buffer.from('%PDF-'))) throw new InvalidPDFError();
                yield prefix;
              } else yield buffer;
            }
          } catch (error) {
            if (error instanceof ResponseTooLargeError || error instanceof InvalidPDFError) throw error;
            throw new DocumentUnreadError();
          }
          if (prefix.length < 5) throw new InvalidPDFError();
          isComplete = true;
        })();
        let storedLocation = '';
        if (this.client.documentStore) {
          try { storedLocation = await this.client.documentStore.storeCombinedDocument({ accountId: session.accountId, envelopeId, includesCertificate: input.includeCertificate ?? false }, content); }
          catch (error) { if (error instanceof ResponseTooLargeError || error instanceof InvalidPDFError || error instanceof DocumentUnreadError) throw error; return newQueryBranch('invalidResponse', zeroDocument, this.client.receipt(envelopeId), failure(operation, 'LOCAL_DEFECT', 'the CombinedDocumentStore did not store the combined PDF')); }
          if (!isComplete) return newQueryBranch('invalidResponse', zeroDocument, this.client.receipt(envelopeId), failure(operation, 'LOCAL_DEFECT', 'the CombinedDocumentStore returned before reading the whole combined PDF'));
          if (Buffer.byteLength(storedLocation) > 1024) return newQueryBranch('invalidResponse', zeroDocument, this.client.receipt(envelopeId), failure(operation, 'LOCAL_DEFECT', 'the CombinedDocumentStore did not store the combined PDF'));
        } else for await (const chunk of content) { void chunk; }
        const document: CombinedDocument = { envelopeId, contentType: 'application/pdf', byteCount, sha256: digest.digest('hex'), ...(input.includeCertificate ? { includesCertificate: true } : {}), ...(storedLocation ? { storedLocation } : {}) };
        return newQueryBranch('downloaded', document, this.client.receipt(envelopeId));
      } catch (error) {
        if (error instanceof ResponseTooLargeError) return newQueryBranch('tooLarge', zeroDocument, this.client.receipt(envelopeId), failure(operation, 'RESPONSE_TOO_LARGE', 'the combined PDF exceeds the configured maxDocumentBytes'));
        if (error instanceof InvalidPDFError) return newQueryBranch('invalidResponse', zeroDocument, this.client.receipt(envelopeId), failure(operation, 'PROTOCOL', 'DocuSign returned something other than a PDF for the combined document'));
        return newQueryRetry(failure(operation, 'TRANSPORT', 'the combined PDF download was interrupted'));
      } finally { await response.close(); }
    } finally { clearInterval(heartbeat); await pendingHeartbeat; }
  }
}

class InvalidPDFError extends Error {}
class DocumentUnreadError extends Error {}

function sessionQueryFailure<Output>(error: SessionError, zero: Output): QueryAttempt<Output> {
  if (error.route === 'retry') return newQueryRetry(error.failure, error.retryAfterMs);
  return newQueryBranch(error.route === 'rejected' ? 'providerRejected' : error.route, zero, zeroReceipt(), error.failure);
}
function sessionMutationFailure<Output>(error: SessionError, zero: Output, rejected: string, invalid: string): MutationAttempt<Output> {
  if (error.route === 'retry') return newMutationRetry(error.failure, error.retryAfterMs);
  return newMutationBranch(error.route === 'rejected' ? rejected : error.route === 'invalidResponse' ? invalid : 'defect', zero, zeroReceipt(), error.failure);
}
function queryOutcome<Output>(client: DocuSignClient, outcome: DocuSignOutcome, envelopeId: string, zero: Output): QueryAttempt<Output> {
  if (outcome.retry) return newQueryRetry(outcome.failure, outcome.retryAfterMs);
  return newQueryBranch(outcome.notFound ? 'notFound' : 'providerRejected', zero, client.receipt(envelopeId, outcome.errorCode), outcome.failure);
}
function errorMessage(error: unknown): string { return error instanceof Error ? error.message : 'invalid input'; }
function isProvablyNotSent(error: unknown): boolean { return error instanceof AddressRefusal || error instanceof Error && 'isProvablyNotSent' in error && error.isProvablyNotSent === true; }
async function clearCheckpoint(call: Call): Promise<void> { try { await call.context.recordHeartbeat(undefined); } catch { /* The previous marker keeps a later attempt from a duplicate POST. */ } }
async function recordCheckpoint(call: Call, checkpoint: { isDispatched: boolean; dispatchedAt: string }): Promise<void> { try { await call.context.recordHeartbeat(checkpoint); } catch { /* The previous checkpoint remains available for recovery. */ } }
function goTimestamp(value: Date): string { return value.toISOString().replace(/\.([0-9]*?)0*Z$/, (_match, fraction: string) => fraction ? `.${fraction}Z` : 'Z'); }
function readCreateCheckpoint(call: Call): { isDispatched: boolean; dispatchedAt: string } | undefined {
  try {
    if (!call.context.hasLastHeartbeatValue()) return undefined;
    const marker: unknown = call.context.getLastHeartbeatValue();
    if (marker === undefined || marker === null) return undefined;
    if (marker && typeof marker === 'object' && 'isDispatched' in marker && marker.isDispatched === false) return undefined;
    if (marker && typeof marker === 'object' && 'isDispatched' in marker && marker.isDispatched === true && 'dispatchedAt' in marker && typeof marker.dispatchedAt === 'string' && !Number.isNaN(new Date(marker.dispatchedAt).getTime())) return marker as { isDispatched: boolean; dispatchedAt: string };
    return { isDispatched: true, dispatchedAt: goTimestamp(call.context.firstAttemptAt) };
  } catch { return { isDispatched: true, dispatchedAt: goTimestamp(call.context.firstAttemptAt) }; }
}
function buildCreateBody(input: CreateEnvelopeFromTemplateInput, marker: string): Record<string, unknown> {
  if (!isGuid(input.templateId)) throw new Error('templateId must be a DocuSign template ID GUID, such as 00000000-0000-0000-0000-000000000000');
  if (!input.templateRoles || input.templateRoles.length < 1 || input.templateRoles.length > 100) throw new Error('templateRoles must fill 1 to 100 template roles');
  if ([...(input.emailSubject ?? '')].length > 100 || [...(input.emailBlurb ?? '')].length > 10000) throw new Error('emailSubject must be at most 100 characters and emailBlurb at most 10000');
  const names = new Set<string>();
  const roles = input.templateRoles.map((role, index) => {
    if (!role.roleName?.trim() || [...role.roleName].length > 100) throw new Error(`templateRoles[${index}]: roleName must be 1 to 100 characters`);
    if (!role.name?.trim() || [...role.name].length > 100) throw new Error(`templateRoles[${index}]: name must be 1 to 100 characters`);
    const at = role.email.indexOf('@');
    const domain = role.email.slice(at + 1);
    if (at <= 0 || !domain.includes('.') || domain.includes('@') || Buffer.byteLength(role.email) > 100 || /[ \t\r\n<>,;]/.test(role.email)) throw new Error(`templateRoles[${index}]: email must be one email address of at most 100 characters`);
    if ((role.routingOrder ?? 0) < 0 || (role.routingOrder ?? 0) > 999) throw new Error(`templateRoles[${index}]: routingOrder must be 1 to 999, or 0 to keep the template's order`);
    if (names.has(role.roleName.toLowerCase())) throw new Error(`templateRoles[${index}]: role ${JSON.stringify(role.roleName)} is filled twice`);
    names.add(role.roleName.toLowerCase());
    return { roleName: role.roleName, name: role.name, email: role.email, ...(role.routingOrder ? { routingOrder: String(role.routingOrder) } : {}) };
  });
  if ((input.customFields?.length ?? 0) > 10) throw new Error('customFields holds at most 10 fields');
  const fields = [{ name: 'dexIdempotencyKey', value: marker, show: 'false', required: 'false' }];
  const fieldNames = new Set(['dexidempotencykey']);
  for (const [index, field] of (input.customFields ?? []).entries()) {
    if (!field.name || [...field.name].length > 50 || [...field.value].length > 100) throw new Error(`customFields[${index}] needs a 1 to 50 character name and a value of at most 100 characters`);
    if (fieldNames.has(field.name.toLowerCase())) throw new Error(`customFields[${index}]: name ${JSON.stringify(field.name)} is reserved or repeated`);
    fieldNames.add(field.name.toLowerCase()); fields.push({ name: field.name, value: field.value, show: 'false', required: 'false' });
  }
  return { templateId: input.templateId.toLowerCase(), status: input.isDraft ? 'created' : 'sent', ...(input.emailSubject ? { emailSubject: input.emailSubject } : {}), ...(input.emailBlurb ? { emailBlurb: input.emailBlurb } : {}), templateRoles: roles, customFields: { textCustomFields: fields } };
}
function decodeCreated(body: Uint8Array, isDraft: boolean): CreatedEnvelope {
  const raw = parseObject(body);
  if (typeof raw.envelopeId !== 'string' || !isGuid(raw.envelopeId) || typeof raw.status !== 'string' || raw.status.toLowerCase() !== (isDraft ? 'created' : 'sent')) throw new Error(malformed);
  const statusDateTime = optionalString(raw.statusDateTime);
  return { envelopeId: raw.envelopeId.toLowerCase(), status: raw.status.toLowerCase(), ...(statusDateTime ? { statusChangedAt: parseDocuSignTimestamp(statusDateTime) } : {}) };
}
function parseObject(body: Uint8Array): Record<string, unknown> { const value: unknown = JSON.parse(Buffer.from(body).toString('utf8')); if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error(malformed); return value as Record<string, unknown>; }
function optionalString(value: unknown): string { if (value == null) return ''; if (typeof value !== 'string') throw new Error(malformed); return value; }
/** Parse DocuSign's RFC 3339 nanosecond timestamps with Go-compatible calendar validation. */
export function parseDocuSignTimestamp(value: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
  if (!match) throw new Error(malformed);
  const year = Number(match[1]); const month = Number(match[2]); const day = Number(match[3]);
  const hour = Number(match[4]); const minute = Number(match[5]); const second = Number(match[6]);
  const zone = match[8] ?? 'Z';
  if (month < 1 || month > 12 || day < 1 || day > 31 || hour > 23 || minute > 59 || second > 59 || (zone !== 'Z' && Number(zone.slice(4)) > 59)) throw new Error(malformed);
  const local = new Date(0); local.setUTCHours(0, 0, 0, 0); local.setUTCFullYear(year, month - 1, day); local.setUTCHours(hour, minute, second, 0);
  if (local.getUTCFullYear() !== year || local.getUTCMonth() !== month - 1 || local.getUTCDate() !== day) throw new Error(malformed);
  const offset = zone === 'Z' ? 0 : (zone[0] === '+' ? 1 : -1) * (Number(zone.slice(1, 3)) * 60 + Number(zone.slice(4, 6)));
  const utc = new Date(local.getTime() - offset * 60_000);
  const seconds = utc.toISOString().slice(0, 19);
  const fraction = (match[7] ?? '').slice(0, 9).replace(/0+$/, '');
  return `${seconds}${fraction ? `.${fraction}` : ''}Z`;
}
function decodeEnvelope(body: Uint8Array): Envelope {
  const raw = parseObject(body);
  const envelopeId = optionalString(raw.envelopeId); const status = optionalString(raw.status).toLowerCase();
  if (!isGuid(envelopeId) || !/^[a-z]{1,32}$/.test(status)) throw new Error(malformed);
  const result: Envelope = { envelopeId: envelopeId.toLowerCase(), status };
  for (const [source, target] of [['emailSubject', 'emailSubject'], ['voidedReason', 'voidedReason']] as const) { const value = optionalString(raw[source]); if (value) result[target] = value; }
  for (const [source, target] of [['createdDateTime', 'createdAt'], ['sentDateTime', 'sentAt'], ['deliveredDateTime', 'deliveredAt'], ['completedDateTime', 'completedAt'], ['declinedDateTime', 'declinedAt'], ['voidedDateTime', 'voidedAt'], ['statusChangedDateTime', 'statusChangedAt'], ['expireDateTime', 'expiresAt']] as const) { const value = optionalString(raw[source]); if (value) result[target] = parseDocuSignTimestamp(value); }
  const fields = raw.customFields;
  if (fields != null) {
    if (typeof fields !== 'object' || Array.isArray(fields)) throw new Error(malformed);
    const text = (fields as Record<string, unknown>).textCustomFields;
    if (text != null && !Array.isArray(text)) throw new Error(malformed);
    const customFields = (text ?? []).map((field: unknown) => { if (!field || typeof field !== 'object' || Array.isArray(field)) throw new Error(malformed); const item = field as Record<string, unknown>; return { name: optionalString(item.name), value: optionalString(item.value) }; }).filter(field => field.name);
    if (customFields.length) result.customFields = customFields;
  }
  return result;
}
async function readEnvelope(client: DocuSignClient, call: Call, session: DocuSignSession, envelopeId: string, operation: string): Promise<{ envelope?: Envelope; outcome?: DocuSignOutcome; error?: Failure }> {
  let response: DocuSignResponse;
  try { response = await client.requestAPI(call, session, 'GET', `/envelopes/${escapePath(envelopeId)}`, { include: 'custom_fields' }); }
  catch (error) { return error instanceof ResponseTooLargeError ? { error: failure(operation, 'RESPONSE_TOO_LARGE', tooLarge) } : { outcome: { retry: true, notFound: false, retryAfterMs: 0, errorCode: '', failure: failure(operation, 'TRANSPORT', 'DocuSign could not be reached to read the envelope') } }; }
  if (response.status !== 200) return { outcome: client.classify(operation, response) };
  try { const envelope = decodeEnvelope(response.body); if (envelope.envelopeId !== envelopeId.toLowerCase()) throw new Error(malformed); return { envelope }; }
  catch { return { error: failure(operation, 'PROTOCOL', malformed) }; }
}
function decodeRecipients(body: Uint8Array, envelopeId: string): EnvelopeRecipients {
  const raw = parseObject(body); const current = optionalString(raw.currentRoutingOrder);
  const order = parseRoutingOrder(current);
  const collections = [['signers', 'signer'], ['carbonCopies', 'carbonCopy'], ['certifiedDeliveries', 'certifiedDelivery'], ['inPersonSigners', 'inPersonSigner'], ['agents', 'agent'], ['editors', 'editor'], ['intermediaries', 'intermediary'], ['witnesses', 'witness']] as const;
  const recipients: Recipient[] = [];
  for (const [source, type] of collections) {
    const entries = raw[source]; if (entries != null && !Array.isArray(entries)) throw new Error(malformed);
    for (const entry of entries ?? []) {
      if (!entry || typeof entry !== 'object' || Array.isArray(entry)) throw new Error(malformed);
      const value = entry as Record<string, unknown>; const recipientId = optionalString(value.recipientId); const status = optionalString(value.status);
      const routing = optionalString(value.routingOrder); const routingOrder = parseRoutingOrder(routing);
      if (!recipientId || !status) throw new Error(malformed);
      const recipient: Recipient = { recipientId, type, routingOrder, status: status.toLowerCase() };
      for (const [from, to] of [['name', 'name'], ['email', 'email'], ['roleName', 'roleName'], ['declinedReason', 'declinedReason']] as const) { const text = optionalString(value[from]); if (text) recipient[to] = text; }
      for (const [from, to] of [['sentDateTime', 'sentAt'], ['deliveredDateTime', 'deliveredAt'], ['signedDateTime', 'signedAt'], ['declinedDateTime', 'declinedAt']] as const) { const text = optionalString(value[from]); if (text) recipient[to] = parseDocuSignTimestamp(text); }
      recipients.push(recipient);
    }
  }
  recipients.sort((a, b) => a.routingOrder - b.routingOrder || compareRecipientId(a.recipientId, b.recipientId));
  return { envelopeId, ...(order ? { currentRoutingOrder: order } : {}), recipients };
}
function parseRoutingOrder(value: string): number { if (!value) return 0; if (!/^[+-]?\d+$/.test(value)) throw new Error(malformed); const parsed = Number(value); if (!Number.isSafeInteger(parsed) || parsed < 0) throw new Error(malformed); return parsed; }
function compareRecipientId(a: string, b: string): number { const number = /^[+-]?\d+$/.test(a); const other = /^[+-]?\d+$/.test(b); return number && other ? Number(a) - Number(b) : number ? -1 : other ? 1 : a < b ? -1 : a > b ? 1 : 0; }
async function recoverCreated(client: DocuSignClient, call: Call, session: DocuSignSession, marker: string, checkpoint: { isDispatched: boolean; dispatchedAt: string }, retryable: boolean, unknown: Failure): Promise<MutationAttempt<CreatedEnvelope>> {
  const uncertain = newMutationUncertain(zeroCreated, unknown, client.receipt(''));
  const timeoutMs = recoveryRequestTimeout(call);
  if (timeoutMs === undefined) return uncertain;
  let response: DocuSignResponse;
  try { response = await listTagged(client, call, session, marker, checkpoint.dispatchedAt, timeoutMs); }
  catch (error) { if (retryable && !(error instanceof ResponseTooLargeError)) { await recordCheckpoint(call, checkpoint); return newMutationRetry(failure('createEnvelopeFromTemplate', 'AVAILABILITY', 'DocuSign could not list envelopes to find the one an earlier attempt may have created')); } return uncertain; }
  if (response.status !== 200) { const outcome = client.classify('createEnvelopeFromTemplate', response); if (retryable && outcome.retry) { await recordCheckpoint(call, checkpoint); return newMutationRetry(outcome.failure, outcome.retryAfterMs); } return uncertain; }
  try {
    const matches = findTagged(response.body, marker);
    if (matches.length !== 1) return uncertain;
    const recovered = matches[0]; if (!recovered) return uncertain;
    return newMutationBranch('created', { ...recovered, wasRecovered: true }, client.receipt(recovered.envelopeId));
  } catch { return uncertain; }
}
async function findBeforeResend(client: DocuSignClient, call: Call, session: DocuSignSession, marker: string): Promise<MutationAttempt<CreatedEnvelope> | undefined> {
  const timeoutMs = recoveryRequestTimeout(call);
  if (timeoutMs === undefined) return undefined;
  let response: DocuSignResponse;
  try { response = await listTagged(client, call, session, marker, call.context.firstAttemptAt.toISOString(), timeoutMs); }
  catch (error) { return error instanceof ResponseTooLargeError ? undefined : newMutationRetry(failure('createEnvelopeFromTemplate', 'AVAILABILITY', 'DocuSign could not list envelopes to find the one an earlier attempt may have created')); }
  if (response.status !== 200) { const outcome = client.classify('createEnvelopeFromTemplate', response); return outcome.retry ? newMutationRetry(outcome.failure, outcome.retryAfterMs) : undefined; }
  try {
    const matches = findTagged(response.body, marker);
    if (matches.length === 1 && matches[0]) return newMutationBranch('created', { ...matches[0], wasRecovered: true }, client.receipt(matches[0].envelopeId));
    if (matches.length > 1) return newMutationUncertain(zeroCreated, failure('createEnvelopeFromTemplate', 'CONFLICT', "several DocuSign envelopes carry this Step's dexIdempotencyKey, so none was adopted and none was sent"), client.receipt(''));
  } catch { /* A malformed optional read-back permits the first POST attempt. */ }
  return undefined;
}
function listTagged(client: DocuSignClient, call: Call, session: DocuSignSession, marker: string, since: string, timeoutMs: number): Promise<DocuSignResponse> {
  const from = new Date(new Date(since).getTime() - 3_600_000).toISOString().replace(/\.\d{3}Z$/, 'Z');
  return client.requestAPI(call, session, 'GET', '/envelopes', { from_date: from, custom_field: `dexIdempotencyKey=${marker}`, include: 'custom_fields' }, undefined, 'application/json', client.maxResponseBytes, timeoutMs);
}
function recoveryRequestTimeout(call: Call): number | undefined {
  const timeoutMs = call.context.deadline ? Math.min(25_000, call.context.deadline.getTime() - Date.now() - 2_000) : 25_000;
  return timeoutMs >= 2_000 ? timeoutMs : undefined;
}
function findTagged(body: Uint8Array, marker: string): CreatedEnvelope[] {
  const raw = parseObject(body); if (raw.envelopes != null && !Array.isArray(raw.envelopes)) throw new Error(malformed);
  return (raw.envelopes ?? []).map((entry: unknown) => decodeEnvelope(Buffer.from(JSON.stringify(entry)))).filter((envelope: Envelope) => envelope.customFields?.some(field => field.name === 'dexIdempotencyKey' && field.value === marker)).map((envelope: Envelope) => ({ envelopeId: envelope.envelopeId, status: envelope.status, ...(envelope.statusChangedAt ? { statusChangedAt: envelope.statusChangedAt } : {}) }));
}
