// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { encodeGoJSONValue } from '@superdurable/connectors-sdk/projectconfig';
import type { TriggerEvent } from '@superdurable/connectors-sdk';

/** Bounded connected-member UserInfo claims, including an unverified email. */
export interface ProfileClaims {
  /** App-scoped LinkedIn member identifier. */ subject: string;
  /** Full name. */ name?: string;
  /** First name. */ givenName?: string;
  /** Last name. */ familyName?: string;
  /** Lowercase primary email, if valid. */ email?: string;
  /** Whether LinkedIn verified the email. */ emailVerified: boolean;
  /** HTTPS picture URL, including a signed query when supplied. */ pictureUrl?: string;
  /** Member locale. */ locale?: string;
}

/** Profile claims changed between consecutive successful polls. */
export interface ProfileChangedEvent {
  /** The connected member. */ subject: string;
  /** Previously committed claims. */ previous: ProfileClaims;
  /** Current claims. */ current: ProfileClaims;
  /** Changed fields in manifest order. */ changedFields: string[];
}

const trackedFields = ['name', 'givenName', 'familyName', 'email', 'emailVerified', 'picture', 'locale'] as const;
function pictureIdentity(address: string): string {
  const fragment = address.split('#', 1)[0] ?? '';
  const question = fragment.indexOf('?');
  return question < 0 ? fragment : fragment.slice(0, question) + (question === fragment.length - 1 ? '?' : '');
}
function tracked(profile: ProfileClaims): readonly (string | boolean)[] {
  return [profile.name ?? '', profile.givenName ?? '', profile.familyName ?? '', profile.email ?? '', profile.emailVerified, pictureIdentity(profile.pictureUrl ?? ''), profile.locale ?? ''];
}

/** Hash tracked claims with Go's JSON escaping and field order. */
export function profileHash(profile: ProfileClaims): string {
  const values = tracked(profile);
  const document = `{${trackedFields.map((field, index) => `${encodeGoJSONValue(field)}:${encodeGoJSONValue(values[index])}`).join(',')}}`;
  return createHash('sha256').update(document).digest('hex').slice(0, 32);
}

/** List profile claim changes, ignoring a picture URL's signed query. */
export function changedProfileFields(previous: ProfileClaims, current: ProfileClaims): string[] {
  const before = tracked(previous), after = tracked(current);
  return trackedFields.flatMap((field, index) => before[index] !== after[index] ? [field === 'picture' ? 'pictureUrl' : field] : []);
}

/** Stable event identity across refetches; sequence distinguishes a profile changed back and forth. */
export function profileChangeId(subject: string, previousHash: string, currentHash: string, sequence: number): string {
  return 'linkedin-profile-' + createHash('sha256').update([subject, previousHash, currentHash, String(sequence)].join('\0')).digest('hex').slice(0, 32);
}

/** Create one observed change event. */
export function newProfileChangedEvent(previousHash: string, currentHash: string, sequence: number, previous: ProfileClaims, current: ProfileClaims, observedAt: Date): TriggerEvent<ProfileChangedEvent> {
  return { id: profileChangeId(current.subject, previousHash, currentHash, sequence), occurredAt: observedAt, payload: { subject: current.subject, previous, current, changedFields: changedProfileFields(previous, current) } };
}
