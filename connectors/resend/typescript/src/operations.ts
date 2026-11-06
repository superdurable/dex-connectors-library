// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { parseRetryAfter, readErrorTokens } from '@superdurable/connectors-sdk/providerhttp';
import { GetEmailDefinition, SendEmailDefinition } from './generated/definitions.js';
import type { Email, GetEmailInput, SendEmailInput, SentEmail } from './generated/models.js';
import type { ResendClient, ResendResponse } from './client.js';

const emptySent: SentEmail = { id: '' };
const emptyEmail: Email = { id: '', from: '', to: [], subject: '', created_at: '' };

/** Send one email with the stable Dex call ID as Resend's idempotency key. */
export class SendEmailOperation implements Mutation<SendEmailInput, SentEmail> {
  /** Bind a validated Resend client. */ constructor(private readonly client: ResendClient) {}
  /** Return the generated operation contract. */ definition() { return SendEmailDefinition; }
  /** Derive the provider key from the stable call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate, send, and classify one attempt. */
  async invoke(call: Call, input: SendEmailInput): Promise<MutationAttempt<SentEmail>> {
    const operation = 'sendEmail';
    if (!input || !input.from || !input.to?.length || !input.subject || !input.html) return newMutationBranch('defect', emptySent, {}, failure(operation, 'VALIDATION', 'from, to, subject, and html are required'));
    if (input.to.some(recipient => !recipient)) return newMutationBranch('defect', emptySent, {}, failure(operation, 'VALIDATION', 'recipient cannot be blank'));
    if (!call.idempotencyKey || Buffer.byteLength(call.idempotencyKey) > 256) return newMutationBranch('defect', emptySent, {}, failure(operation, 'VALIDATION', 'idempotency key is invalid'));
    const apiKey = await this.client.resolveAPIKey(call);
    if (!apiKey) return credentialDefect(operation, emptySent);
    let response: ResendResponse;
    try { response = await this.client.exchange(call, apiKey, 'POST', '/emails', Buffer.from(JSON.stringify({ from: input.from, to: input.to, subject: input.subject, html: input.html }))); }
    catch { return newMutationRetry(failure(operation, 'AVAILABILITY', 'provider request failed')); }
    const retry = retryResponse(operation, response);
    if (retry) return newMutationRetry(retry.failure, retry.delay);
    if (response.status === 409) {
      if (readErrorTokens(response.body, ['/name'])[0] === 'invalid_idempotent_request') return newMutationBranch('providerRejected', emptySent, {}, failure(operation, 'PROVIDER_REJECTION', 'idempotency key was used for a different request'));
      return newMutationRetry(failure(operation, 'AVAILABILITY', 'provider is processing the same request'));
    }
    if (![200, 201, 202].includes(response.status)) return newMutationBranch('providerRejected', emptySent, {}, failure(operation, 'PROVIDER_REJECTION', `provider rejected the request with HTTP ${response.status}`));
    if (response.isTooLarge) return invalidMutation(operation, 'RESPONSE_TOO_LARGE');
    try {
      const document = decodeObject(response.body);
      if (typeof document.id !== 'string' || !document.id) throw new TypeError('email ID missing');
      return newMutationBranch('sent', { id: document.id }, {});
    } catch { return invalidMutation(operation, 'PROTOCOL'); }
  }
}

/** Retrieve one sent email by its provider ID. */
export class GetEmailOperation implements Query<GetEmailInput, Email> {
  /** Bind a validated Resend client. */ constructor(private readonly client: ResendClient) {}
  /** Return the generated operation contract. */ definition() { return GetEmailDefinition; }
  /** Validate, retrieve, and classify one attempt. */
  async invoke(call: Call, input: GetEmailInput): Promise<QueryAttempt<Email>> {
    const operation = 'getEmail';
    if (!input?.id) return newQueryBranch('defect', emptyEmail, {}, failure(operation, 'VALIDATION', 'email ID is required'));
    const apiKey = await this.client.resolveAPIKey(call);
    if (!apiKey) return credentialDefect(operation, emptyEmail);
    let response: ResendResponse;
    try { response = await this.client.exchange(call, apiKey, 'GET', `/emails/${goPathEscape(input.id)}`); }
    catch { return newQueryRetry(failure(operation, 'AVAILABILITY', 'provider request failed')); }
    const retry = retryResponse(operation, response);
    if (retry) return newQueryRetry(retry.failure, retry.delay);
    if (response.status === 404) return newQueryBranch('notFound', emptyEmail, {}, failure(operation, 'NOT_FOUND', 'provider resource was not found'));
    if (response.status !== 200) return newQueryBranch('providerRejected', emptyEmail, {}, failure(operation, 'PROVIDER_REJECTION', `provider rejected the request with HTTP ${response.status}`));
    if (response.isTooLarge) return invalidQuery(operation, 'RESPONSE_TOO_LARGE');
    try {
      const document = decodeObject(response.body);
      const required = document.id;
      if (typeof required !== 'string' || !required) throw new TypeError('email ID missing');
      const from = stringField(document, 'from');
      const subject = stringField(document, 'subject');
      const createdAt = stringField(document, 'created_at');
      const to = document.to === undefined || document.to === null ? [] : document.to;
      if (!Array.isArray(to) || to.some(value => typeof value !== 'string')) throw new TypeError('invalid recipients');
      return newQueryBranch('found', { id: required, from, to: to as string[], subject, created_at: createdAt }, {});
    } catch { return invalidQuery(operation, 'PROTOCOL'); }
  }
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'resend', operation, kind, message }; }
function credentialDefect<T>(operation: string, empty: T): QueryAttempt<T> { return newQueryBranch('defect', empty, {}, failure(operation, 'AUTHENTICATION', 'connection credentials or request are invalid')); }
function retryResponse(operation: string, response: ResendResponse): { failure: Failure; delay: number } | undefined {
  if (response.status !== 429 && response.status !== 408 && response.status < 500) return undefined;
  const isRateLimit = response.status === 429;
  const header = response.headers['retry-after'];
  return { failure: failure(operation, isRateLimit ? 'RATE_LIMIT' : 'AVAILABILITY', isRateLimit ? 'provider rate limited the request' : 'provider is temporarily unavailable'), delay: parseRetryAfter(Array.isArray(header) ? header[0] ?? '' : header ?? '') };
}
function invalidQuery(operation: string, kind: 'PROTOCOL' | 'RESPONSE_TOO_LARGE'): QueryAttempt<Email> {
  return newQueryBranch('invalidResponse', emptyEmail, {}, failure(operation, kind, kind === 'PROTOCOL' ? 'provider response is incomplete or malformed' : 'provider response exceeded the configured limit'));
}
function invalidMutation(operation: string, kind: 'PROTOCOL' | 'RESPONSE_TOO_LARGE'): MutationAttempt<SentEmail> {
  return newMutationBranch('invalidResponse', emptySent, {}, failure(operation, kind, kind === 'PROTOCOL' ? 'provider response is incomplete or malformed' : 'provider response exceeded the configured limit'));
}
function decodeObject(body: Uint8Array): Record<string, unknown> {
  const value: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('invalid response object');
  return value as Record<string, unknown>;
}
function stringField(document: Record<string, unknown>, name: string): string {
  const value = document[name];
  if (value === undefined || value === null) return '';
  if (typeof value !== 'string') throw new TypeError('invalid response field');
  return value;
}
// Go url.PathEscape leaves these path-safe ASCII characters unchanged.
function goPathEscape(value: string): string {
  let result = '';
  for (const byte of Buffer.from(value, 'utf8')) {
    if ((byte >= 65 && byte <= 90) || (byte >= 97 && byte <= 122) || (byte >= 48 && byte <= 57) || '-._~$&+:=@'.includes(String.fromCharCode(byte))) result += String.fromCharCode(byte);
    else result += `%${byte.toString(16).toUpperCase().padStart(2, '0')}`;
  }
  return result;
}
