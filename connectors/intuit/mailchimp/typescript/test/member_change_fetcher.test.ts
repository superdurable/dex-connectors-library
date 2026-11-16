// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// The memberChanged fetcher's baseline, lookback, paging, filter, and failure rules, as Go tests them.
import { newTriggerContext } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { describe, expect, it } from 'vitest';
import { MemberChangedTriggerConfigurationSchema, MemberChangedTriggerDefinition, MemberStatuses } from '../src/index.js';
import { MemberChangeFetcher, MemberPollPageSize } from '../src/member_change_fetcher.js';
import { memberChangeID } from '../src/member_snapshot.js';
import { APIKey, MailchimpAudienceFake, audienceId, newFakeClient } from './mailchimp_audience_fake.js';

const reference = { provider: 'mailchimp', name: 'mailchimp-fetcher-test' };
const context = newTriggerContext();

function fetcherFor(audience: MailchimpAudienceFake, ...statuses: string[]): MemberChangeFetcher {
  return new MemberChangeFetcher(newFakeClient(audience), reference, MemberChangedTriggerDefinition.trigger, { audienceId, statuses });
}

/** Fetch pages until the pass ends and return the delivered emails and the final cursor. */
async function drain(fetcher: MemberChangeFetcher, cursor: Cursor): Promise<[string[], Cursor]> {
  const emails: string[] = [];
  for (let page = 0; page < 100; page += 1) {
    const changes = await fetcher.fetchChanges(context, cursor);
    for (const change of changes.changes) {
      expect(change.id).toBe(memberChangeID(audienceId, change.payload.member));
      emails.push(change.payload.member.emailAddress);
    }
    cursor = changes.next;
    if (!changes.hasMorePages) return [emails, cursor];
  }
  throw new Error('the pass never ended');
}

describe('memberChanged fetcher', () => {
  it('commits no snapshot for an empty audience, as Go does', async () => {
    const audience = new MailchimpAudienceFake(audienceId);
    const fetcher = fetcherFor(audience);
    const seeded = await fetcher.seedCursor(context);
    expect(seeded.token).toBe('{"audienceId":"57afe96172"}');
    expect(seeded.snapshot, "Go's cursor document holds a null snapshot").toBeUndefined();
    audience.upsert('ben@example.com', 'subscribed', 'Ben');
    const [emails, cursor] = await drain(fetcher, seeded);
    expect(emails).toEqual(['ben@example.com']);
    expect(cursor.snapshot).toBeDefined();
  });

  it('records a baseline and delivers later changes once', async () => {
    const audience = new MailchimpAudienceFake(audienceId);
    audience.upsert('ben@example.com', 'subscribed', 'Ben');
    audience.upsert('cara@example.com', 'subscribed', 'Cara');
    const fetcher = fetcherFor(audience);
    let cursor = await fetcher.seedCursor(context);
    expect(cursor.token).toContain('"baseline":"2026-11-01T09:00:02Z"');
    let emails: string[];
    [emails, cursor] = await drain(fetcher, cursor);
    expect(emails, 'the baseline is never delivered').toEqual([]);
    audience.upsert('dana@example.com', 'pending', 'Dana');
    audience.upsert('ben@example.com', 'unsubscribed', 'Ben');
    [emails, cursor] = await drain(fetcher, cursor);
    expect(emails).toEqual(['dana@example.com', 'ben@example.com']);
    expect((await drain(fetcher, cursor))[0], 'a pass over the lookback window delivers each version once').toEqual([]);
  });

  it('delivers changes in the baseline second after the seed', async () => {
    const audience = new MailchimpAudienceFake(audienceId);
    const seededAt = Date.UTC(2026, 10, 1, 9, 30);
    audience.upsertAt('ben@example.com', 'subscribed', 'Ben', seededAt);
    audience.upsertAt('cara@example.com', 'subscribed', 'Cara', seededAt);
    audience.upsertAt('old@example.com', 'subscribed', 'Old', seededAt - 60_000);
    const fetcher = fetcherFor(audience);
    const seeded = await fetcher.seedCursor(context);
    expect(seeded.snapshot?.entries).toHaveLength(2);
    audience.upsertAt('dana@example.com', 'subscribed', 'Dana', seededAt);
    audience.upsertAt('ben@example.com', 'subscribed', 'Benjamin', seededAt);
    const [emails, cursor] = await drain(fetcher, seeded);
    expect(emails).toEqual(['ben@example.com', 'dana@example.com']);
    expect((await drain(fetcher, cursor))[0]).toEqual([]);
  });

  it('skips the second of a seed whose whole page shares it', async () => {
    const audience = new MailchimpAudienceFake(audienceId);
    for (let index = 0; index < MemberPollPageSize + 3; index += 1) audience.upsertAt(`import${String(index).padStart(4, '0')}@example.com`, 'subscribed', 'Import', Date.UTC(2026, 10, 1, 10));
    const fetcher = fetcherFor(audience);
    const cursor = await fetcher.seedCursor(context);
    expect(cursor.token).toContain('"baseline":"2026-11-01T10:00:01Z"');
    expect((await drain(fetcher, cursor))[0]).toEqual([]);
  });

  it('delivers a late-listed change inside the lookback', async () => {
    const audience = new MailchimpAudienceFake(audienceId);
    audience.upsert('ben@example.com', 'subscribed', 'Ben');
    const fetcher = fetcherFor(audience);
    let cursor = await fetcher.seedCursor(context);
    for (let index = 0; index < 10; index += 1) audience.upsert('cara@example.com', 'subscribed', 'Cara');
    [, cursor] = await drain(fetcher, cursor);
    audience.upsertAt('late@example.com', 'subscribed', 'Late', audience.clockMs - 3000);
    expect((await drain(fetcher, cursor))[0]).toEqual(['late@example.com']);
  });

  it('pages contacts sharing one second by offset and delivers each once', async () => {
    const audience = new MailchimpAudienceFake(audienceId);
    const fetcher = fetcherFor(audience);
    const seeded = await fetcher.seedCursor(context);
    for (let index = 0; index < 2 * MemberPollPageSize + 7; index += 1) {
      audience.upsertAt(`import${String(index).padStart(4, '0')}@example.com`, 'subscribed', 'Import', Date.UTC(2026, 10, 1, 10) + Math.floor(index / 300) * 1000);
    }
    const [emails, cursor] = await drain(fetcher, seeded);
    expect(emails).toHaveLength(2 * MemberPollPageSize + 7);
    expect(new Set(emails).size, 'no contact is delivered twice').toBe(emails.length);
    expect(cursor.token).toContain('"watermark":"2026-11-01T10:00:01Z"');
    expect(cursor.token, 'a finished pass keeps no offset').not.toContain('passOffset');
  });

  it('skips other statuses but still passes them', async () => {
    const audience = new MailchimpAudienceFake(audienceId);
    const fetcher = fetcherFor(audience, 'unsubscribed', 'cleaned');
    const seeded = await fetcher.seedCursor(context);
    audience.upsert('ben@example.com', 'subscribed', 'Ben');
    audience.upsert('cara@example.com', 'unsubscribed', 'Cara');
    const [emails, cursor] = await drain(fetcher, seeded);
    expect(emails).toEqual(['cara@example.com']);
    audience.upsert('ben@example.com', 'cleaned', 'Ben');
    expect((await drain(fetcher, cursor))[0]).toEqual(['ben@example.com']);
  });

  it('classifies provider failures for the kit', async () => {
    const audience = new MailchimpAudienceFake(audienceId);
    const fetcher = fetcherFor(audience);
    const cursor = await fetcher.seedCursor(context);
    const failure = async () => fetcher.fetchChanges(context, cursor).then(() => undefined, (error: unknown) => error);
    audience.failNextList({ status: 429, retryAfter: '7' });
    const limited = await failure();
    expect(limited).toBeInstanceOf(RateLimitedError);
    expect((limited as RateLimitedError).retryAfterMs).toBe(7000);
    audience.failNextList({ status: 403, withoutDocument: true });
    const throttled = await failure();
    expect(throttled, 'a document-less 403 is Mailchimp throttling').toBeInstanceOf(RateLimitedError);
    expect((throttled as RateLimitedError).retryAfterMs).toBe(0);
    audience.failNextList({ status: 400 });
    expect(isCursorInvalid(await failure()), 'a 400 for the cursor query reseeds').toBe(true);
    for (const status of [403, 404, 503]) {
      audience.failNextList({ status });
      const error = await failure();
      expect(error).toBeInstanceOf(Error);
      expect(isCursorInvalid(error) || error instanceof RateLimitedError, String(status)).toBe(false);
      expect(String(error)).not.toContain(APIKey);
    }
  });

  it.each(['not json', '{"audienceId":"0000000000"}', '{"audienceId":"57afe96172","watermark":"yesterday"}', '{"audienceId":"57afe96172","passOffset":-1}'])(
    'treats the foreign or corrupt cursor %s as invalid', async token => {
      const audience = new MailchimpAudienceFake(audienceId);
      const error = await fetcherFor(audience).fetchChanges(context, { token }).then(() => undefined, (caught: unknown) => caught);
      expect(isCursorInvalid(error)).toBe(true);
      expect(audience.requests, 'an invalid cursor sends no request').toEqual([]);
    });

  it('asks for ascending changes after the lookback', async () => {
    const audience = new MailchimpAudienceFake(audienceId);
    audience.upsert('ben@example.com', 'subscribed', 'Ben');
    const fetcher = fetcherFor(audience);
    let cursor = await fetcher.seedCursor(context);
    audience.upsertAt('cara@example.com', 'subscribed', 'Cara', Date.UTC(2026, 10, 1, 9, 10));
    [, cursor] = await drain(fetcher, cursor);
    await drain(fetcher, cursor);
    const requests = audience.requests;
    expect(requests).toHaveLength(3);
    expect(requests[0]).toContain('sort_dir=DESC');
    expect(requests[1], 'the first pass lists the baseline second').toContain('since_last_changed=2026-11-01T09%3A00%3A00%2B00%3A00');
    expect(requests[2], 'later passes reread five minutes').toContain('since_last_changed=2026-11-01T09%3A05%3A00%2B00%3A00');
    for (const request of requests.slice(1)) expect(request.includes('sort_dir=ASC') && request.includes('count=250'), request).toBe(true);
  });

  it('validates the configuration', () => {
    for (const configuration of [{}, { audienceId: '' }, { audienceId: '57afe96172/x' }, { audienceId, statuses: ['gone'] }, { audienceId, statuses: ['pending', 'pending'] }, { audienceId, extra: true }]) {
      expect(MemberChangedTriggerConfigurationSchema.safeParse(configuration).success, JSON.stringify(configuration)).toBe(false);
    }
    expect(MemberChangedTriggerConfigurationSchema.safeParse({ audienceId, statuses: [...MemberStatuses] }).success).toBe(true);
  });

  it('keeps a five-second poll interval floor', () => {
    const audience = new MailchimpAudienceFake(audienceId);
    expect(newFakeClient(audience).pollIntervalMs).toBe(30_000);
    expect(newFakeClient(audience, 5000).pollIntervalMs).toBe(5000);
    expect(() => newFakeClient(audience, 4500)).toThrow('at least 5s');
  });
});
