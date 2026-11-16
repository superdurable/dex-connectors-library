// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// profileChanged fetcher behavior, ported from the Go connector's profile_fetcher tests.
import { newTriggerContext } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid } from '@superdurable/connectors-sdk/pollingtrigger';
import { describe, expect, it } from 'vitest';
import { LinkedInClient, ProfileChangedTriggerDefinition, newProjectConnection } from '../src/index.js';
import { ProfileFetcher } from '../src/profile_fetcher.js';
import { LinkedInProfileFake, adaProfile, newFakeClient, newTestProject } from './linkedin_profile_fake.js';

const reference = { provider: 'linkedin', name: 'linkedin-watcher' };
const context = newTriggerContext();

function newFetcher(fake: LinkedInProfileFake, accessToken?: string): ProfileFetcher {
  return new ProfileFetcher(newFakeClient(fake, reference.name, undefined, accessToken), reference, ProfileChangedTriggerDefinition.trigger);
}

describe('ProfileFetcher', () => {
  it('seeds without backlog, ignores a re-signed picture, then delivers each change', async () => {
    const fake = new LinkedInProfileFake(adaProfile()), fetcher = newFetcher(fake);
    const seed = await fetcher.seedCursor(context);
    expect(seed.token).toContain('"subject":"782bbtaQ"');
    const unchanged = await fetcher.fetchChanges(context, seed);
    expect(unchanged.changes, 'the baseline is never delivered').toEqual([]);
    expect(unchanged.next.token).toBe(seed.token);

    fake.update(profile => { profile.picture = 'https://media.licdn.com/dms/image/v2/ada/profile-displayphoto-shrink_100_100/0/1?e=2&v=beta&t=second'; });
    const resigned = await fetcher.fetchChanges(context, seed);
    expect(resigned.changes, 'a re-signed URL of the same picture is not a change').toEqual([]);
    expect(resigned.next.token, 'the stored profile keeps the latest picture URL').toContain('t=second');

    fake.update(profile => { profile.name = 'Ada King'; profile.family_name = 'King'; profile.email_verified = false; });
    const page = await fetcher.fetchChanges(context, resigned.next);
    expect(page.changes).toHaveLength(1);
    const change = page.changes[0]!;
    expect(change.payload.subject).toBe('782bbtaQ');
    expect(change.payload.changedFields).toEqual(['name', 'familyName', 'emailVerified']);
    expect(change.payload.previous.name).toBe('Ada Lovelace');
    expect(change.payload.previous.emailVerified).toBe(true);
    expect(change.payload.current.name).toBe('Ada King');
    expect(change.payload.current.email, 'an unverified email is still reported').toBe('ada@example.com');
    expect(change.payload.current.emailVerified).toBe(false);
    expect(change.id).toMatch(/^linkedin-profile-[0-9a-f]{32}$/);

    const refetched = await fetcher.fetchChanges(context, resigned.next);
    expect(refetched.changes[0]?.id, 'a refetch from the same cursor keeps the event ID').toBe(change.id);
    expect(refetched.next.token).toBe(page.next.token);
  });

  it('gives a back-and-forth edit a new event ID each time', async () => {
    const fake = new LinkedInProfileFake(adaProfile()), fetcher = newFetcher(fake);
    let cursor = await fetcher.seedCursor(context);
    const seen = new Set<string>();
    for (const locale of ['fr-FR', 'en-US', 'fr-FR']) {
      fake.update(profile => { profile.locale = locale; });
      const page = await fetcher.fetchChanges(context, cursor);
      expect(page.changes.map(change => change.payload.changedFields)).toEqual([['locale']]);
      expect(seen.has(page.changes[0]!.id), `locale ${locale} reuses an earlier event ID`).toBe(false);
      seen.add(page.changes[0]!.id);
      cursor = page.next;
    }
  });

  it('reseeds for another member or an undecodable cursor', async () => {
    const fake = new LinkedInProfileFake(adaProfile()), fetcher = newFetcher(fake);
    const seed = await fetcher.seedCursor(context);
    fake.update(profile => { profile.sub = '9aQzr21'; profile.name = 'Grace Hopper'; });
    await expect(fetcher.fetchChanges(context, seed).catch(error => isCursorInvalid(error)), 'a connection re-authorized as another member reseeds').resolves.toBe(true);
    for (const token of ['', 'not json', '{"version":2,"subject":"782bbtaQ","hash":"x","profile":{"subject":"782bbtaQ"}}', '{"version":1,"subject":"782bbtaQ","hash":"x","profile":{"subject":"other"}}']) {
      await expect(fetcher.fetchChanges(context, { token }).catch(error => isCursorInvalid(error)), token).resolves.toBe(true);
    }
  });

  it('maps provider failures to poll signals', async () => {
    const fake = new LinkedInProfileFake(adaProfile()), fetcher = newFetcher(fake);
    const seed = await fetcher.seedCursor(context);
    const failure = async () => { try { await fetcher.fetchChanges(context, seed); } catch (error) { return error; } throw new Error('the poll succeeded'); };
    fake.failNext(429, '120');
    expect(await failure()).toMatchObject({ retryAfterMs: 120_000 });
    fake.failNext(429);
    const unpaced = await failure();
    expect(unpaced, 'without Retry-After the runner backs off exponentially').toBeInstanceOf(RateLimitedError);
    expect((unpaced as RateLimitedError).retryAfterMs).toBe(0);
    for (const [status, message] of [[403, 'forbidden'], [500, 'HTTP 500'], [400, 'HTTP 400']] as const) {
      fake.failNext(status);
      const error = await failure();
      expect((error as Error).message).toContain(message);
      expect(isCursorInvalid(error)).toBe(false);
      expect(error).not.toBeInstanceOf(RateLimitedError);
    }
    const revoked = await newFetcher(fake, 'revoked-' + 'grant').fetchChanges(context, seed).then(() => new Error('the poll succeeded'), (error: unknown) => error as Error);
    expect(revoked.message, 'a 401 is a failed poll that backs off, not a reseed').toContain('invalid or revoked');
    expect(isCursorInvalid(revoked)).toBe(false);
  });

  it('rejects a poll interval under one minute and reads the stored one', async () => {
    const credentials = { resolve: async () => { throw new Error('unused'); } } as never;
    expect(() => new LinkedInClient({ pollIntervalMs: 59_000 }, credentials)).toThrow('pollInterval must be at least 1m0s');
    expect(new LinkedInClient({}, credentials).pollIntervalMs).toBe(900_000);
    for (const [stored, milliseconds] of [[3_600_000_000_000, 3_600_000], ['30m', 1_800_000]] as const) {
      const { project } = await newTestProject('linkedin-watcher', [], undefined, stored);
      expect((newProjectConnection(project, 'linkedin-watcher').operations as LinkedInClient).pollIntervalMs).toBe(milliseconds);
    }
    const { project } = await newTestProject('linkedin-watcher', [], undefined, 30_000_000_000);
    expect(() => newProjectConnection(project, 'linkedin-watcher')).toThrow('at least 1m0s');
  });
});
