// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter } from '@superdurable/connectors-sdk/providerhttp';
import { InvalidKlaviyoCredentialError, type KlaviyoClient } from './client.js';
import { CreateEventDefinition, GetProfileDefinition } from './generated/definitions.js';
import type { CreateEventInput, EventAcceptance, GetProfileInput, Profile } from './generated/models.js';

const emptyProfile: Profile = { id: '' };
const emptyAcceptance: EventAcceptance = { accepted: false };

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'klaviyo', operation, message }; }
function queryFailure(branch: string, kind: Failure['kind'], message: string): QueryAttempt<Profile> { return newQueryBranch(branch, emptyProfile, {}, failure('getProfile', kind, message)); }
function mutationFailure(branch: string, kind: Failure['kind'], message: string): MutationAttempt<EventAcceptance> { return newMutationBranch(branch, emptyAcceptance, {}, failure('createEvent', kind, message)); }
function isRetryStatus(status: number): boolean { return status === 408 || status === 409 || status === 429 || status >= 500; }
function rejectionKind(status: number): Failure['kind'] { return status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION'; }
function retryHeader(headers: Record<string, unknown>): string { const value = headers['retry-after']; return Array.isArray(value) ? String(value[0] ?? '') : typeof value === 'string' ? value : ''; }
function pathEscape(value: string): string { return Buffer.from(value, 'utf8').reduce((result, byte) => result + (/[A-Za-z0-9\-._~!$&'()*+,;=:@?]/.test(String.fromCharCode(byte)) ? String.fromCharCode(byte) : `%${byte.toString(16).toUpperCase().padStart(2, '0')}`), ''); }
function validEmail(value: string): boolean {
  // Go's mail.ParseAddress returns a different Address for quoted local parts and display names.
  const atom = "[A-Za-z0-9!#$%&'*+/=?^_`{|}~\\-\\p{L}\\p{N}\\p{M}\\p{S}]+";
  return new RegExp(`^${atom}(?:\\.${atom})*@(?:${atom}(?:\\.${atom})*|\\[[0-9]+(?:\\.[0-9]+){3}\\])$`, 'u').test(value);
}

/** Read a profile by its opaque Klaviyo ID. */
export class GetProfileOperation implements Query<GetProfileInput, Profile> {
  /** Bind an authenticated client. */ constructor(private readonly client: KlaviyoClient) {}
  /** Return generated manifest metadata. */ definition() { return GetProfileDefinition; }
  /** Validate, send, and classify one lookup attempt. */
  async invoke(call: Call, input: GetProfileInput): Promise<QueryAttempt<Profile>> {
    if (!input.id.trim() || /[/?&#]/.test(input.id)) return queryFailure('defect', 'VALIDATION', 'profile ID is invalid');
    let response;
    try { response = await this.client.request(call, 'GET', `/profiles/${pathEscape(input.id)}`); }
    catch (error) {
      if (error instanceof InvalidKlaviyoCredentialError) return queryFailure('defect', 'AUTHENTICATION', 'Klaviyo credentials are unavailable or invalid');
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (isRetryStatus(error.status)) return newQueryRetry(failure('getProfile', 'AVAILABILITY', 'Klaviyo profile lookup is temporarily unavailable'), parseRetryAfter(retryHeader(error.headers ?? {}), this.client.now()));
        if (error.status === 404) return queryFailure('notFound', 'NOT_FOUND', 'Klaviyo profile was not found');
        if (error.status !== 200) return queryFailure('providerRejected', rejectionKind(error.status), `Klaviyo rejected profile lookup with HTTP ${error.status}`);
        return queryFailure('invalidResponse', 'RESPONSE_TOO_LARGE', 'Klaviyo profile response exceeded the limit');
      }
      return newQueryRetry(failure('getProfile', 'TRANSPORT', 'Klaviyo profile response is unavailable'));
    }
    if (isRetryStatus(response.status)) return newQueryRetry(failure('getProfile', 'AVAILABILITY', 'Klaviyo profile lookup is temporarily unavailable'), parseRetryAfter(retryHeader(response.headers), this.client.now()));
    if (response.status === 404) return queryFailure('notFound', 'NOT_FOUND', 'Klaviyo profile was not found');
    if (response.status !== 200) return queryFailure('providerRejected', rejectionKind(response.status), `Klaviyo rejected profile lookup with HTTP ${response.status}`);
    try {
      const wire: unknown = JSON.parse(Buffer.from(response.body).toString('utf8'));
      if (!wire || typeof wire !== 'object' || !('data' in wire) || !wire.data || typeof wire.data !== 'object') throw new Error('invalid profile');
      const data = wire.data as Record<string, unknown>;
      if (data.type !== 'profile' || typeof data.id !== 'string' || !data.id) throw new Error('invalid profile');
      const attributes = data.attributes && typeof data.attributes === 'object' ? data.attributes as Record<string, unknown> : {};
      return newQueryBranch('found', { id: data.id, ...(typeof attributes.email === 'string' && attributes.email ? { email: attributes.email } : {}) });
    } catch { return queryFailure('invalidResponse', 'PROTOCOL', 'Klaviyo returned an invalid profile'); }
  }
}

/** Submit one deduplicated Klaviyo event. */
export class CreateEventOperation implements Mutation<CreateEventInput, EventAcceptance> {
  /** Bind an authenticated client. */ constructor(private readonly client: KlaviyoClient) {}
  /** Return generated manifest metadata. */ definition() { return CreateEventDefinition; }
  /** Derive the provider unique ID from Dex's stable Call ID. */ idempotencyKey(callId: string): string { return createHash('sha256').update(callId).digest('hex'); }
  /** Validate, send, and classify one event submission attempt. */
  async invoke(call: Call, input: CreateEventInput): Promise<MutationAttempt<EventAcceptance>> {
    if (!validEmail(input.email) || !input.metricName.trim() || !call.idempotencyKey) return mutationFailure('defect', 'VALIDATION', 'email, metric name, and stable call identity are required');
    const payload = Buffer.from(JSON.stringify({ data: { type: 'event', attributes: { properties: input.properties ?? null, metric: { data: { type: 'metric', attributes: { name: input.metricName } } }, profile: { data: { type: 'profile', attributes: { email: input.email } } }, unique_id: call.idempotencyKey } } }), 'utf8');
    if (payload.length > 1 << 20) return mutationFailure('defect', 'VALIDATION', 'event payload is invalid or too large');
    let response;
    try { response = await this.client.request(call, 'POST', '/events', payload); }
    catch (error) {
      if (error instanceof InvalidKlaviyoCredentialError) return mutationFailure('defect', 'AUTHENTICATION', 'Klaviyo credentials are unavailable or invalid');
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (isRetryStatus(error.status)) return newMutationRetry(failure('createEvent', 'AVAILABILITY', 'Klaviyo event submission is temporarily unavailable'), parseRetryAfter(retryHeader(error.headers ?? {}), this.client.now()));
        if (error.status === 202) return newMutationBranch('accepted', { accepted: true });
        return mutationFailure('providerRejected', rejectionKind(error.status), `Klaviyo rejected event submission with HTTP ${error.status}`);
      }
      return newMutationRetry(failure('createEvent', 'TRANSPORT', 'Klaviyo event response is unavailable'));
    }
    if (isRetryStatus(response.status)) return newMutationRetry(failure('createEvent', 'AVAILABILITY', 'Klaviyo event submission is temporarily unavailable'), parseRetryAfter(retryHeader(response.headers), this.client.now()));
    if (response.status !== 202) return mutationFailure('providerRejected', rejectionKind(response.status), `Klaviyo rejected event submission with HTTP ${response.status}`);
    return newMutationBranch('accepted', { accepted: true });
  }
}
