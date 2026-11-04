// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { GetAuthenticatedProfileDefinition } from './generated/definitions.js';
import type { GetAuthenticatedProfileInput, AuthenticatedProfile } from './generated/models.js';
import type { LinkedInClient, LinkedInResponse } from './client.js';

const emptyProfile: AuthenticatedProfile = { subject: '', verifiedEmail: '' };
const encoder = new TextEncoder();
function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: 'linkedin', operation: 'getAuthenticatedProfile', message }; }
function branch(name: string, kind: Failure['kind'], message: string, requestId = ''): QueryAttempt<AuthenticatedProfile> {
  return newQueryBranch(name, emptyProfile, requestId ? { providerRequestId: requestId } : zeroReceipt(), failure(kind, message));
}
function header(response: LinkedInResponse, name: string): string {
  const value = response.headers[name.toLowerCase()];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}
function requestId(response: LinkedInResponse): string {
  for (const name of ['x-li-uuid', 'x-restli-id', 'x-request-id']) {
    const value = header(response, name).trim();
    if (value && value.length <= 128 && /^[A-Za-z0-9_.:/-]+$/.test(value)) return value;
  }
  return '';
}
function bounded(value: string, maximum: number): string {
  let text = value.trim();
  while (encoder.encode(text).length > maximum) text = text.slice(0, -1);
  return text;
}
function stringField(source: Record<string, unknown>, key: string): string {
  const value = source[key];
  if (value === undefined || value === null) return '';
  if (typeof value !== 'string') throw new TypeError('invalid LinkedIn claim');
  return value;
}
function safeURL(value: string): string {
  value = bounded(value, 2048);
  try {
    const url = new URL(value);
    return url.protocol === 'https:' && url.hostname && !url.username && !url.password ? value : '';
  } catch { return ''; }
}
function locale(value: unknown): string {
  if (typeof value === 'string') return bounded(value, 64);
  if (!value || typeof value !== 'object' || Array.isArray(value)) return '';
  const fields = value as Record<string, unknown>;
  if (fields.language != null && typeof fields.language !== 'string') return '';
  if (fields.country != null && typeof fields.country !== 'string') return '';
  const language = bounded(stringField(fields, 'language'), 16);
  const country = bounded(stringField(fields, 'country'), 16);
  return language && country ? `${language}-${country}` : language || country;
}
function verifiedEmail(value: string, verified: boolean): string {
  const email = value.trim().toLowerCase();
  if (!verified || encoder.encode(email).length > 320 || !email) return '';
  // Address grammar permits quoted local parts and domain literals, as in Go mail.ParseAddress.
  const match = /^(?:[\p{L}\p{N}!#$%&'*+\-/=?^_`{|}~]+(?:\.[\p{L}\p{N}!#$%&'*+\-/=?^_`{|}~]+)*|"(?:[^"\\\r\n]|\\.)+")@(?:[\p{L}\p{N}](?:[\p{L}\p{N}.-]*[\p{L}\p{N}])?|\[[^\]\r\n]+\])$/iu.exec(email);
  return match ? email : '';
}
function decodeProfile(body: Uint8Array): AuthenticatedProfile | 'emailMissing' {
  const claims: unknown = JSON.parse(new TextDecoder().decode(body));
  if (!claims || typeof claims !== 'object' || Array.isArray(claims)) throw new TypeError('invalid LinkedIn UserInfo');
  const source = claims as Record<string, unknown>;
  const subject = stringField(source, 'sub').trim();
  if (!subject || encoder.encode(subject).length > 255) throw new TypeError('invalid LinkedIn subject');
  if (source.email_verified !== undefined && source.email_verified !== null && typeof source.email_verified !== 'boolean') throw new TypeError('invalid LinkedIn verification');
  const email = verifiedEmail(stringField(source, 'email'), source.email_verified === true);
  if (!email) return 'emailMissing';
  const profile: AuthenticatedProfile = { subject, verifiedEmail: email };
  const name = bounded(stringField(source, 'name'), 256);
  const givenName = bounded(stringField(source, 'given_name'), 128);
  const familyName = bounded(stringField(source, 'family_name'), 128);
  const pictureUrl = safeURL(stringField(source, 'picture'));
  const localeValue = locale(source.locale);
  if (name) profile.name = name;
  if (givenName) profile.givenName = givenName;
  if (familyName) profile.familyName = familyName;
  if (pictureUrl) profile.pictureUrl = pictureUrl;
  if (localeValue) profile.locale = localeValue;
  return profile;
}

/** LinkedIn authenticated UserInfo query. */
export class GetAuthenticatedProfileOperation implements Query<GetAuthenticatedProfileInput, AuthenticatedProfile> {
  /** Bind the client. */ constructor(private readonly client: LinkedInClient) {}
  /** Return the manifest definition. */ definition() { return GetAuthenticatedProfileDefinition; }
  /** Read the authenticated member profile. */
  async invoke(call: Call, _input: GetAuthenticatedProfileInput): Promise<QueryAttempt<AuthenticatedProfile>> {
    void _input;
    const token = await this.client.resolveAccessToken(call);
    if (!token) return branch('defect', 'AUTHENTICATION', 'LinkedIn authorization is unavailable or revoked');
    let response: LinkedInResponse;
    try { response = await this.client.requestProfile(call, token); }
    catch { return newQueryRetry(failure('AVAILABILITY', 'LinkedIn is unavailable')); }
    const id = requestId(response);
    if (response.isTooLarge) return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'LinkedIn UserInfo response exceeds the configured size limit', id);
    if (response.status === 429) {
      const value = header(response, 'retry-after');
      const seconds = /^[+-]?\d+$/.test(value) ? Number(value) : 0;
      return newQueryRetry(failure('RATE_LIMIT', 'LinkedIn rate limit was reached'), Number.isSafeInteger(seconds) && seconds > 0 ? seconds * 1000 : 60_000);
    }
    if (response.status >= 500) return newQueryRetry(failure('AVAILABILITY', 'LinkedIn is unavailable'));
    if (response.status === 401) return branch('authorizationRevoked', 'AUTHENTICATION', 'LinkedIn authorization is invalid or revoked', id);
    if (response.status === 403) return branch('insufficientScope', 'AUTHORIZATION', 'LinkedIn authorization is forbidden or lacks scope', id);
    if (response.status === 404) return branch('notFound', 'NOT_FOUND', 'LinkedIn member was not found', id);
    if (response.status < 200 || response.status >= 300) return branch('providerRejected', 'PROVIDER_REJECTION', 'LinkedIn rejected the UserInfo request', id);
    try {
      const profile = decodeProfile(response.body);
      return profile === 'emailMissing'
        ? branch('verifiedEmailRequired', 'AUTHENTICATION', 'LinkedIn verified email is required', id)
        : newQueryBranch('profileLoaded', profile, id ? { providerRequestId: id } : zeroReceipt());
    } catch { return branch('invalidResponse', 'PROTOCOL', 'LinkedIn returned an invalid UserInfo response', id); }
  }
}
