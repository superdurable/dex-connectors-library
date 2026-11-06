// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { ReauthorizationRequiredError, resolveCredential, resolveCredentialAfterRejection, type Call, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { RefreshFailedError } from '@superdurable/connectors-sdk/projectconfig';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { AddCommentOperation, CreateIssueOperation, FindUserByEmailOperation, GetIssueOperation, ListWorkflowStatesOperation, SearchIssuesOperation, UpdateIssueOperation } from './operations.js';

const defaultEndpoint = 'https://api.linear.app/graphql';
const requestIdPattern = /^[A-Za-z0-9._:-]{1,128}$/;
const knownTypes = new Set(['feature not accessible', 'invalid input', 'ratelimited', 'network error', 'authentication error', 'forbidden', 'bootstrap error', 'unknown', 'internal error', 'other', 'user error', 'graphql error', 'lock timeout', 'usage limit exceeded']);

/** Replace the transport and clock for deterministic provider tests. */
export interface LinearClientOptions {
  /** Alternate GraphQL endpoint for a provider stand-in. */
  endpoint?: string;
  /** Sender; production uses the SDK's bounded provider transport. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Clock for receipts and Retry-After. */
  now?: () => Date;
}

/** One classified GraphQL exchange. */
export interface LinearExchange {
  disposition: 'succeeded' | 'retry' | 'rejected' | 'invalid' | 'defect';
  data?: Record<string, unknown>;
  status: number;
  requestId: string;
  failure?: Failure;
  retryAfterMs: number;
}

/** Authenticated Linear GraphQL client with generated OAuth refresh rules. */
export class LinearClient {
  private readonly endpoint: string;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Linear', auth: Authorization, credentialFields: CredentialFields });

  /** Validate configuration and bind a renewable credential source. */
  constructor(config: Config, readonly credentials: CredentialSource, options: LinearClientOptions = {}) {
    if (!credentials) throw new TypeError('Linear credential provider is required');
    this.endpoint = validateBaseURL(options.endpoint ?? defaultEndpoint);
    this.maxResponseBytes = config.maxResponseBytes ?? 4_194_304;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('Linear maxResponseBytes must be positive');
    if (config.webhookMaxBodyBytes !== undefined && (!Number.isSafeInteger(config.webhookMaxBodyBytes) || config.webhookMaxBodyBytes < 1)) throw new TypeError('Linear webhookMaxBodyBytes must be positive');
    if (config.webhookSignatureToleranceMs !== undefined && (!Number.isSafeInteger(config.webhookSignatureToleranceMs) || config.webhookSignatureToleranceMs < 1_000)) throw new TypeError('Linear webhookSignatureTolerance must be at least one second');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the search query. */ searchIssues() { return new SearchIssuesOperation(this); }
  /** Bind the issue query. */ getIssue() { return new GetIssueOperation(this); }
  /** Bind the create mutation. */ createIssue() { return new CreateIssueOperation(this); }
  /** Bind the update mutation. */ updateIssue() { return new UpdateIssueOperation(this); }
  /** Bind the comment mutation. */ addComment() { return new AddCommentOperation(this); }
  /** Bind the user query. */ findUserByEmail() { return new FindUserByEmailOperation(this); }
  /** Bind the workflow state query. */ listWorkflowStates() { return new ListWorkflowStatesOperation(this); }

  /** Return a secret-free correlation receipt. */
  receipt(call: Call, exchange: LinearExchange, objectId = ''): ReceiptDraft {
    return { callId: call.id, provider: 'linear', observedAt: this.now().toISOString(),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(objectId ? { providerObjectId: objectId } : {}),
      ...(exchange.requestId ? { providerRequestId: exchange.requestId } : {}) };
  }

  /** Resolve credentials once for an operation and classify host failures. */
  async open(call: Call, operation: string): Promise<{ credentials: Credentials } | { failed: LinearExchange }> {
    try {
      const credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      const token = credentials.authMethodId === 'linear-oauth' ? credentials.accessToken?.reveal() : credentials.apiKey?.reveal();
      if (!['linear-oauth', 'personal-api-key'].includes(credentials.authMethodId ?? '') || !token || !isHeaderSafeCredential(token)) throw new TypeError('unusable Linear credential');
      return { credentials };
    } catch (error) {
      if (error instanceof ReauthorizationRequiredError) return { failed: this.failed(operation, 'rejected', 'AUTHENTICATION', 'Linear authorization must be renewed in Dex Web Connections') };
      if (error instanceof RefreshFailedError) return { failed: this.failed(operation, 'retry', 'AVAILABILITY', 'Linear OAuth token refresh is temporarily unavailable') };
      return { failed: this.failed(operation, 'defect', 'AUTHENTICATION', 'Linear connection credentials are unavailable or unusable') };
    }
  }

  /** Send one named GraphQL request, then refresh and resend a rejected OAuth token once. */
  async exchange(call: Call, operation: string, credentials: Credentials, operationName: string, query: string, variables: Record<string, unknown>): Promise<LinearExchange> {
    let result = await this.sendOnce(call, operation, credentials, operationName, query, variables);
    if (result.status !== 401 || credentials.authMethodId !== 'linear-oauth' || !('resolveAfterRejection' in this.credentials)) return result;
    try {
      const replacement = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      result = await this.sendOnce(call, operation, replacement, operationName, query, variables);
    } catch { /* The original 401 remains the provider's terminal classification. */ }
    return result;
  }

  private async sendOnce(call: Call, operation: string, credentials: Credentials, operationName: string, query: string, variables: Record<string, unknown>): Promise<LinearExchange> {
    const token = credentials.authMethodId === 'linear-oauth' ? credentials.accessToken?.reveal() : credentials.apiKey?.reveal();
    if (!token || !isHeaderSafeCredential(token)) return this.failed(operation, 'defect', 'AUTHENTICATION', 'Linear connection credentials are unavailable or unusable');
    const authorization = credentials.authMethodId === 'linear-oauth' ? `Bearer ${token}` : token;
    const request: ProviderRequest = { url: this.endpoint, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/json', Authorization: authorization },
      body: Buffer.from(JSON.stringify({ operationName, query, variables })), maxResponseBytes: this.maxResponseBytes,
      maxErrorResponseBytes: this.maxResponseBytes,
      timeoutMs: 12_000, addressPolicy: this.endpoint === defaultEndpoint ? 'publicHTTPS' : 'unrestricted', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError) {
        const status = error.status ?? 0;
        if (status >= 200 && status < 300) return this.failed(operation, 'invalid', 'RESPONSE_TOO_LARGE', 'Linear response exceeds the configured maxResponseBytes limit', status, header(error.headers ?? {}, 'x-request-id'));
        return classify(operation, status, {}, parseRetryAfter(header(error.headers ?? {}, 'retry-after'), this.now()), header(error.headers ?? {}, 'x-request-id'));
      }
      return this.failed(operation, 'retry', 'TRANSPORT', 'Linear request failed before a response arrived');
    }
    const requestId = header(response.headers, 'x-request-id');
    const safeRequestId = requestIdPattern.test(requestId.trim()) ? requestId.trim() : '';
    const body = Buffer.from(response.body).toString('utf8');
    if (body.includes(token)) return this.failed(operation, 'invalid', 'PROTOCOL', 'Linear response reflected the connection credential', response.status, safeRequestId);
    let envelope: unknown;
    try { envelope = JSON.parse(body); }
    catch {
      if (response.status >= 200 && response.status < 300) return this.failed(operation, 'invalid', 'PROTOCOL', 'Linear returned a response that is not a GraphQL JSON object', response.status, safeRequestId);
      return classify(operation, response.status, {}, parseRetryAfter(header(response.headers, 'retry-after'), this.now()), safeRequestId);
    }
    if (!envelope || typeof envelope !== 'object' || Array.isArray(envelope)) {
      if (response.status >= 200 && response.status < 300) return this.failed(operation, 'invalid', 'PROTOCOL', 'Linear returned a response that is not a GraphQL JSON object', response.status, safeRequestId);
      return classify(operation, response.status, {}, 0, safeRequestId);
    }
    const fields = envelope as Record<string, unknown>;
    if (fields.errors != null && !Array.isArray(fields.errors)) {
      if (response.status >= 200 && response.status < 300) return this.failed(operation, 'invalid', 'PROTOCOL', 'Linear returned a response that is not a GraphQL JSON object', response.status, safeRequestId);
      return classify(operation, response.status, {}, parseRetryAfter(header(response.headers, 'retry-after'), this.now()), safeRequestId);
    }
    const errors = Array.isArray(fields.errors) ? fields.errors : [];
    const data = fields.data && typeof fields.data === 'object' && !Array.isArray(fields.data) ? fields.data as Record<string, unknown> : undefined;
    if (errors.length || response.status < 200 || response.status >= 300) {
      const first = errors[0] && typeof errors[0] === 'object' ? errors[0] as Record<string, unknown> : {};
      const extensions = first.extensions && typeof first.extensions === 'object' ? first.extensions as Record<string, unknown> : {};
      const code = typeof extensions.code === 'string' && /^[A-Za-z0-9_.:-]{1,64}$/.test(extensions.code) && !extensions.code.includes(token) ? extensions.code : '';
      const errorType = typeof extensions.type === 'string' && knownTypes.has(extensions.type) ? extensions.type : '';
      return { ...classify(operation, response.status, { code, errorType }, parseRetryAfter(header(response.headers, 'retry-after'), this.now()), safeRequestId), ...(data ? { data } : {}) };
    }
    if (!data) return this.failed(operation, 'invalid', 'PROTOCOL', 'Linear returned neither data nor errors', response.status, safeRequestId);
    return { disposition: 'succeeded', data, status: response.status, requestId: safeRequestId, retryAfterMs: 0 };
  }

  private failed(operation: string, disposition: LinearExchange['disposition'], kind: Failure['kind'], message: string, status = 0, requestId = ''): LinearExchange {
    return { disposition, status, requestId, retryAfterMs: 0, failure: { kind, provider: 'linear', operation, message } };
  }
}

function header(headers: ProviderResponse['headers'], name: string): string {
  const value = headers[name] ?? headers[name.toLowerCase()] ?? headers[name.replace(/(^|-)([a-z])/g, (_, prefix: string, letter: string) => prefix + letter.toUpperCase())];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}

function classify(operation: string, status: number, summary: { code?: string; errorType?: string }, retryAfterMs: number, requestId: string): LinearExchange {
  const code = summary.code ?? '', errorType = summary.errorType ?? '';
  let disposition: LinearExchange['disposition'] = 'rejected';
  let kind: Failure['kind'] = 'PROVIDER_REJECTION';
  let verb = 'rejected the request';
  if (code === 'RATELIMITED' || errorType === 'ratelimited') { disposition = 'retry'; kind = 'RATE_LIMIT'; verb = 'rate limited the request'; }
  else if (['AUTHENTICATION_ERROR', 'UNAUTHENTICATED'].includes(code) || errorType === 'authentication error') { kind = 'AUTHENTICATION'; verb = 'rejected the credential'; }
  else if (code === 'FORBIDDEN' || ['forbidden', 'feature not accessible'].includes(errorType)) { kind = 'AUTHORIZATION'; verb = 'denied permission'; }
  else if (errorType === 'usage limit exceeded') { kind = 'QUOTA_EXHAUSTED'; verb = 'reported a workspace usage limit'; }
  else if (['GRAPHQL_PARSE_FAILED', 'GRAPHQL_VALIDATION_FAILED', 'BAD_REQUEST', 'OPERATION_RESOLUTION_FAILURE'].includes(code) || errorType === 'graphql error') { disposition = 'defect'; kind = 'PROTOCOL'; verb = "rejected the connector's GraphQL request"; }
  else if (['INTERNAL_SERVER_ERROR', 'INTERNAL_ERROR', 'LOCK_TIMEOUT'].includes(code) || ['internal error', 'lock timeout', 'network error', 'bootstrap error'].includes(errorType)) { disposition = 'retry'; kind = 'AVAILABILITY'; verb = 'could not complete the request'; }
  else if (['INPUT_ERROR', 'BAD_USER_INPUT', 'INVALID_INPUT'].includes(code) || ['invalid input', 'user error'].includes(errorType)) { kind = 'VALIDATION'; verb = 'rejected the input'; }
  else if (status === 429) { disposition = 'retry'; kind = 'RATE_LIMIT'; verb = 'rate limited the request'; }
  else if (status === 408 || status >= 500) { disposition = 'retry'; kind = 'AVAILABILITY'; verb = 'could not complete the request'; }
  else if (status === 401) { kind = 'AUTHENTICATION'; verb = 'rejected the credential'; }
  else if (status === 403) { kind = 'AUTHORIZATION'; verb = 'denied permission'; }
  else if (status === 400 && !code && !errorType) { disposition = 'defect'; kind = 'PROTOCOL'; verb = "rejected the connector's GraphQL request"; }
  else if (status >= 300 && status < 400) { kind = 'PROTOCOL'; verb = 'redirected the request'; }
  const suffix = [code, errorType].filter(Boolean).join('; ');
  const failure: Failure = { kind, provider: 'linear', operation, message: `Linear ${verb} (HTTP ${status})${suffix ? ` [${suffix}]` : ''}` };
  return { disposition, status, requestId, retryAfterMs, failure };
}
