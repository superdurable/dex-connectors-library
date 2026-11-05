// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError, RetryError, resolveCredential, resolveCredentialAfterRejection, type Call, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, readErrorTokens, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { AddUpdateOperation, CreateItemOperation, GetItemOperation, ListItemsOperation, UpdateItemColumnValuesOperation } from './operations.js';

const defaultAPIURL = 'https://api.monday.com/v2';
const defaultMaxResponseBytes = 4_194_304;
const defaultTimeoutMs = 12_000;
const token = (value: unknown): string => typeof value === 'string' || typeof value === 'number' ? String(value) : '';
const headerValue = (value: unknown): string => Array.isArray(value) ? token(value[0]) : token(value);
const codes = (values: string[]) => new Set(values);
const rateCodes = codes(['COMPLEXITY_BUDGET_EXHAUSTED', 'ComplexityException', 'RATE_LIMIT_EXCEEDED', 'IP_RATE_LIMIT_EXCEEDED', 'maxConcurrencyExceeded', 'FIELD_LIMIT_EXCEEDED']);
const unavailableCodes = codes(['API_TEMPORARILY_BLOCKED']);
const authCodes = codes(['Unauthorized', 'UNAUTHORIZED', 'NOT_AUTHENTICATED']);
const permissionCodes = codes(['UserUnauthorizedException', 'USER_UNAUTHORIZED', 'USER_ACCESS_DENIED', 'missingRequiredPermissions']);
const missingCodes = codes(['ResourceNotFoundException', 'InvalidBoardIdException', 'InvalidItemIdException', 'InvalidGroupIdException']);
const documentCodes = codes(['JsonParseException', 'InvalidVersionException', 'GRAPHQL_PARSE_FAILED', 'GRAPHQL_VALIDATION_FAILED', 'undefinedField', 'argumentLiteralsIncompatible', 'missingRequiredArguments', 'variableMismatch']);
const validationCodes = codes(['ColumnValueException', 'CorrectedValueException', 'InvalidColumnIdException', 'InvalidUserIdException', 'InvalidArgumentException', 'ItemNameTooLongException', 'RecordInvalidException', 'ItemsLimitationException']);

/** Injectable dependencies for a monday.com compatible provider and deterministic receipts. */
export interface MondayClientOptions {
  /** Replace the provider sender for tests. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Replace the provider API URL for a loopback stand-in. */ apiURL?: string;
  /** Replace the receipt and Retry-After clock. */ now?: () => Date;
}

/** One GraphQL document and the variables sent with it. */
export interface MondayRequest { document: string; variables: Record<string, unknown>; isMutation?: boolean; requiresCheckpoint?: boolean; }
/** Classified provider exchange, including safe response metadata. */
export interface MondayExchange {
  outcome: 'succeeded' | 'notFound' | 'retry' | 'uncertain' | 'rejected' | 'invalid' | 'defect';
  status: number; data?: unknown; requestID: string; isReplayed: boolean; failure?: Failure; retryAfterMs: number; confirmedNotProcessed: boolean;
}

/** Authenticated monday.com GraphQL client with manifest driven credential refresh. */
export class MondayClient {
  private readonly apiURL: string;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'monday.com', auth: Authorization, credentialFields: CredentialFields });

  /** Validate configuration and retain the host credential source. */
  constructor(config: Config, private readonly credentials: CredentialSource, options: MondayClientOptions = {}) {
    if (!credentials) throw new TypeError('monday.com credential provider is required');
    this.apiURL = validateBaseURL(options.apiURL || defaultAPIURL);
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('monday.com response limit must be positive');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the listItems Query. */ listItems(): ListItemsOperation { return new ListItemsOperation(this); }
  /** Bind the getItem Query. */ getItem(): GetItemOperation { return new GetItemOperation(this); }
  /** Bind the createItem Mutation. */ createItem(): CreateItemOperation { return new CreateItemOperation(this); }
  /** Bind the updateItemColumnValues Mutation. */ updateItemColumnValues(): UpdateItemColumnValuesOperation { return new UpdateItemColumnValuesOperation(this); }
  /** Bind the addUpdate Mutation. */ addUpdate(): AddUpdateOperation { return new AddUpdateOperation(this); }

  /** Make one authenticated exchange, recording the same resend checkpoint as Go for irreversible writes. */
  async exchange(call: Call, operation: string, request: MondayRequest): Promise<MondayExchange> {
    const attempt = call.context.attempt;
    const checkpoint = { callId: call.id, attempt };
    if (request.requiresCheckpoint) {
      try {
        const hasPrevious = call.context.hasLastHeartbeatValue();
        const previous = hasPrevious ? call.context.getLastHeartbeatValue() as Record<string, unknown> : undefined;
        if (previous && previous.callId !== call.id) return failureExchange(operation, attempt > 1 ? 'uncertain' : 'defect', attempt > 1 ? 'TRANSPORT' : 'LOCAL_DEFECT', 'mutation checkpoint is invalid');
        if (!(attempt === 1 && !hasPrevious || previous?.canResend === true && previous.attempt === attempt - 1)) return failureExchange(operation, 'uncertain', 'TRANSPORT', 'an earlier attempt may have applied the mutation');
      } catch { return failureExchange(operation, attempt > 1 ? 'uncertain' : 'defect', attempt > 1 ? 'TRANSPORT' : 'LOCAL_DEFECT', 'mutation checkpoint is invalid'); }
    }
    let credentials: Credentials;
    try { credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal); }
    catch (error) {
      const result = error instanceof ReauthorizationRequiredError
        ? failureExchange(operation, 'rejected', 'AUTHENTICATION', 'monday.com authorization must be renewed')
        : error instanceof RetryError ? failureExchange(operation, 'retry', 'AVAILABILITY', 'monday.com OAuth token refresh is temporarily unavailable')
          : failureExchange(operation, 'defect', 'AUTHENTICATION', 'monday.com connection credentials are unavailable');
      if (request.requiresCheckpoint && result.outcome === 'retry') await this.recordCanResend(call);
      return result;
    }
    const accessToken = credentials.authMethodId === 'monday-oauth' ? credentials.accessToken?.reveal() : credentials.apiToken?.reveal();
    if (!['personal-api-token', 'monday-oauth'].includes(credentials.authMethodId ?? '') || !accessToken || !isHeaderSafeCredential(accessToken)) {
      return failureExchange(operation, 'defect', 'AUTHENTICATION', 'monday.com connection credentials are unavailable');
    }
    if (request.requiresCheckpoint) {
      if (call.context.deadline && call.context.deadline.getTime() - Date.now() < 6_000) {
        await this.recordCanResend(call);
        return failureExchange(operation, 'retry', 'AVAILABILITY', 'not enough time remains to send the mutation');
      }
      try { await call.context.recordHeartbeat(checkpoint); }
      catch { await this.recordCanResend(call); return failureExchange(operation, 'retry', 'AVAILABILITY', 'dispatch checkpoint could not be recorded'); }
    }
    let result = await this.sendOnce(call, operation, request, accessToken);
    if (result.status === 401 && credentials.authMethodId === 'monday-oauth' && 'resolveAfterRejection' in this.credentials) {
      try {
        const replacement = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
        const replacementToken = replacement.accessToken?.reveal();
        if (replacementToken && isHeaderSafeCredential(replacementToken)) result = await this.sendOnce(call, operation, request, replacementToken);
      } catch { /* Go retains the original HTTP 401 when coordinated refresh fails. */ }
    }
    if (request.requiresCheckpoint && result.outcome === 'retry') {
      if (result.status >= 200 && result.status < 300 && (!result.confirmedNotProcessed || result.data !== undefined)) result = { ...result, outcome: 'uncertain' };
      else if (result.data === undefined) await this.recordCanResend(call);
    }
    return result;
  }

  /** Build a secret free receipt for one response. */
  receipt(call: Call, result: MondayExchange, objectID: string): ReceiptDraft {
    return {
      callId: call.id, ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), provider: 'monday',
      ...(objectID ? { providerObjectId: objectID } : {}), ...(result.requestID ? { providerRequestId: result.requestID } : {}),
      observedAt: this.now().toISOString(), ...(result.isReplayed ? { metadata: { idempotencyReplayed: 'true' } } : {}),
    };
  }

  private async recordCanResend(call: Call): Promise<void> {
    try { await call.context.recordHeartbeat({ callId: call.id, attempt: call.context.attempt, canResend: true }); }
    catch { /* Go also ignores a best effort resend checkpoint error. */ }
  }

  private async sendOnce(call: Call, operation: string, request: MondayRequest, accessToken: string): Promise<MondayExchange> {
    if (request.isMutation && !call.idempotencyKey) return failureExchange(operation, 'defect', 'LOCAL_DEFECT', 'monday.com mutation has no idempotency key');
    const timeoutMs = Math.min(defaultTimeoutMs, call.context.deadline ? call.context.deadline.getTime() - Date.now() - 3_000 : defaultTimeoutMs);
    if (timeoutMs <= 0) return failureExchange(operation, 'retry', 'TRANSPORT', 'monday.com request context expired before dispatch');
    const providerRequest: ProviderRequest = {
      url: this.apiURL, method: 'POST', headers: {
        Accept: 'application/json', 'Content-Type': 'application/json', Authorization: accessToken, 'API-Version': '2026-10',
        ...(request.isMutation ? { 'Idempotency-Key': call.idempotencyKey! } : {}),
      }, body: Buffer.from(JSON.stringify({ query: request.document, variables: request.variables })),
      maxResponseBytes: this.maxResponseBytes, maxErrorResponseBytes: this.maxResponseBytes, ignoreErrorBodyReadFailure: true,
      addressPolicy: this.apiURL === defaultAPIURL ? 'publicHTTPS' : 'privateNetworkHTTP', timeoutMs, signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.send(providerRequest); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined && error.headers !== undefined) {
        response = { status: error.status, headers: error.headers, body: new Uint8Array() };
        if (response.status >= 200 && response.status < 300) return failureExchange(operation, 'invalid', 'RESPONSE_TOO_LARGE', 'monday.com response exceeds the configured maxResponseBytes limit', response.status, '', headerValue(response.headers['idempotency-replayed']).trim().toLowerCase() === 'true');
      } else if (error instanceof ProviderTransportError && error.isProvablyNotSent) return failureExchange(operation, 'retry', 'TRANSPORT', 'monday.com could not be reached; no request was sent');
      else return failureExchange(operation, request.isMutation && operation !== 'updateItemColumnValues' ? 'uncertain' : 'retry', 'TRANSPORT', 'monday.com request failed before a response arrived');
    }
    const body = response.body;
    const status = response.status;
    const isReplayed = headerValue(response.headers['idempotency-replayed']).trim().toLowerCase() === 'true';
    if (Buffer.from(body).includes(Buffer.from(accessToken))) return failureExchange(operation, 'invalid', 'PROTOCOL', 'monday.com response reflected the connection credential', status, '', isReplayed);
    const requestID = readErrorTokens(body, ['/extensions/request_id'])[0] ?? '';
    const safeRequestID = /^[A-Za-z0-9._:-]{1,128}$/.test(requestID) ? requestID : '';
    const base = { status, requestID: safeRequestID, isReplayed };
    const retryAfterMs = parseRetryAfter(headerValue(response.headers['retry-after']), this.now());
    let envelope: Record<string, unknown> | undefined;
    try { const parsed: unknown = JSON.parse(Buffer.from(body).toString()); if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) envelope = parsed as Record<string, unknown>; }
    catch { /* Status alone classifies non-success replies. */ }
    const data = envelope?.data === null ? undefined : envelope?.data;
    if (status < 200 || status >= 300) return classifyFailure(operation, body, status, accessToken, retryAfterMs, base, data);
    if (!envelope) return { ...failureExchange(operation, 'invalid', 'PROTOCOL', 'monday.com returned a response that is not a GraphQL JSON object', status, safeRequestID, isReplayed), retryAfterMs };
    if (envelope.errors !== undefined && envelope.errors !== null && !Array.isArray(envelope.errors)) return failureExchange(operation, 'invalid', 'PROTOCOL', 'monday.com returned a response that is not a GraphQL JSON object', status, safeRequestID, isReplayed);
    if (Array.isArray(envelope.errors) && envelope.errors.length) return classifyFailure(operation, body, status, accessToken, retryAfterMs, base, data);
    if (data === undefined) return failureExchange(operation, 'invalid', 'PROTOCOL', 'monday.com returned neither data nor errors', status, safeRequestID, isReplayed);
    return { outcome: 'succeeded', ...base, data, retryAfterMs: 0, confirmedNotProcessed: false };
  }
}

function failureExchange(operation: string, outcome: MondayExchange['outcome'], kind: Failure['kind'], message: string, status = 0, requestID = '', isReplayed = false): MondayExchange {
  return { outcome, status, requestID, isReplayed, failure: { kind, provider: 'monday', operation, message }, retryAfterMs: 0, confirmedNotProcessed: false };
}

function classifyFailure(operation: string, body: Uint8Array, status: number, secret: string, retryAfterMs: number, base: { status: number; requestID: string; isReplayed: boolean }, data: unknown): MondayExchange {
  const codeValue = readErrorTokens(body, ['/errors/0/extensions/code', '/error_code'])[0] ?? '';
  const code = !codeValue.includes(secret) ? codeValue : '';
  const statusCode = Number(readErrorTokens(body, ['/errors/0/extensions/status_code'])[0] ?? 0);
  const classifiedStatus = status >= 200 && status < 300 && statusCode >= 400 ? statusCode : status;
  const delay = Number(readErrorTokens(body, ['/errors/0/extensions/retry_in_seconds'])[0] ?? 0);
  if (delay > 0 && delay <= 3600) retryAfterMs = Math.max(retryAfterMs, Math.ceil(delay) * 1000);
  const columnIDValue = readErrorTokens(body, ['/errors/0/extensions/error_data/column_id'])[0] ?? '';
  const columnID = !columnIDValue.includes(secret) ? columnIDValue : '';
  const describe = (verb: string) => `monday.com ${verb} (HTTP ${status})${code || columnID ? ` [${[code, columnID ? `column ${columnID}` : ''].filter(Boolean).join('; ')}]` : ''}`;
  const confirmedNotProcessed = data === undefined && (rateCodes.has(code) || code === 'IDEMPOTENCY_CONFLICT' || unavailableCodes.has(code) || [429, 409, 423, 503].includes(classifiedStatus));
  const outcome = (disposition: MondayExchange['outcome'], kind: Failure['kind'], message: string): MondayExchange => ({ outcome: disposition, ...base, ...(data === undefined ? {} : { data }), failure: { kind, provider: 'monday', operation, message }, retryAfterMs, confirmedNotProcessed });
  if (code === 'DAILY_LIMIT_EXCEEDED') return outcome('rejected', 'QUOTA_EXHAUSTED', describe('daily call limit is exhausted until midnight UTC'));
  if (rateCodes.has(code)) return outcome('retry', 'RATE_LIMIT', describe('rate limited the request'));
  if (code === 'IDEMPOTENCY_CONFLICT') { retryAfterMs = Math.max(retryAfterMs, 2000); return outcome('retry', 'CONFLICT', describe('is still processing an earlier attempt with this idempotency key')); }
  if (unavailableCodes.has(code)) return outcome('retry', 'AVAILABILITY', describe('temporarily refused the request'));
  if (authCodes.has(code)) return outcome('rejected', 'AUTHENTICATION', describe('rejected the credential'));
  if (permissionCodes.has(code)) return outcome('rejected', 'AUTHORIZATION', describe('denied permission'));
  if (missingCodes.has(code)) return outcome('notFound', 'NOT_FOUND', describe('found no such board, group, or item'));
  if (validationCodes.has(code)) return outcome('rejected', 'VALIDATION', describe('rejected the input'));
  if (documentCodes.has(code)) return outcome('defect', 'PROTOCOL', describe("rejected the connector's GraphQL request"));
  if (classifiedStatus === 429) return outcome('retry', 'RATE_LIMIT', describe('rate limited the request'));
  if (classifiedStatus === 409) { retryAfterMs = Math.max(retryAfterMs, 2000); return outcome('retry', 'CONFLICT', describe('is still processing an earlier attempt with this idempotency key')); }
  if (classifiedStatus === 423) return outcome('retry', 'AVAILABILITY', describe('locked the board for a concurrent change'));
  if (classifiedStatus === 408 || classifiedStatus >= 500) return outcome('retry', 'AVAILABILITY', describe('could not complete the request'));
  if (classifiedStatus === 401) return outcome('rejected', 'AUTHENTICATION', describe('rejected the credential'));
  if (classifiedStatus === 403) return outcome('rejected', 'AUTHORIZATION', describe('denied permission'));
  if (classifiedStatus === 404) return outcome('notFound', 'NOT_FOUND', describe('found no such board, group, or item'));
  if (classifiedStatus === 400) return outcome('defect', 'PROTOCOL', describe("rejected the connector's GraphQL request"));
  if (classifiedStatus === 422) return outcome('rejected', 'VALIDATION', describe('rejected the input'));
  if (classifiedStatus >= 300 && classifiedStatus < 400) return outcome('rejected', 'PROTOCOL', describe('redirected the request'));
  return outcome('rejected', 'PROVIDER_REJECTION', describe('rejected the request'));
}
