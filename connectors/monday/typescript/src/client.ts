// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError, RetryError, resolveCredential, resolveCredentialAfterRejection, type Call, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, readErrorTokens, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { Authorization, CredentialFields } from './generated/refresh.js';
import { isCredentialMaterialError, type Config, type CredentialSource, type Credentials } from './generated/models.js';
import { AddUpdateOperation, CreateItemOperation, GetItemOperation, ListItemsOperation, UpdateItemColumnValuesOperation } from './operations.js';

const defaultAPIURL = 'https://api.monday.com/v2';
const defaultMaxResponseBytes = 4_194_304;
const defaultTimeoutMs = 12_000;
/** The manifest's default pollInterval, five minutes. */
const defaultPollIntervalMs = 300_000;
/** Keeps itemChanged polls from spending the account's daily call limit too fast. */
export const MinimumPollIntervalMs = 30_000;
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
export interface MondayRequest { document: string; variables: Record<string, unknown>; isMutation?: boolean; }
/** Credentials resolved for one call, with the token the selected method sends. */
export interface ResolvedMondayCredentials { credentials: Credentials; accessToken: string; }
/** Facts an exchange proves about whether monday.com processed a write. */
export interface MondayExchangeEvidence {
  /** A rate limit, 503, or unsent request that monday.com never processed. */ provenNotProcessed?: boolean;
  /** A listed refusal status or error code, or a request never sent. */ definiteRefusal?: boolean;
  /** The response echoed the connection credential. */ credentialReflected?: boolean;
  /** A 2xx whose body broke after its headers, so a write may have been accepted. */ successBodyUnreadable?: boolean;
}
/** Classified provider exchange, including safe response metadata. */
export interface MondayExchange extends MondayExchangeEvidence {
  outcome: 'succeeded' | 'notFound' | 'retry' | 'uncertain' | 'rejected' | 'invalid' | 'defect';
  status: number; data?: unknown; requestID: string; isReplayed: boolean; failure?: Failure; retryAfterMs: number;
}
/** What a write exchange proves: accepted, proven unprocessed, definitely refused, or unknown. */
export type WriteDisposition = 'accepted' | 'notProcessed' | 'refused' | 'unknown';

/**
 * Separate definite write outcomes from unknown ones, as Go's classifyWrite does. Any refusal beside
 * a value in the mutation field is unknown, because the value suggests monday.com accepted the write.
 */
export function classifyWrite(exchange: MondayExchange, mutationField: string): WriteDisposition {
  if (exchange.outcome === 'succeeded') return 'accepted';
  if (hasMutationFieldValue(exchange.data, mutationField)) return 'unknown';
  if (exchange.outcome === 'retry' && exchange.provenNotProcessed) return 'notProcessed';
  if (exchange.definiteRefusal && ['notFound', 'rejected', 'defect'].includes(exchange.outcome)) return 'refused';
  return 'unknown';
}

/** Report a non-null value in the named mutation field. */
export function hasMutationFieldValue(data: unknown, mutationField: string): boolean {
  return !!data && typeof data === 'object' && !Array.isArray(data) && (data as Record<string, unknown>)[mutationField] != null;
}

/** Statuses proving monday.com refused a write; any other status leaves it unknown. */
const definiteWriteRefusalStatuses = new Set([400, 401, 403, 404, 411, 413, 414, 415, 422]);
/** The request deadline margin plus the least request window in which a fenced write may resend. */
const minimumFencedRequestWindowMs = 6_000;

/** Authenticated monday.com GraphQL client with manifest driven credential refresh. */
export class MondayClient {
  private readonly apiURL: string;
  private readonly maxResponseBytes: number;
  /** The itemChanged Trigger's delay between polls in milliseconds; at least MinimumPollIntervalMs. */
  readonly pollIntervalMs: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'monday.com', auth: Authorization, credentialFields: CredentialFields });

  /**
   * Validate configuration and retain the host credential source. An omitted or zero pollIntervalMs uses the
   * manifest's five minutes; one under 30 seconds throws, as Go's New does.
   */
  constructor(config: Config, private readonly credentials: CredentialSource, options: MondayClientOptions = {}) {
    if (!credentials) throw new TypeError('monday.com credential provider is required');
    this.apiURL = validateBaseURL(options.apiURL || defaultAPIURL);
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('monday.com response limit must be positive');
    this.pollIntervalMs = config.pollIntervalMs || defaultPollIntervalMs;
    if (!Number.isFinite(this.pollIntervalMs) || this.pollIntervalMs < MinimumPollIntervalMs) throw new TypeError('monday.com pollInterval must be at least 30s');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the listItems Query. */ listItems(): ListItemsOperation { return new ListItemsOperation(this); }
  /** Bind the getItem Query. */ getItem(): GetItemOperation { return new GetItemOperation(this); }
  /** Bind the createItem Mutation. */ createItem(): CreateItemOperation { return new CreateItemOperation(this); }
  /** Bind the updateItemColumnValues Mutation. */ updateItemColumnValues(): UpdateItemColumnValuesOperation { return new UpdateItemColumnValuesOperation(this); }
  /** Bind the addUpdate Mutation. */ addUpdate(): AddUpdateOperation { return new AddUpdateOperation(this); }

  /** The client clock, used for receipts, Retry-After, and the daily-limit pause. */
  currentTime(): Date { return this.now(); }

  /** Resolve credentials and classify one unfenced exchange, refreshing once after an OAuth 401. */
  async exchange(call: Call, operation: string, request: MondayRequest): Promise<MondayExchange> {
    const resolved = await this.resolveCredentials(call, operation);
    if ('outcome' in resolved) return resolved;
    return this.sendWithRejectionRefresh(call, operation, request, resolved, call.context.cancellationSignal);
  }

  /** Resolve the call's credentials, or return the exchange that ends the attempt before any send. */
  async resolveCredentials(call: Call, operation: string): Promise<ResolvedMondayCredentials | MondayExchange> {
    let credentials: Credentials;
    try { credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal); }
    catch (error) {
      if (error instanceof ReauthorizationRequiredError) return failureExchange(operation, 'rejected', 'AUTHENTICATION', 'monday.com authorization must be renewed', 0, '', false, { definiteRefusal: true });
      if (error instanceof RetryError) return failureExchange(operation, 'retry', 'AVAILABILITY', 'monday.com OAuth token refresh is temporarily unavailable', 0, '', false, { provenNotProcessed: true });
      if (error instanceof ConnectionNotSelectedError || error instanceof CredentialBrokerRejectedError || error instanceof ObjectNotFoundError || isCredentialMaterialError(error)) {
        return failureExchange(operation, 'defect', 'AUTHENTICATION', 'monday.com connection credentials are unavailable', 0, '', false, { definiteRefusal: true });
      }
      // A broker or store outage is transient; only a missing or refused connection is terminal.
      return failureExchange(operation, 'retry', 'AVAILABILITY', 'monday.com connection credentials could not be read, so nothing was sent', 0, '', false, { provenNotProcessed: true });
    }
    const accessToken = credentials.authMethodId === 'monday-oauth' ? credentials.accessToken?.reveal() : credentials.apiToken?.reveal();
    if (!['personal-api-token', 'monday-oauth'].includes(credentials.authMethodId ?? '') || !accessToken || !isHeaderSafeCredential(accessToken)) {
      return failureExchange(operation, 'defect', 'AUTHENTICATION', 'monday.com connection credentials are unavailable', 0, '', false, { definiteRefusal: true });
    }
    return { credentials, accessToken };
  }

  /**
   * Send once under signal and resend once after monday.com rejects an OAuth token. A fenced write
   * passes its write deadline and resends only when that window still fits a request.
   */
  async sendWithRejectionRefresh(call: Call, operation: string, request: MondayRequest, resolved: ResolvedMondayCredentials, signal: AbortSignal | undefined, writeDeadlineMs?: number): Promise<MondayExchange> {
    let result = await this.sendOnce(call, operation, request, resolved.accessToken, signal);
    if (result.status === 401 && resolved.credentials.authMethodId === 'monday-oauth' && 'resolveAfterRejection' in this.credentials) {
      try {
        const replacement = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
        const replacementToken = replacement.accessToken?.reveal();
        if (replacementToken && isHeaderSafeCredential(replacementToken)) {
          // HTTP 401 proves the first send was refused, and the refreshed one is not sent.
          if (writeDeadlineMs !== undefined && writeDeadlineMs - Date.now() < minimumFencedRequestWindowMs) {
            return failureExchange(operation, 'retry', 'AVAILABILITY', 'not enough time remains to resend the mutation', 0, '', false, { provenNotProcessed: true });
          }
          result = await this.sendOnce(call, operation, request, replacementToken, signal);
        }
      } catch { /* Go retains the original HTTP 401 when coordinated refresh fails. */ }
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

  private async sendOnce(call: Call, operation: string, request: MondayRequest, accessToken: string, signal: AbortSignal | undefined): Promise<MondayExchange> {
    if (request.isMutation && !call.idempotencyKey) return failureExchange(operation, 'defect', 'LOCAL_DEFECT', 'monday.com mutation has no idempotency key', 0, '', false, { definiteRefusal: true });
    const timeoutMs = Math.min(defaultTimeoutMs, call.context.deadline ? call.context.deadline.getTime() - Date.now() - 3_000 : defaultTimeoutMs);
    if (timeoutMs <= 0) return failureExchange(operation, 'retry', 'TRANSPORT', 'monday.com request context expired before dispatch', 0, '', false, { provenNotProcessed: true });
    const providerRequest: ProviderRequest = {
      url: this.apiURL, method: 'POST', headers: {
        Accept: 'application/json', 'Content-Type': 'application/json', Authorization: accessToken, 'API-Version': '2026-10',
        ...(request.isMutation ? { 'Idempotency-Key': call.idempotencyKey! } : {}),
      }, body: Buffer.from(JSON.stringify({ query: request.document, variables: request.variables })),
      // A broken or oversized error body throws, so a refusal status is never read from a partial body.
      maxResponseBytes: this.maxResponseBytes,
      addressPolicy: this.apiURL === defaultAPIURL ? 'publicHTTPS' : 'privateNetworkHTTP', timeoutMs, ...(signal ? { signal } : {}),
    };
    let response: ProviderResponse;
    let isErrorBodyOversized = false;
    try { response = await this.send(providerRequest); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined && error.headers !== undefined) {
        response = { status: error.status, headers: error.headers, body: new Uint8Array() };
        isErrorBodyOversized = true;
        if (response.status >= 200 && response.status < 300) return failureExchange(operation, 'invalid', 'RESPONSE_TOO_LARGE', 'monday.com response exceeds the configured maxResponseBytes limit', response.status, '', headerValue(response.headers['idempotency-replayed']).trim().toLowerCase() === 'true');
      } else if (error instanceof ProviderTransportError && error.isProvablyNotSent) return failureExchange(operation, 'retry', 'TRANSPORT', 'monday.com could not be reached; no request was sent', 0, '', false, { provenNotProcessed: true });
      else if ((error instanceof ProviderTransportError || error instanceof ProviderStalledError) && error.status !== undefined && error.status >= 200 && error.status < 300) {
        // Keep the status: monday.com may have accepted the write, so it is never resent.
        const isReplayed = headerValue(error.headers?.['idempotency-replayed']).trim().toLowerCase() === 'true';
        return failureExchange(operation, request.isMutation ? 'invalid' : 'retry', 'TRANSPORT', 'monday.com response could not be read', error.status, '', isReplayed, { successBodyUnreadable: true });
      } else return failureExchange(operation, request.isMutation ? 'uncertain' : 'retry', 'TRANSPORT', 'monday.com request failed before a response arrived');
    }
    const body = response.body;
    const status = response.status;
    const isReplayed = headerValue(response.headers['idempotency-replayed']).trim().toLowerCase() === 'true';
    if (Buffer.from(body).includes(Buffer.from(accessToken))) return failureExchange(operation, 'invalid', 'PROTOCOL', 'monday.com response reflected the connection credential', status, '', isReplayed, { credentialReflected: true });
    const requestID = readErrorTokens(body, ['/extensions/request_id'])[0] ?? '';
    const safeRequestID = /^[A-Za-z0-9._:-]{1,128}$/.test(requestID) ? requestID : '';
    const base = { status, requestID: safeRequestID, isReplayed };
    const retryAfterMs = parseRetryAfter(headerValue(response.headers['retry-after']), this.now());
    let envelope: Record<string, unknown> | undefined;
    try { const parsed: unknown = JSON.parse(Buffer.from(body).toString()); if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) envelope = parsed as Record<string, unknown>; }
    catch { /* Status alone classifies non-success replies. */ }
    const data = envelope?.data === null ? undefined : envelope?.data;
    if (status < 200 || status >= 300) {
      const classified = classifyFailure(operation, body, status, accessToken, retryAfterMs, base, data);
      // An oversized error body may hide an accepted write, so its refusal status proves nothing.
      if (request.isMutation && isErrorBodyOversized && classified.definiteRefusal) return { ...classified, outcome: 'retry', definiteRefusal: false };
      return classified;
    }
    if (!envelope) return { ...failureExchange(operation, 'invalid', 'PROTOCOL', 'monday.com returned a response that is not a GraphQL JSON object', status, safeRequestID, isReplayed), retryAfterMs };
    if (envelope.errors !== undefined && envelope.errors !== null && !Array.isArray(envelope.errors)) return failureExchange(operation, 'invalid', 'PROTOCOL', 'monday.com returned a response that is not a GraphQL JSON object', status, safeRequestID, isReplayed);
    if (Array.isArray(envelope.errors) && envelope.errors.length) return classifyFailure(operation, body, status, accessToken, retryAfterMs, base, data);
    if (data === undefined) return failureExchange(operation, 'invalid', 'PROTOCOL', 'monday.com returned neither data nor errors', status, safeRequestID, isReplayed);
    return { outcome: 'succeeded', ...base, data, retryAfterMs: 0 };
  }
}

function failureExchange(operation: string, outcome: MondayExchange['outcome'], kind: Failure['kind'], message: string, status = 0, requestID = '', isReplayed = false, evidence: MondayExchangeEvidence = {}): MondayExchange {
  return { outcome, status, requestID, isReplayed, failure: { kind, provider: 'monday', operation, message }, retryAfterMs: 0, ...evidence };
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
  const outcome = (disposition: MondayExchange['outcome'], kind: Failure['kind'], message: string, evidence: MondayExchangeEvidence = {}): MondayExchange => ({ outcome: disposition, ...base, ...(data === undefined ? {} : { data }), failure: { kind, provider: 'monday', operation, message }, retryAfterMs, ...evidence });
  // Rate limits and 503 prove nothing was processed; a listed code or status proves a refusal.
  const notProcessed = { provenNotProcessed: true }, refused = { definiteRefusal: true };
  if (code === 'DAILY_LIMIT_EXCEEDED') return outcome('rejected', 'QUOTA_EXHAUSTED', describe('daily call limit is exhausted until midnight UTC'), refused);
  if (rateCodes.has(code)) return outcome('retry', 'RATE_LIMIT', describe('rate limited the request'), notProcessed);
  if (code === 'IDEMPOTENCY_CONFLICT') { retryAfterMs = Math.max(retryAfterMs, 2000); return outcome('retry', 'CONFLICT', describe('is still processing an earlier attempt with this idempotency key')); }
  if (unavailableCodes.has(code)) return outcome('retry', 'AVAILABILITY', describe('temporarily refused the request'), notProcessed);
  if (authCodes.has(code)) return outcome('rejected', 'AUTHENTICATION', describe('rejected the credential'), refused);
  if (permissionCodes.has(code)) return outcome('rejected', 'AUTHORIZATION', describe('denied permission'), refused);
  if (missingCodes.has(code)) return outcome('notFound', 'NOT_FOUND', describe('found no such board, group, or item'), refused);
  if (validationCodes.has(code)) return outcome('rejected', 'VALIDATION', describe('rejected the input'), refused);
  if (documentCodes.has(code)) return outcome('defect', 'PROTOCOL', describe("rejected the connector's GraphQL request"), refused);
  if (classifiedStatus === 429) return outcome('retry', 'RATE_LIMIT', describe('rate limited the request'), notProcessed);
  if (classifiedStatus === 409) { retryAfterMs = Math.max(retryAfterMs, 2000); return outcome('retry', 'CONFLICT', describe('is still processing an earlier attempt with this idempotency key')); }
  if (classifiedStatus === 423) return outcome('retry', 'AVAILABILITY', describe('locked the board for a concurrent change'));
  if (classifiedStatus === 503) return outcome('retry', 'AVAILABILITY', describe('could not complete the request'), notProcessed);
  if (classifiedStatus === 408 || classifiedStatus >= 500) return outcome('retry', 'AVAILABILITY', describe('could not complete the request'));
  if (classifiedStatus === 401) return outcome('rejected', 'AUTHENTICATION', describe('rejected the credential'), refused);
  if (classifiedStatus === 403) return outcome('rejected', 'AUTHORIZATION', describe('denied permission'), refused);
  if (classifiedStatus === 404) return outcome('notFound', 'NOT_FOUND', describe('found no such board, group, or item'), refused);
  if (classifiedStatus === 400) return outcome('defect', 'PROTOCOL', describe("rejected the connector's GraphQL request"), refused);
  if (classifiedStatus === 422) return outcome('rejected', 'VALIDATION', describe('rejected the input'), refused);
  if (classifiedStatus >= 300 && classifiedStatus < 400) return outcome('rejected', 'PROTOCOL', describe('redirected the request'));
  return outcome('rejected', 'PROVIDER_REJECTION', describe('rejected the request'), definiteWriteRefusalStatuses.has(classifiedStatus) ? refused : {});
}
