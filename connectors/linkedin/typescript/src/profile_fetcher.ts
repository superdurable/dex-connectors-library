// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectionRef, TriggerContext, TriggerEvent, TriggerRef } from '@superdurable/connectors-sdk';
import { markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest, type ChangeFetcher, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { parseRetryAfter } from '@superdurable/connectors-sdk/providerhttp';
import type { LinkedInClient, LinkedInResponse } from './client.js';
import { newProfileChangedEvent, profileHash, type ProfileChangedEvent, type ProfileClaims } from './profile_change.js';
import { decodeProfileCursor, encodeProfileCursor } from './profile_cursor.js';
import { decodeProfileClaims } from './operations.js';

/** Polls connected-member UserInfo and compares each response to the committed profile cursor. */
export class ProfileFetcher implements ChangeFetcher<TriggerEvent<ProfileChangedEvent>> {
  /** Bind a connection and Trigger identity for renewable credential calls. */
  constructor(private readonly client: LinkedInClient, private readonly connection: ConnectionRef, private readonly trigger: TriggerRef) {}

  /** Read a baseline without delivering existing profile claims. */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    const profile = await this.readProfile(context);
    return { token: encodeProfileCursor({ version: 1, subject: profile.subject, hash: profileHash(profile), sequence: 0, profile }) };
  }

  /** Return one change, or reseed when the cursor/member is invalid. */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<ProfileChangedEvent>>> {
    const position = decodeProfileCursor(cursor.token);
    if (!position) throw markCursorInvalid(new Error('LinkedIn profile cursor is not decodable'));
    const profile = await this.readProfile(context);
    if (profile.subject !== position.subject) throw markCursorInvalid(new Error('LinkedIn connection now belongs to another member than the stored profile'));
    const currentHash = profileHash(profile);
    const changes = currentHash === position.hash ? [] : [newProfileChangedEvent(position.hash, currentHash, position.sequence, position.profile, profile, this.client.now())];
    return { changes, next: { token: encodeProfileCursor({ ...position, hash: currentHash, sequence: position.sequence + changes.length, profile }) } };
  }

  private async readProfile(context: TriggerContext): Promise<ProfileClaims> {
    await waitForPollRequest(context);
    const call = newTriggerCredentialCall(this.connection, this.trigger, context.signal);
    const token = await this.client.resolveTriggerAccessToken(call);
    let response: LinkedInResponse;
    try { response = await this.client.requestProfile(call, token); }
    catch { throw new Error('LinkedIn UserInfo request could not reach LinkedIn'); }
    if (context.signal.aborted) throw new Error('LinkedIn profile poll was cancelled');
    if (response.isTooLarge) throw new Error('LinkedIn UserInfo response exceeds the configured size limit');
    if (response.status === 429) throw markRateLimited(new Error('LinkedIn rate limit was reached'), parseRetryAfter(header(response, 'retry-after'), this.client.now()));
    if (response.status === 401) throw new Error('LinkedIn authorization is invalid or revoked; reconnect the connection');
    if (response.status === 403) throw new Error('LinkedIn authorization is forbidden or lacks the openid or profile scope');
    if (response.status < 200 || response.status >= 300) throw new Error(`LinkedIn UserInfo request returned HTTP ${response.status}`);
    try { return decodeProfileClaims(response.body); }
    catch (error) {
      if (error instanceof RangeError) throw new Error('LinkedIn returned a UserInfo response without a valid sub claim');
      throw new Error('LinkedIn returned an invalid UserInfo response');
    }
  }
}

function header(response: LinkedInResponse, name: string): string {
  const value = response.headers[name];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}
