// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, StaticCredentialProvider, newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { MaxChangesPerPage, RateLimitedError, isCursorInvalid, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { encodeTriggerEvent } from '@superdurable/connectors-sdk/projectconfig';
import { describe, expect, it } from 'vitest';
import { MessageReceivedTriggerConfigurationSchema, TwilioClient, decodeMessageReceivedPayload, type Message, type MessageReceivedEvent } from '../src/index.js';
import { InboundMessageFetcher, InboundSnapshotKey, encodeGoQuery, newMessageReceivedTriggerEvent } from '../src/inbound_message_fetcher.js';
import { TwilioMessagesFake } from './twilio_messages_fake.js';

// Split literals keep synthetic SIDs from matching repository secret scanning.
const accountSid = 'AC' + '00112233445566778899aabbccddeeff';
const authToken = 'fetcher-auth-token-0123456789abcd';
const number = '+14155550100';
const otherNumber = '+14155550199';
const customer = '+14155550123';
const reference = { provider: 'twilio', name: 'twilio-messaging' };
const start = Date.UTC(2026, 10, 15, 12, 0, 0);
const context = newTriggerContext(new AbortController().signal);

class FetcherFixture {
  readonly provider = new TwilioMessagesFake(accountSid, accountSid, authToken);
  nowMs = start;
  readonly client: TwilioClient;
  constructor(token = authToken) { this.client = this.newClient(token); }
  newClient(token: string): TwilioClient {
    return new TwilioClient({ accountSid, endpoint: 'http://127.0.0.1:24210/2010-04-01' },
      new StaticCredentialProvider([[reference, { authMethodId: 'auth-token', authToken: new SecretString(token) }]]),
      { send: this.provider.send, now: () => new Date(this.nowMs) });
  }
  fetcher(to: string, client = this.client): InboundMessageFetcher { return new InboundMessageFetcher(client, reference, to); }
  ago(seconds: number): Date { return new Date(this.nowMs - seconds * 1000); }
}

async function fetchAll(fetcher: InboundMessageFetcher, cursor: Cursor): Promise<[TriggerEvent<MessageReceivedEvent>[], Cursor]> {
  const events: TriggerEvent<MessageReceivedEvent>[] = [];
  for (let page = 0; page < 20; page++) {
    const changes = await fetcher.fetchChanges(context, cursor);
    events.push(...changes.changes);
    cursor = changes.next;
    if (!changes.hasMorePages) return [events, cursor];
  }
  throw new Error('fetcher kept reporting more pages');
}
const ids = (events: TriggerEvent<MessageReceivedEvent>[]) => events.map(event => event.id);

describe('messageReceived fetcher parity with the Go connector', () => {
  it('records a baseline and delivers later inbound messages once, oldest first', async () => {
    const fixture = new FetcherFixture();
    fixture.provider.receive(customer, number, 'before the baseline', fixture.ago(30));
    const fetcher = fixture.fetcher(number);
    let cursor = await fetcher.seedCursor(context);
    expect(cursor.token).toBe(`{"accountSid":"${accountSid}","to":"${number}","watermark":${start / 1000}}`);
    expect(cursor.snapshot?.entries).toHaveLength(1);

    fixture.nowMs += 60_000;
    const second = fixture.provider.receive(customer, number, 'STOP', fixture.ago(10));
    const first = fixture.provider.receive(customer, number, 'hello', fixture.ago(20));
    fixture.provider.sendOutbound(number, customer, 'outbound reply', fixture.ago(5));
    fixture.provider.receive(customer, otherNumber, 'to another number', fixture.ago(5));
    const [events, following] = await fetchAll(fetcher, cursor);
    cursor = following;
    expect(ids(events)).toEqual([`twilio-message-${first}`, `twilio-message-${second}`]);
    expect(events[0]?.payload.body).toBe('hello');
    expect(events[0]?.payload.message.from).toBe(customer);
    expect(events[0]?.payload.message.status).toBe('received');
    expect(events[0]?.occurredAt.getTime()).toBe(fixture.nowMs - 20_000);

    const page = await fetcher.fetchChanges(context, cursor);
    expect(page.changes).toEqual([]);
    expect(page.next).toEqual(cursor);
    for (const query of fixture.provider.listRequests) {
      expect([query.To, query.PageSize]).toEqual([number, '200']);
      expect(query['DateSent>']).toBeTruthy();
    }
  });

  it('holds a receiving message until received, and delivers it as-is after the hold limit', async () => {
    const fixture = new FetcherFixture();
    const fetcher = fixture.fetcher('');
    let cursor = await fetcher.seedCursor(context);
    fixture.nowMs += 60_000;
    const mms = fixture.provider.receive(customer, number, 'photo', fixture.ago(50), 'receiving');
    const text = fixture.provider.receive(customer, otherNumber, 'text', fixture.ago(5));
    let events: TriggerEvent<MessageReceivedEvent>[];
    [events, cursor] = await fetchAll(fetcher, cursor);
    expect(ids(events)).toEqual([`twilio-message-${text}`]);
    fixture.nowMs += 5 * 60_000;
    const held = cursor;
    fixture.provider.setStatus(mms, 'received');
    [events] = await fetchAll(fetcher, held);
    expect(ids(events)).toEqual([`twilio-message-${mms}`]);

    fixture.provider.setStatus(mms, 'receiving');
    fixture.nowMs += 10 * 60_000;
    [events] = await fetchAll(fetcher, held);
    expect(ids(events)).toEqual([`twilio-message-${mms}`]);
    expect(events[0]?.payload.message.status).toBe('receiving');
  });

  it('spans provider pages and change pages', async () => {
    const fixture = new FetcherFixture();
    const fetcher = fixture.fetcher(number);
    const cursor = await fetcher.seedCursor(context);
    fixture.nowMs += 60_000;
    for (let index = 0; index < 650; index++) fixture.provider.receive(customer, number, 'bulk', fixture.ago(index % 60));
    const page = await fetcher.fetchChanges(context, cursor);
    expect(page.changes).toHaveLength(MaxChangesPerPage);
    expect(page.hasMorePages).toBe(true);
    const [rest] = await fetchAll(fetcher, page.next);
    expect(rest).toHaveLength(150);
    expect(new Set(ids([...page.changes, ...rest])).size).toBe(650);
  });

  it('stops a date-only listing at the first message before the lookback', async () => {
    const fixture = new FetcherFixture();
    fixture.provider.useDateOnlyFilters();
    for (let index = 0; index < 2500; index++) fixture.provider.sendOutbound(number, customer, 'earlier today', fixture.ago(3600 + index));
    const fetcher = fixture.fetcher('');
    const cursor = await fetcher.seedCursor(context);
    fixture.nowMs += 60_000;
    const reply = fixture.provider.receive(customer, number, 'reply', fixture.ago(10));
    const before = fixture.provider.listRequests.length;
    const [events] = await fetchAll(fetcher, cursor);
    expect(ids(events)).toEqual([`twilio-message-${reply}`]);
    expect(fixture.provider.listRequests.length - before).toBe(1);
  });

  it('narrows a busy window toward the watermark and leaves a held window for the next poll', async () => {
    const fixture = new FetcherFixture();
    const fetcher = fixture.fetcher('');
    const cursor = await fetcher.seedCursor(context);
    const baseline = fixture.nowMs;
    fixture.nowMs += 5 * 60_000;
    const held = fixture.provider.receive(customer, number, 'photo', new Date(baseline + 10_000), 'receiving');
    const early = fixture.provider.receive(customer, number, 'early', new Date(baseline + 5_000));
    for (let index = 0; index < 2100; index++) fixture.provider.sendOutbound(number, customer, 'campaign', fixture.ago(index % 300));
    const page = await fetcher.fetchChanges(context, cursor);
    expect(ids(page.changes)).toEqual([`twilio-message-${early}`]);
    expect(page.hasMorePages).toBe(false);
    fixture.provider.setStatus(held, 'received');
    const [events] = await fetchAll(fetcher, page.next);
    expect(ids(events)).toEqual([`twilio-message-${held}`]);
  });
});

describe('messageReceived failures, resets, and configuration', () => {
  it('pauses on 429, marks a foreign or undecodable cursor invalid, and keeps a rejected credential secret', async () => {
    const fixture = new FetcherFixture();
    const fetcher = fixture.fetcher(number);
    const cursor = await fetcher.seedCursor(context);
    fixture.provider.throttleNextList(7);
    const throttled = await fetcher.fetchChanges(context, cursor).catch((error: unknown) => error);
    expect(throttled).toBeInstanceOf(RateLimitedError);
    expect((throttled as RateLimitedError).retryAfterMs).toBe(7000);
    expect(String((throttled as Error).message)).toContain('20429');

    expect(isCursorInvalid(await fixture.fetcher(otherNumber).fetchChanges(context, cursor).catch((error: unknown) => error))).toBe(true);
    for (const token of ['not json', 'null', '[]', '{"watermark":1.5}', '{"watermark":1.0}', '{"watermark":0}', '{"watermark":"1"}']) {
      expect(isCursorInvalid(await fetcher.fetchChanges(context, { token }).catch((error: unknown) => error)), token).toBe(true);
    }
    const rejected = fixture.fetcher('', fixture.newClient('wrong-token-0123456789abcdef0123'));
    const error = await rejected.seedCursor(context).catch((failure: unknown) => failure) as Error;
    expect(error.message).toContain('HTTP 401');
    expect(error.message).not.toContain('wrong-token');
  });

  it('decodes cursor keys case-insensitively, as Go json.Unmarshal does', async () => {
    const fixture = new FetcherFixture();
    const token = `{"ACCOUNTSID":"${accountSid}","To":"${number}","extra":1,"watermark":${start / 1000}}`;
    const page = await fixture.fetcher(number).fetchChanges(context, { token });
    expect(page.changes).toEqual([]);
    expect(page.next.token).toBe(`{"accountSid":"${accountSid}","to":"${number}","watermark":${start / 1000}}`);
  });

  it('validates the receiving address and poll interval', () => {
    for (const valid of ['', number, `whatsapp:${number}`]) expect(MessageReceivedTriggerConfigurationSchema.safeParse({ to: valid }).success, valid).toBe(true);
    for (const invalid of [' +14155550100', '4155550100', 'MG0123', 'whatsapp:4155550100']) expect(MessageReceivedTriggerConfigurationSchema.safeParse({ to: invalid }).success, invalid).toBe(false);
    expect(() => new TwilioClient({ accountSid, pollIntervalMs: 1000 }, new StaticCredentialProvider([]))).toThrow('pollInterval must be at least 5s');
  });
});

// Printed by the Go connector for the same inputs, so Go and TypeScript replicas share cursors and inboxes.
// Split SID literals keep the fixtures from matching repository secret scanning.
const goSeedToken = '{"accountSid":"AC' + '00112233445566778899aabbccddeeff","to":"whatsapp:+14155550100","watermark":1794744000}';
const goNextToken = '{"accountSid":"AC' + '00112233445566778899aabbccddeeff","to":"whatsapp:+14155550100","watermark":1794744060}';
const goSnapshotKey = '01794744000-SM0123456789abcdef0123456789abcdef';
const goSnapshotHash = '5c9cf9e4e827f98f';
const goFirstQuery = 'DateSent%3E=2026-11-15T11%3A58%3A00Z&PageSize=200&To=whatsapp%3A%2B14155550100';
const goEventPayload = '{"message":{"sid":"SM0123456789abcdef0123456789abcdef","accountSid":"AC' + '00112233445566778899aabbccddeeff","from":"+14155550123","to":"+14155550100","status":"received","direction":"inbound","segmentCount":1,"createdAt":"2026-11-15T12:00:00Z","sentAt":"2026-11-15T12:00:00Z","updatedAt":"2026-11-15T12:00:00Z"},"body":"Hi \\u003c\\u0026\\u003e"}';

describe('messageReceived documents match Go byte for byte', () => {
  it('encodes the cursor token, snapshot keys, and request query as Go does', async () => {
    const fixture = new FetcherFixture();
    const fetcher = fixture.fetcher(`whatsapp:${number}`);
    const seeded = await fetcher.seedCursor(context);
    expect(seeded.token).toBe(goSeedToken);
    expect(seeded.snapshot?.entries).toEqual([]);
    expect(fixture.provider.listRequests[0]).toEqual({ 'DateSent>': '2026-11-15T11:58:00Z', PageSize: '200', To: `whatsapp:${number}` });
    expect(encodeGoQuery({ PageSize: '200', 'DateSent>': '2026-11-15T11:58:00Z', To: `whatsapp:${number}` })).toBe(goFirstQuery);
    const item = new InboundSnapshotKey(goSnapshotKey);
    expect(item.snapshotHash()).toBe(goSnapshotHash);
    const encode = (fetcher as unknown as { encodeCursor(watermark: number, previous: unknown, upserted: InboundSnapshotKey[]): Cursor }).encodeCursor.bind(fetcher);
    const following = encode(1794744060, undefined, [item]);
    expect(following.token).toBe(goNextToken);
    expect(following.snapshot?.entries.map(entry => [entry.key, entry.hash, entry.seen])).toEqual([[goSnapshotKey, goSnapshotHash, 0]]);
    expect(encode(1794744000 + 121, following.snapshot, []).snapshot?.entries).toEqual([]);
  });

  it('replays a message a Go replica recorded and stores one Go decodes', () => {
    const sentAt = '2026-11-15T12:00:00Z';
    const message: Message = { sid: 'SM0123456789abcdef0123456789abcdef', accountSid, from: customer, to: number, status: 'received', direction: 'inbound', segmentCount: 1, createdAt: sentAt, sentAt, updatedAt: sentAt };
    const event = newMessageReceivedTriggerEvent(message, 'Hi <&>', new Date(start + 3_600_000));
    expect([event.id, event.occurredAt.toISOString()]).toEqual(['twilio-message-SM0123456789abcdef0123456789abcdef', '2026-11-15T12:00:00.000Z']);
    expect(decodeMessageReceivedPayload(goEventPayload, Buffer.from(goEventPayload))).toEqual(event.payload);
    const stored = JSON.parse(Buffer.from(encodeTriggerEvent(event)).toString()) as { payload: unknown };
    expect(stored.payload).toEqual(JSON.parse(goEventPayload));
    const undated = newMessageReceivedTriggerEvent({ sid: message.sid, accountSid, to: number, createdAt: '0001-01-01T00:00:00Z', sentAt: '0001-01-01T00:00:00Z', updatedAt: '0001-01-01T00:00:00Z' }, '', new Date(start));
    expect(undated.occurredAt.getTime()).toBe(start);
    expect(undated.payload.body).toBeUndefined();
  });
});
