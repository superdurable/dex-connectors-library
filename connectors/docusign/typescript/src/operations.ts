// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { AddressRefusal, ProviderStalledError, ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { definitionMaxSends, newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
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
    const { checkpoint: earlier, sends } = readCreateCheckpoint(call);
    const isPossiblySent = earlier !== undefined || sends.hasPossibleEarlierSend;
    const recordUnsent = () => recordCheckpoint(call, unsentCheckpoint(call, earlier, sends, this.client.currentTime()));
    let session: DocuSignSession;
    try { session = await this.client.openSession(call, operation); }
    catch (error) {
      if (!(error instanceof SessionError)) throw error;
      if (error.route === 'retry') { await recordUnsent(); return newMutationRetry(error.failure, error.retryAfterMs); }
      if (!isPossiblySent) return sessionMutationFailure(error, zeroCreated, 'providerRejected', 'providerRejected');
      // A terminal failure after a possible send selects uncertain, never a failure branch.
      const unknown = { ...error.failure, message: `an earlier attempt may have sent the DocuSign create request and its outcome is unknown; ${error.failure.message}` };
      return newMutationUncertain(zeroCreated, unknown, this.client.receipt(''));
    }
    if (isPossiblySent) {
      const decided = await findBeforeResend(this.client, call, session, marker, earlier, sends, recordUnsent);
      if (decided) return decided;
    }
    const checkpoint = dispatchCheckpoint(call, earlier, sends, this.client.currentTime());
    try { await call.context.recordHeartbeat(checkpoint); }
    catch { return newMutationRetry(failure(operation, 'AVAILABILITY', 'the create checkpoint could not be recorded, so nothing was sent to DocuSign')); }
    let response: DocuSignResponse;
    try { response = await this.client.requestAPI(call, session, 'POST', '/envelopes', undefined, body, 'application/json', this.client.maxResponseBytes, 40_000); }
    catch (error) {
      if (error instanceof ResponseTooLargeError) return recoverAccepted(this.client, call, session, marker, checkpoint, sends, failure(operation, 'RESPONSE_TOO_LARGE', 'DocuSign accepted the create request but its answer could not be read'));
      if (isAcceptedBeforeFailure(error)) return recoverAccepted(this.client, call, session, marker, checkpoint, sends, failure(operation, 'PROTOCOL', 'DocuSign accepted the create request but its answer could not be read'));
      if (isProvablyNotSent(error)) { await recordUnsent(); return newMutationRetry(failure(operation, 'TRANSPORT', 'DocuSign could not be reached, so no envelope was created')); }
      return recoverCreated(this.client, call, session, marker, checkpoint, sends.canResendLater, failure(operation, 'TRANSPORT', "DocuSign's answer to the create request was lost"));
    }
    if (response.status >= 200 && response.status < 300) {
      if (response.status !== 200 && response.status !== 201) return recoverAccepted(this.client, call, session, marker, checkpoint, sends, failure(operation, 'PROTOCOL', `DocuSign accepted the create request with HTTP ${response.status} instead of 201`));
      try { const created = decodeCreated(response.body, input.isDraft ?? false); return newMutationBranch('created', created, this.client.receipt(created.envelopeId)); }
      catch { return recoverAccepted(this.client, call, session, marker, checkpoint, sends, failure(operation, 'PROTOCOL', 'DocuSign accepted the create request but returned an unusable envelope summary')); }
    }
    const outcome = this.client.classify(operation, response);
    // DocuSign answers a rate limit or 503 before it processes the request.
    if (response.status === 503 || (outcome.retry && response.status < 500 && response.status !== 408)) { await recordUnsent(); return newMutationRetry(outcome.failure, outcome.retryAfterMs); }
    // This send was refused, but an earlier attempt's create may still exist.
    const isDefiniteRefusal = isDefiniteCreateRefusal(response);
    if (isDefiniteRefusal && isPossiblySent) return newMutationUncertain(zeroCreated, outcome.failure, this.client.receipt('', outcome.errorCode));
    if (isDefiniteRefusal) { await recordUnsent(); return newMutationBranch('providerRejected', zeroCreated, this.client.receipt('', outcome.errorCode), outcome.failure); }
    return recoverCreated(this.client, call, session, marker, checkpoint, sends.canResendLater, outcome.failure);
  }
}

const definiteRefusalStatuses = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);

/** Report a refusal status with a complete body that names no created envelope. */
function isDefiniteCreateRefusal(response: DocuSignResponse): boolean {
  return definiteRefusalStatuses.has(response.status) && response.isErrorBodyIncomplete !== true && !hasEnvelopeId(response.body);
}

/** Report a body whose envelopeId is a GUID, which means DocuSign created the envelope. */
function hasEnvelopeId(body: Uint8Array): boolean {
  let decoded: unknown;
  try { decoded = JSON.parse(Buffer.from(body).toString('utf8')); }
  catch { return false; }
  const envelopeId = typeof decoded === 'object' && decoded !== null ? (decoded as Record<string, unknown>).envelopeId : undefined;
  return typeof envelopeId === 'string' && isGuid(envelopeId);
}

/** The create heartbeat; attempt 0 marks a checkpoint from an earlier release, which has no exact send count. */
interface CreateCheckpoint { isDispatched: boolean; dispatchedAt: string; attempt: number; sendCount: number; isAccepted: boolean }

/** Possible earlier creates: a checkpoint's send count plus the attempts after it, else attempt - 1. */
interface CreateSendBudget { attempt: number; priorSends: number; isAccepted: boolean; hasPossibleEarlierSend: boolean; canSendNow: boolean; canResendLater: boolean; canReadBackLater: boolean }

function createSendBudget(call: Call, priorSends: number, isAccepted: boolean): CreateSendBudget {
  const operationMaxSends = definitionMaxSends(CreateEnvelopeFromTemplateDefinition);
  const maxSends = operationMaxSends >= 2 ? call.maxSendsOverride ?? operationMaxSends : 1;
  const attempt = call.context.attempt;
  // Read-back attempts send nothing, so the attempt number bounds them.
  return { attempt, priorSends, isAccepted, hasPossibleEarlierSend: priorSends > 0, canSendNow: !isAccepted && priorSends < maxSends, canResendLater: !isAccepted && priorSends + 1 < maxSends, canReadBackLater: attempt < maxSends };
}

/** Return the checkpoint recorded before this attempt sends, keeping the earliest dispatch time. */
function dispatchCheckpoint(call: Call, earlier: CreateCheckpoint | undefined, sends: CreateSendBudget, now: Date): CreateCheckpoint {
  const dispatchedAt = earlier?.dispatchedAt ?? goTimestamp(sends.hasPossibleEarlierSend ? call.context.firstAttemptAt : now);
  return { isDispatched: true, dispatchedAt, attempt: sends.attempt, sendCount: sends.priorSends + 1, isAccepted: false };
}

/** Return the checkpoint of an attempt that provably sent nothing, so a later attempt does not count it. */
function unsentCheckpoint(call: Call, earlier: CreateCheckpoint | undefined, sends: CreateSendBudget, now: Date): CreateCheckpoint {
  return { ...dispatchCheckpoint(call, earlier, sends, now), isDispatched: earlier !== undefined || sends.hasPossibleEarlierSend, sendCount: sends.priorSends, isAccepted: sends.isAccepted };
}

function resendLaterOrSelectUncertain(client: DocuSignClient, canResendLater: boolean, unknown: Failure): MutationAttempt<CreatedEnvelope> {
  // The checkpoint stays the last heartbeat, so the next attempt reads back before it sends.
  return canResendLater ? newMutationRetry(unknown) : newMutationUncertain(zeroCreated, unknown, client.receipt(''));
}

/** Let a later attempt read an accepted create back, never resend it. */
function readBackLaterOrSelectUncertain(client: DocuSignClient, canReadBackLater: boolean, unknown: Failure): MutationAttempt<CreatedEnvelope> {
  return canReadBackLater ? newMutationRetry(unknown) : newMutationUncertain(zeroCreated, unknown, client.receipt(''));
}

/** Report a transport failure after 2xx response headers, which means DocuSign accepted the create. */
function isAcceptedBeforeFailure(error: unknown): boolean {
  const status = error instanceof ProviderTransportError || error instanceof ProviderStalledError ? error.status : undefined;
  return status !== undefined && status >= 200 && status < 300;
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
    // A 3xx, 409, other unlisted status, or an incomplete error body leaves the void unknown; voiding again is safe.
    if (outcome.retry || !definiteRefusalStatuses.has(response.status) || response.isErrorBodyIncomplete === true) return newMutationRetry(outcome.failure, outcome.retryAfterMs);
    if (outcome.notFound) return newMutationBranch('notFound', zeroVoided, receipt, outcome.failure);
    if (!['ENVELOPE_CANNOT_VOID_INVALID_STATE', 'ENVELOPE_INVALID_STATUS'].includes(outcome.errorCode.toUpperCase())) return newMutationBranch('providerRejected', zeroVoided, receipt, outcome.failure);
    const read = await readEnvelope(this.client, call, session, envelopeId, operation);
    // The status read-back is inconclusive, and voiding again is safe.
    if (read.error) return newMutationRetry(read.error);
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
async function recordCheckpoint(call: Call, checkpoint: CreateCheckpoint): Promise<void> { try { await call.context.recordHeartbeat(checkpoint); } catch { /* The previous checkpoint remains available for recovery. */ } }
function goTimestamp(value: Date): string { return value.toISOString().replace(/\.([0-9]*?)0*Z$/, (_match, fraction: string) => fraction ? `.${fraction}Z` : 'Z'); }
/** Return an earlier dispatch checkpoint and the send budget; an unreadable checkpoint counts as dispatched. */
function readCreateCheckpoint(call: Call): { checkpoint: CreateCheckpoint | undefined; sends: CreateSendBudget } {
  const attempt = call.context.attempt;
  const fallback = createSendBudget(call, Math.max(attempt - 1, 0), false);
  const unreadable = { checkpoint: { isDispatched: true, dispatchedAt: goTimestamp(call.context.firstAttemptAt), attempt: 0, sendCount: 0, isAccepted: false }, sends: fallback };
  let value: unknown;
  try {
    if (!call.context.hasLastHeartbeatValue()) return { checkpoint: undefined, sends: fallback };
    value = call.context.getLastHeartbeatValue();
  } catch { return unreadable; }
  if (value === undefined || value === null) return { checkpoint: undefined, sends: fallback };
  if (typeof value !== 'object' || Array.isArray(value)) return unreadable;
  const raw = value as Record<string, unknown>;
  if (typeof raw.isDispatched !== 'boolean') return unreadable;
  if (raw.isDispatched && (typeof raw.dispatchedAt !== 'string' || Number.isNaN(new Date(raw.dispatchedAt).getTime()))) return unreadable;
  const checkpoint: CreateCheckpoint = {
    isDispatched: raw.isDispatched, dispatchedAt: typeof raw.dispatchedAt === 'string' ? raw.dispatchedAt : '',
    attempt: Number.isSafeInteger(raw.attempt) ? raw.attempt as number : 0, sendCount: Number.isSafeInteger(raw.sendCount) ? raw.sendCount as number : 0, isAccepted: raw.isAccepted === true,
  };
  const hasExactSendCount = checkpoint.attempt >= 1 && checkpoint.attempt < attempt && checkpoint.sendCount >= 0 && checkpoint.sendCount <= checkpoint.attempt && (checkpoint.isDispatched || checkpoint.sendCount === 0);
  const priorSends = hasExactSendCount ? checkpoint.sendCount + attempt - 1 - checkpoint.attempt : fallback.priorSends;
  return { checkpoint: checkpoint.isDispatched ? checkpoint : undefined, sends: createSendBudget(call, priorSends, checkpoint.isAccepted) };
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
/** Read back after this attempt's unknown send; a miss resends on a later attempt below the cap. */
async function recoverCreated(client: DocuSignClient, call: Call, session: DocuSignSession, marker: string, checkpoint: CreateCheckpoint, canResendLater: boolean, unknown: Failure): Promise<MutationAttempt<CreatedEnvelope>> {
  // Listings can lag, so a miss is not proof; the next attempt reads back again before it sends.
  return await findTaggedEnvelope(client, call, session, marker, checkpoint.dispatchedAt) ?? resendLaterOrSelectUncertain(client, canResendLater, unknown);
}
/** Mark a 2xx create accepted, so no attempt sends it again, and read it back. When the accepted marker could not be recorded, a later attempt could resend, so a miss selects uncertain. */
async function recoverAccepted(client: DocuSignClient, call: Call, session: DocuSignSession, marker: string, checkpoint: CreateCheckpoint, sends: CreateSendBudget, unknown: Failure): Promise<MutationAttempt<CreatedEnvelope>> {
  let isAcceptedMarkerRecorded = true;
  try { await call.context.recordHeartbeat({ ...checkpoint, isAccepted: true }); }
  catch { isAcceptedMarkerRecorded = false; }
  return await findTaggedEnvelope(client, call, session, marker, checkpoint.dispatchedAt) ?? readBackLaterOrSelectUncertain(client, isAcceptedMarkerRecorded && sends.canReadBackLater, unknown);
}
/** Adopt exactly one tagged envelope or select uncertain for several; undefined is inconclusive. */
async function findTaggedEnvelope(client: DocuSignClient, call: Call, session: DocuSignSession, marker: string, since: string): Promise<MutationAttempt<CreatedEnvelope> | undefined> {
  const timeoutMs = recoveryRequestTimeout(call);
  if (timeoutMs === undefined) return undefined;
  let response: DocuSignResponse;
  try { response = await listTagged(client, call, session, marker, since, timeoutMs); }
  catch { return undefined; }
  if (response.status !== 200) return undefined;
  try {
    const matches = findTagged(response.body, marker);
    if (matches.length === 1 && matches[0]) return newMutationBranch('created', { ...matches[0], wasRecovered: true }, client.receipt(matches[0].envelopeId));
    if (matches.length > 1) return duplicateEnvelopesUncertain(client);
  } catch { /* An unreadable listing is no proof either way. */ }
  return undefined;
}
/** Read back before a later attempt sends; undefined lets it send below the cap. After an accepted create it never sends. */
async function findBeforeResend(client: DocuSignClient, call: Call, session: DocuSignSession, marker: string, prior: CreateCheckpoint | undefined, sends: CreateSendBudget, recordUnsent: () => Promise<void>): Promise<MutationAttempt<CreatedEnvelope> | undefined> {
  const unknown = failure('createEnvelopeFromTemplate', 'TRANSPORT', sends.isAccepted ? "DocuSign accepted an earlier attempt's create request, but the envelope could not be found yet" : 'an earlier attempt may have sent the DocuSign create request and its outcome is unknown');
  const canRetryListing = !sends.isAccepted || sends.canReadBackLater;
  const timeoutMs = recoveryRequestTimeout(call);
  if (timeoutMs !== undefined) {
    let response: DocuSignResponse | undefined;
    try { response = await listTagged(client, call, session, marker, prior?.dispatchedAt ?? call.context.firstAttemptAt.toISOString(), timeoutMs); }
    catch (error) {
      if (!(error instanceof ResponseTooLargeError) && canRetryListing) {
        await recordUnsent();
        return newMutationRetry(failure('createEnvelopeFromTemplate', 'AVAILABILITY', 'DocuSign could not list envelopes to find the one an earlier attempt may have created'));
      }
    }
    if (response && response.status !== 200) {
      const outcome = client.classify('createEnvelopeFromTemplate', response);
      if (outcome.retry && canRetryListing) { await recordUnsent(); return newMutationRetry(outcome.failure, outcome.retryAfterMs); }
    } else if (response) {
      try {
        const matches = findTagged(response.body, marker);
        if (matches.length === 1 && matches[0]) return newMutationBranch('created', { ...matches[0], wasRecovered: true }, client.receipt(matches[0].envelopeId));
        if (matches.length > 1) return duplicateEnvelopesUncertain(client);
      } catch { /* A malformed read-back is inconclusive, so the cap decides whether to send. */ }
    }
  }
  if (sends.canSendNow) return undefined;
  if (sends.isAccepted && sends.canReadBackLater) { await recordUnsent(); return newMutationRetry(unknown); }
  if (prior) await recordCheckpoint(call, prior);
  return newMutationUncertain(zeroCreated, unknown, client.receipt(''));
}
/** Several tagged envelopes are duplicates already present: adopt none and send no more. */
function duplicateEnvelopesUncertain(client: DocuSignClient): MutationAttempt<CreatedEnvelope> {
  return newMutationUncertain(zeroCreated, failure('createEnvelopeFromTemplate', 'CONFLICT', "several DocuSign envelopes carry this Step's dexIdempotencyKey, so none was adopted and none was sent"), client.receipt(''));
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
