// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import type { TriggerEvent } from '@superdurable/connectors-sdk';
import { z } from 'zod';
import { goBoundedString, goCanonicalEmailAddress, goDecodeUTF8, goJSON, goTrimSpace, utf8Length } from './go_text.js';
import { goField, parseGoJSON } from './go_json.js';
import { goSafeURL, pictureIdentity } from './go_url.js';

/** The name claim. */ export const ProfileFieldName = 'name';
/** The given_name claim. */ export const ProfileFieldGivenName = 'givenName';
/** The family_name claim. */ export const ProfileFieldFamilyName = 'familyName';
/** The email claim. */ export const ProfileFieldEmail = 'email';
/** The email_verified claim. */ export const ProfileFieldEmailVerified = 'emailVerified';
/** The picture claim; only the address without its query counts, so a re-signed URL of one picture is no change. */
export const ProfileFieldPictureURL = 'pictureUrl';
/** The locale claim. */ export const ProfileFieldLocale = 'locale';

/** Names one tracked claim in ProfileChangedEvent.changedFields, by its ProfileClaims field, in Go's order. */
export const ProfileFieldSchema = z.enum([ProfileFieldName, ProfileFieldGivenName, ProfileFieldFamilyName, ProfileFieldEmail, ProfileFieldEmailVerified, ProfileFieldPictureURL, ProfileFieldLocale]);
/** One tracked profile claim. */
export type ProfileField = z.infer<typeof ProfileFieldSchema>;

/**
 * The connected member's bounded LinkedIn OpenID Connect UserInfo profile, with the Go connector's JSON
 * fields. Unlike AuthenticatedProfile it keeps an unverified or absent email, since a change of verification
 * is itself a profile change. Empty strings are omitted, as Go's omitempty does.
 */
export const ProfileClaimsSchema = z.object({
  /** LinkedIn's sub claim: the member's identifier, stable for this LinkedIn app. */ subject: z.string(),
  /** The full name. */ name: z.string().optional(),
  /** The first name. */ givenName: z.string().optional(),
  /** The last name. */ familyName: z.string().optional(),
  /** The lower-cased primary email address; omitted when LinkedIn returns none or an invalid one. */ email: z.string().optional(),
  /** LinkedIn's email_verified claim; false when LinkedIn omits it. */ emailVerified: z.boolean(),
  /** The HTTPS profile picture URL as Go's url.URL writes it, including any signed query. */ pictureUrl: z.string().optional(),
  /** The member's locale, such as en-US. */ locale: z.string().optional(),
});
/** The connected member's profile claims. */
export type ProfileClaims = z.infer<typeof ProfileClaimsSchema>;

/** One change of the connected member's own profile claims between two polls. */
export const ProfileChangedEventSchema = z.object({
  /** The member whose profile changed; the connection's own member, never another one. */ subject: z.string(),
  /** The profile the previous poll returned. */ previous: ProfileClaimsSchema,
  /** The profile this poll returned. */ current: ProfileClaimsSchema,
  /** The claims that differ, in the order of the ProfileField constants. */ changedFields: z.array(ProfileFieldSchema),
});
/** One profileChanged delivery. */
export type ProfileChangedEvent = z.infer<typeof ProfileChangedEventSchema>;

/** The profileChanged binding configuration. The binding always watches the connected member's own profile, so it has no field. */
export const ProfileChangedTriggerConfigurationSchema = z.object({}).strict();
/** The empty profileChanged configuration. */
export type ProfileChangedTriggerConfiguration = z.infer<typeof ProfileChangedTriggerConfigurationSchema>;

/** The claims a profile hash covers, in Go's trackedProfile order; the picture drops its query. */
function trackedProfile(profile: ProfileClaims): Array<[string, string | boolean]> {
  return [
    ['name', profile.name ?? ''], ['givenName', profile.givenName ?? ''], ['familyName', profile.familyName ?? ''], ['email', profile.email ?? ''],
    ['emailVerified', profile.emailVerified], ['picture', pictureIdentity(profile.pictureUrl ?? '')], ['locale', profile.locale ?? ''],
  ];
}

/** The first 16 bytes of SHA-256 over Go's JSON encoding of the tracked claims, hex encoded. */
export function profileHash(profile: ProfileClaims): string {
  const encoded = `{${trackedProfile(profile).map(([name, value]) => `${goJSON(name)}:${goJSON(value)}`).join(',')}}`;
  return createHash('sha256').update(encoded, 'utf8').digest().subarray(0, 16).toString('hex');
}

/** The tracked claims that differ between two profiles. */
export function changedProfileFields(previous: ProfileClaims, current: ProfileClaims): ProfileField[] {
  const before = trackedProfile(previous), after = trackedProfile(current);
  const fields = ProfileFieldSchema.options;
  return fields.filter((_, index) => before[index]![1] !== after[index]![1]);
}

/** The Trigger event ID; the delivered-change count gives a profile edited back and forth new IDs. */
export function profileChangeID(subject: string, previousHash: string, currentHash: string, sequence: number): string {
  const digest = createHash('sha256').update([subject, previousHash, currentHash, String(sequence)].join('\x00'), 'utf8').digest();
  return `linkedin-profile-${digest.subarray(0, 16).toString('hex')}`;
}

/** One profileChanged Trigger event observed at observedAt. */
export function newProfileChangedTriggerEvent(previousHash: string, currentHash: string, sequence: number, previous: ProfileClaims, current: ProfileClaims, observedAt: Date): TriggerEvent<ProfileChangedEvent> {
  return {
    id: profileChangeID(current.subject, previousHash, currentHash, sequence), occurredAt: observedAt,
    payload: { subject: current.subject, previous, current, changedFields: changedProfileFields(previous, current) },
  };
}

/** The UserInfo body was not one JSON document of the documented claim types. */
export class InvalidUserInfoError extends Error {}

/**
 * Decode a UserInfo body as Go's json.Unmarshal into userInfo and bound it as profileClaimsFromUserInfo does.
 * Returns undefined for a missing or invalid sub claim; throws InvalidUserInfoError for another body.
 */
export function profileClaimsFromUserInfoBody(body: Uint8Array): ProfileClaims | undefined {
  let claims: unknown;
  try { claims = parseGoJSON(goDecodeUTF8(body)); } catch { throw new InvalidUserInfoError('invalid JSON'); }
  if (claims === null) return undefined;
  if (typeof claims !== 'object' || Array.isArray(claims)) throw new InvalidUserInfoError('not an object');
  const source = claims as Record<string, unknown>;
  const text = (name: string): string => {
    const value = goField(source, name);
    if (value === undefined || value === null) return '';
    if (typeof value !== 'string') throw new InvalidUserInfoError(`invalid ${name}`);
    return value;
  };
  const verified = goField(source, 'email_verified');
  if (verified !== undefined && verified !== null && typeof verified !== 'boolean') throw new InvalidUserInfoError('invalid email_verified');
  const subject = goTrimSpace(text('sub')), name = text('name'), givenName = text('given_name'), familyName = text('family_name');
  const email = text('email'), picture = text('picture'), locale = goLocale(goField(source, 'locale', true));
  if (!subject || utf8Length(subject) > 255) return undefined;
  return withoutEmptyClaims({
    subject, name: goBoundedString(name, 256), givenName: goBoundedString(givenName, 128), familyName: goBoundedString(familyName, 128),
    email: goCanonicalEmailAddress(email), emailVerified: verified === true, pictureUrl: goSafeURL(picture), locale,
  });
}

/** Drop empty strings, as Go's omitempty does, keeping Go's field order. */
export function withoutEmptyClaims(profile: ProfileClaims): ProfileClaims {
  return {
    subject: profile.subject, ...(profile.name ? { name: profile.name } : {}), ...(profile.givenName ? { givenName: profile.givenName } : {}),
    ...(profile.familyName ? { familyName: profile.familyName } : {}), ...(profile.email ? { email: profile.email } : {}), emailVerified: profile.emailVerified,
    ...(profile.pictureUrl ? { pictureUrl: profile.pictureUrl } : {}), ...(profile.locale ? { locale: profile.locale } : {}),
  };
}

/** Encode the claims as Go's json.Marshal of ProfileClaims does, byte for byte. */
export function encodeProfileClaimsAsGoJSON(profile: ProfileClaims): string {
  const claims = withoutEmptyClaims(profile);
  return `{${Object.entries(claims).map(([name, value]) => `${goJSON(name)}:${goJSON(value as string | boolean)}`).join(',')}}`;
}

/** Go's localeStringFromLinkedInClaim: a bounded string, or language-country from an object; '' otherwise. */
function goLocale(value: unknown): string {
  if (value === undefined || value === null) return '';
  if (typeof value === 'string') return goBoundedString(value, 64);
  if (typeof value !== 'object' || Array.isArray(value)) return '';
  const fields = value as Record<string, unknown>, language = goField(fields, 'language'), country = goField(fields, 'country');
  if ((language != null && typeof language !== 'string') || (country != null && typeof country !== 'string')) return '';
  const boundedLanguage = goBoundedString((language as string | null | undefined) ?? '', 16), boundedCountry = goBoundedString((country as string | null | undefined) ?? '', 16);
  return boundedLanguage && boundedCountry ? `${boundedLanguage}-${boundedCountry}` : boundedLanguage || boundedCountry;
}
