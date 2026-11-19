// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, SecretString, type Call, type CredentialProvider, type Failure, type MutationAttempt, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Config, Credentials } from './generated/models.js';
import { GetFormOperation, ListFormsOperation, ListResponsesOperation, UpsertWebhookOperation } from './operations.js';

const hosts: Record<string, string> = { us: 'https://api.typeform.com', eu: 'https://api.eu.typeform.com', newEu: 'https://api.typeform.eu' };

/** Injectable sender for provider tests. */
export type TypeformRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Connector dependencies for deterministic tests. */
export interface TypeformClientOptions {
  /** HTTP sender; omitted uses the SDK transport. */ send?: TypeformRequestSender;
  /** Receipt and Retry-After clock. */ now?: () => Date;
}
/** One Typeform API client with live credentials. */
export class TypeformClient {
  private readonly baseURL: string;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: TypeformRequestSender;
  private readonly now: () => Date;

  /** Validate manifest configuration without contacting Typeform. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: TypeformClientOptions = {}) {
    if (!credentials) throw new TypeError('Typeform credential provider is required');
    this.baseURL = hosts[config.dataCenter || 'us'] ?? '';
    if (!this.baseURL) throw new TypeError('Typeform dataCenter is invalid');
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Typeform maxResponseBytes must be positive');
    if (config.webhookMaxBodyBytes !== undefined && (!Number.isSafeInteger(config.webhookMaxBodyBytes) || config.webhookMaxBodyBytes <= 0)) throw new TypeError('Typeform webhookMaxBodyBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind form listing. */ listForms(): ListFormsOperation { return new ListFormsOperation(this); }
  /** Bind one form read. */ getForm(): GetFormOperation { return new GetFormOperation(this); }
  /** Bind response listing. */ listResponses(): ListResponsesOperation { return new ListResponsesOperation(this); }
  /** Bind idempotent webhook upsert. */ upsertWebhook(): UpsertWebhookOperation { return new UpsertWebhookOperation(this); }

  /** Resolve credentials, preserving Go's Retry on a temporarily unreadable connection. */
  async resolve(call: Call, operation: string): Promise<{ credentials: Credentials } | { outcome: ExchangeFailure }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { outcome: { retry: true, failure: this.failure(operation, 'AVAILABILITY', 'the Typeform connection could not be loaded yet') } }; }
    if (!(credentials.accessToken instanceof SecretString) || !isHeaderSafeCredential(credentials.accessToken.reveal())) {
      return { outcome: { branch: 'defect', failure: this.failure(operation, 'AUTHENTICATION', 'the Typeform access token is blank or contains characters a header cannot carry') } };
    }
    return { credentials };
  }
  /** Send one request and classify the response without reading an error body past the SDK bound. */
  async exchange(call: Call, operation: string, credentials: Credentials, method: string, path: string, body?: unknown): Promise<{ response: ProviderResponse } | { outcome: ExchangeFailure }> {
    let encoded: Uint8Array | undefined;
    try { if (body !== undefined) encoded = Buffer.from(JSON.stringify(body)); }
    catch { return { outcome: { branch: 'defect', failure: this.failure(operation, 'LOCAL_DEFECT', 'Typeform request could not be built') } }; }
    let response: ProviderResponse;
    try {
      response = await this.send({ url: this.baseURL + path, method,
        headers: { Authorization: `Bearer ${credentials.accessToken.reveal()}`, Accept: 'application/json', ...(encoded ? { 'Content-Type': 'application/json' } : {}) },
        ...(encoded ? { body: encoded } : {}), maxResponseBytes: this.maxResponseBytes, timeoutMs: 25_000,
        addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        if (error.status !== undefined && (error.status < 200 || error.status >= 300)) return { outcome: this.classifyStatus(operation, error.status, error.headers ?? {}) };
        return { outcome: { branch: 'invalidResponse', receipt: true, failure: this.failure(operation, 'RESPONSE_TOO_LARGE', 'Typeform response exceeds the configured maxResponseBytes') } };
      }
      return { outcome: { retry: true, failure: this.failure(operation, 'TRANSPORT', 'Typeform could not be reached or its answer was lost') } };
    }
    if (response.status >= 200 && response.status < 300) return { response };
    return { outcome: this.classifyStatus(operation, response.status, response.headers, response.body) };
  }
  /** Produce the same provider object receipt as Go after a provider answer. */
  receipt(call: Call, objectID: string): ReceiptDraft {
    return { callId: call.id, provider: 'typeform', observedAt: this.now().toISOString(), ...(objectID ? { providerObjectId: objectID } : {}) };
  }
  /** Translate a classified outcome into the SDK attempt shape. */
  queryOutcome<Output>(call: Call, outcome: ExchangeFailure, empty: Output, objectID: string, hasNotFound: boolean): QueryAttempt<Output> {
    if (outcome.retry) return newQueryRetry(outcome.failure, outcome.retryAfterMs);
    return newQueryBranch(outcome.branch === 'notFound' && !hasNotFound ? 'providerRejected' : outcome.branch!, empty, outcome.receipt ? this.receipt(call, objectID) : {}, outcome.failure);
  }
  /** Translate a classified outcome into the mutation attempt shape. */
  mutationOutcome<Output>(call: Call, outcome: ExchangeFailure, empty: Output, objectID: string): MutationAttempt<Output> {
    if (outcome.retry) return newMutationRetry(outcome.failure, outcome.retryAfterMs);
    return newMutationBranch(outcome.branch!, empty, outcome.receipt ? this.receipt(call, objectID) : {}, outcome.failure);
  }
  /** Make a connector failure without leaking provider text. */
  failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'typeform', operation, message }; }
  /** Classify a malformed 2xx response. */
  malformed(operation: string): ExchangeFailure { return { branch: 'invalidResponse', receipt: true, failure: this.failure(operation, 'PROTOCOL', 'Typeform returned a malformed response') }; }
  private classifyStatus(operation: string, status: number, headers: Record<string, string | string[] | undefined>, body?: Uint8Array): ExchangeFailure {
    const rawRetryAfter = headers['retry-after'] ?? headers['Retry-After'];
    const retryAfter = Array.isArray(rawRetryAfter) ? rawRetryAfter[0] ?? '' : rawRetryAfter ?? '';
    if (status === 429) return { retry: true, retryAfterMs: parseRetryAfter(retryAfter, this.now()), failure: this.failure(operation, 'RATE_LIMIT', 'Typeform rate limited the request; it allows two requests per second per account') };
    if (status === 408 || status >= 500) return { retry: true, failure: this.failure(operation, 'AVAILABILITY', `Typeform answered HTTP ${status}`) };
    let kind: Failure['kind'] = 'PROVIDER_REJECTION';
    let message = `Typeform rejected the request with HTTP ${status}`;
    if (status === 401) { kind = 'AUTHENTICATION'; message = 'Typeform rejected the access token; generate a new personal access token'; }
    else if (status === 403) { kind = 'AUTHORIZATION'; message = 'Typeform denied the request; check that the token is valid and has the scopes in the connector guide'; }
    else if (status === 402) message = "the Typeform account's plan does not include a feature this request uses";
    else if (status === 404) { kind = 'NOT_FOUND'; message = 'the Typeform form was not found'; }
    else if (status === 409) { kind = 'CONFLICT'; message = 'Typeform reported a conflicting change'; }
    else if (status === 400 && body) {
      try { const value = JSON.parse(Buffer.from(body).toString()) as { code?: unknown }; if (value?.code === 'webhook_url_https_required') { kind = 'VALIDATION'; message = 'Typeform requires an HTTPS webhook URL'; } } catch { /* An unreadable error body keeps its HTTP classification. */ }
    }
    return { branch: status === 404 ? 'notFound' : 'providerRejected', receipt: true, failure: this.failure(operation, kind, message) };
  }
}
/** Classified provider outcome. */
export interface ExchangeFailure {
  /** Retry until Dex's policy is exhausted. */ retry?: boolean;
  /** Terminal branch. */ branch?: string;
  /** Connector failure. */ failure: Failure;
  /** Whether a provider answer was received. */ receipt?: boolean;
  /** Provider-directed delay. */ retryAfterMs?: number;
}
