// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError, TriggerDeliveryAbortedError, resolveCredential, type ConnectionRef, type TriggerContext, type TriggerEvent, type TriggerRef } from '@superdurable/connectors-sdk';
import { markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest, type ChangeFetcher, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { parseRetryAfter } from '@superdurable/connectors-sdk/providerhttp';
import type { LinkedInClient, LinkedInResponse } from './client.js';
import { goField, parseGoJSON } from './go_json.js';
import { goJSON } from './go_text.js';
import {
  InvalidUserInfoError, encodeProfileClaimsAsGoJSON, newProfileChangedTriggerEvent, profileClaimsFromUserInfoBody, profileHash, withoutEmptyClaims,
  type ProfileChangedEvent, type ProfileClaims,
} from './profile_changed.js';

/** Versions the committed cursor token; another value records a new baseline. */
const profileCursorVersion = 1;

/** The committed token; sequence counts delivered changes for event IDs. */
interface ProfileCursor { version: number; subject: string; hash: string; sequence: number; profile: ProfileClaims }

/** Encode the token as Go's json.Marshal of profileCursor, byte for byte. */
export function encodeProfileCursor(position: ProfileCursor): Cursor {
  const fields = [`"version":${position.version}`, `"subject":${goJSON(position.subject)}`, `"hash":${goJSON(position.hash)}`];
  if (position.sequence) fields.push(`"sequence":${position.sequence}`);
  fields.push(`"profile":${encodeProfileClaimsAsGoJSON(position.profile)}`);
  return { token: `{${fields.join(',')}}` };
}

/** Decode the token as Go's json.Unmarshal into profileCursor does, or undefined when Go would fail. */
function decodeProfileCursor(token: string): ProfileCursor | undefined {
  let value: unknown;
  try { value = parseGoJSON(token); } catch { return undefined; }
  const empty: ProfileCursor = { version: 0, subject: '', hash: '', sequence: 0, profile: { subject: '', emailVerified: false } };
  if (value === null) return empty;
  if (typeof value !== 'object' || Array.isArray(value)) return undefined;
  const raw = value as Record<string, unknown>;
  const version = goInteger(goField(raw, 'version')), sequence = goInteger(goField(raw, 'sequence'));
  const subject = goString(goField(raw, 'subject')), hash = goString(goField(raw, 'hash')), profile = decodeStoredProfile(goField(raw, 'profile'));
  if (version === undefined || sequence === undefined || subject === undefined || hash === undefined || !profile) return undefined;
  return { version, subject, hash, sequence, profile };
}

function decodeStoredProfile(value: unknown): ProfileClaims | undefined {
  if (value === undefined || value === null) return { subject: '', emailVerified: false };
  if (typeof value !== 'object' || Array.isArray(value)) return undefined;
  const raw = value as Record<string, unknown>;
  const text = (name: string) => goString(goField(raw, name));
  const [subject, name, givenName, familyName, email, pictureUrl, locale] = ['subject', 'name', 'givenName', 'familyName', 'email', 'pictureUrl', 'locale'].map(text);
  const verified = goField(raw, 'emailVerified');
  if ([subject, name, givenName, familyName, email, pictureUrl, locale].includes(undefined) || (verified != null && typeof verified !== 'boolean')) return undefined;
  return withoutEmptyClaims({
    subject: subject!, name: name!, givenName: givenName!, familyName: familyName!, email: email!, emailVerified: verified === true, pictureUrl: pictureUrl!, locale: locale!,
  });
}

/** A JSON string, '' for an absent or null value, or undefined for another type. */
function goString(value: unknown): string | undefined {
  if (value === undefined || value === null) return '';
  return typeof value === 'string' ? value : undefined;
}

/** A JSON integer, 0 for an absent or null value, or undefined for another value. */
function goInteger(value: unknown): number | undefined {
  if (value === undefined || value === null) return 0;
  return typeof value === 'number' && Number.isSafeInteger(value) ? value : undefined;
}

/** Compares each UserInfo profile with the last; LinkedIn has no profile change feed. */
export class ProfileFetcher implements ChangeFetcher<TriggerEvent<ProfileChangedEvent>> {
  /** Bind the client, connection, and Trigger identity. */
  constructor(private readonly client: LinkedInClient, private readonly connection: ConnectionRef, private readonly trigger: TriggerRef) {}

  /** Read the current profile and keep it as the baseline without delivering it. */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    const profile = await this.readProfile(context);
    return encodeProfileCursor({ version: profileCursorVersion, subject: profile.subject, hash: profileHash(profile), sequence: 0, profile });
  }

  /** Return one change when the profile hash moved; another member's profile or an undecodable cursor reseeds. */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<ProfileChangedEvent>>> {
    const position = decodeProfileCursor(cursor.token);
    if (!position || position.version !== profileCursorVersion || !position.subject || position.subject !== position.profile.subject || !position.hash) {
      throw markCursorInvalid(new Error('LinkedIn profile cursor is not decodable'));
    }
    const profile = await this.readProfile(context);
    if (profile.subject !== position.subject) throw markCursorInvalid(new Error('LinkedIn connection now belongs to another member than the stored profile'));
    const hash = profileHash(profile);
    // An unchanged hash still stores the latest picture URL, whose signed query LinkedIn may have renewed.
    const next: ProfileCursor = { ...position, profile };
    const changes: Array<TriggerEvent<ProfileChangedEvent>> = [];
    if (hash !== position.hash) {
      changes.push(newProfileChangedTriggerEvent(position.hash, hash, position.sequence, position.profile, profile, this.client.currentTime()));
      next.hash = hash;
      next.sequence = position.sequence + 1;
    }
    return { changes, next: encodeProfileCursor(next) };
  }

  /** Send one UserInfo request and map LinkedIn's statuses to the poll kit's signals. */
  private async readProfile(context: TriggerContext): Promise<ProfileClaims> {
    await waitForPollRequest(context);
    const call = newTriggerCredentialCall(this.connection, this.trigger, context.signal);
    let token: string;
    try { token = (await resolveCredential(this.client.credentials, call, this.client.refreshDriver, context.signal)).accessToken.reveal(); }
    catch (error) {
      if (error instanceof ReauthorizationRequiredError) throw new Error('LinkedIn connection requires reauthorization; reconnect the connection');
      throw new Error('LinkedIn Trigger credentials are unavailable');
    }
    if (!token) throw new Error('LinkedIn Trigger credentials are unavailable');
    let response: LinkedInResponse;
    try { response = await this.client.requestProfile(call, token); }
    catch {
      if (context.signal.aborted) throw new TriggerDeliveryAbortedError();
      throw new Error('LinkedIn UserInfo request could not reach LinkedIn');
    }
    if (response.isTooLarge) throw new Error('LinkedIn UserInfo response exceeds the configured size limit');
    const status = response.status;
    // LinkedIn limits are daily; without Retry-After the runner backs off exponentially.
    if (status === 429) throw markRateLimited(new Error('LinkedIn rate limit was reached'), parseRetryAfter(header(response, 'retry-after'), this.client.currentTime()));
    if (status === 401) throw new Error('LinkedIn authorization is invalid or revoked; reconnect the connection');
    if (status === 403) throw new Error('LinkedIn authorization is forbidden or lacks the openid or profile scope');
    if (status < 200 || status >= 300) throw new Error(`LinkedIn UserInfo request returned HTTP ${status}`);
    let profile: ProfileClaims | undefined;
    try { profile = profileClaimsFromUserInfoBody(response.body); }
    catch (error) {
      if (error instanceof InvalidUserInfoError) throw new Error('LinkedIn returned an invalid UserInfo response');
      throw error;
    }
    if (!profile) throw new Error('LinkedIn returned a UserInfo response without a valid sub claim');
    return profile;
  }
}

function header(response: LinkedInResponse, name: string): string {
  const value = response.headers[name];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}
