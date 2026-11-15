// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { SecretString, newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid } from '@superdurable/connectors-sdk/pollingtrigger';
import { encodeTriggerEvent } from '@superdurable/connectors-sdk/projectconfig';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { InvoiceChangeFetcher, QuickBooksClient, decodeInvoiceChangedEventPayload, type InvoiceChangedEvent } from '../src/index.js';
import { InvoiceWindow, MaximumSeenInvoiceVersions, decodeInvoiceChangeCursor, encodeInvoiceChangeCursor, second } from '../src/invoice_change_cursor.js';
import { QuickBooksInvoiceFake } from './invoice_fake.js';

type Step = Record<string, unknown> & { op: string };
const realmId = '9341453050298464';
const accessToken = 'trigger-access-token';
const connection = { provider: 'quickbooks', name: 'quickbooks-trigger-test' };
const scenarios = (JSON.parse(readFileSync(new URL('./fixtures/go-invoice-change-cases.json', import.meta.url), 'utf8')) as { scenarios: { name: string; steps: Step[] }[] }).scenarios;
const sha256 = (text: string): string => createHash('sha256').update(text).digest('hex');

/** An unsigned ID token naming the company, as the Go recorder's test client uses. */
function idToken(): string {
  const encode = (value: unknown): string => Buffer.from(JSON.stringify(value)).toString('base64url');
  return `${encode({ alg: 'RS256' })}.${encode({ iss: 'https://oauth.platform.intuit.com/op/v1', aud: ['client'], realmid: realmId })}.c2ln`;
}

function newFakeClient(fake: QuickBooksInvoiceFake, requests: string[]): QuickBooksClient {
  const credentials = { clientId: 'client', clientSecret: new SecretString('secret'), accessToken: new SecretString(accessToken), refreshToken: new SecretString(''), idToken: new SecretString(idToken()) };
  const source = { resolve: async () => credentials, resolveWithRefresh: async () => credentials };
  const send = async (request: ProviderRequest) => { const url = new URL(request.url); requests.push(url.pathname + url.search); return fake.send(request); };
  return new QuickBooksClient({}, source, { send, localProviderURL: 'http://127.0.0.1:1' });
}

function applyProviderStep(fake: QuickBooksInvoiceFake, step: Step): boolean {
  const text = (name: string): string => String(step[name]);
  switch (step.op) {
    case 'create': for (let index = 0; index < Number(step.repeat ?? 1); index++) fake.create(text('docNumber'), text('customerId'), text('amount')); return true;
    case 'createBackdated': fake.createBackdated(text('docNumber'), text('customerId'), text('amount'), Number(step.ageSeconds) * 1000); return true;
    case 'pay': fake.pay(text('id')); return true;
    case 'holdClock': fake.holdClock(); return true;
    case 'releaseClock': fake.releaseClock(); return true;
    case 'throttle': fake.throttle(Number(step.count), text('retryAfter')); return true;
    case 'rejectWatermarks': fake.rejectWatermarkQueries(step.rejects === true); return true;
    default: return false;
  }
}

function expectRecordedChanges(step: Step, changes: TriggerEvent<InvoiceChangedEvent>[]): void {
  if ('changeCount' in step) {
    expect(changes).toHaveLength(Number(step.changeCount));
    expect(sha256(changes.map(change => change.id).join('\n'))).toBe(step.changeIdsSha256);
    expect(sha256(changes.map(change => change.payload.changeKind).join('\n'))).toBe(step.changeKindsSha256);
    return;
  }
  const recorded = step.changes as { id: string; occurredAt: string; payload: unknown }[];
  expect(changes.map(change => change.id)).toEqual(recorded.map(change => change.id));
  changes.forEach((change, index) => {
    const stored = JSON.parse(encodeTriggerEvent(change).toString('utf8')) as { occurredAt: string; payload: unknown };
    expect(stored.occurredAt).toBe(recorded[index]!.occurredAt);
    expect(stored.payload).toEqual(recorded[index]!.payload);
    expect(decodeInvoiceChangedEventPayload(JSON.stringify(recorded[index]!.payload))).toEqual(change.payload);
  });
}

function expectRecordedError(step: Step, error: unknown): void {
  if (step.error === 'cursorInvalid') expect(isCursorInvalid(error)).toBe(true);
  else if (step.error === 'rateLimited') { expect(error).toBeInstanceOf(RateLimitedError); expect((error as RateLimitedError).retryAfterMs).toBe(Number(step.retryAfterSeconds) * 1000); }
  else if (step.error === 'failed') expect(error !== undefined && !isCursorInvalid(error) && !(error instanceof RateLimitedError)).toBe(true);
  else expect(error).toBeUndefined();
  if (error) expect(String(error)).not.toContain(accessToken);
}

describe('invoiceChanged fetcher replays the Go recording', () => {
  it.each(scenarios.map(scenario => [scenario.name, scenario] as const))('%s', async (_name, scenario) => {
    const fake = new QuickBooksInvoiceFake(realmId);
    const requests: string[] = [];
    const fetcher = new InvoiceChangeFetcher(newFakeClient(fake, requests), connection);
    const outputs = new Map<number, string>();
    for (const [index, step] of scenario.steps.entries()) {
      if (applyProviderStep(fake, step)) continue;
      let error: unknown;
      let token = '', hasMorePages = false;
      let changes: TriggerEvent<InvoiceChangedEvent>[] = [];
      try {
        if (step.op === 'seed') token = (await fetcher.seedCursor(newTriggerContext())).token;
        else {
          const cursorIn = 'cursorFromStep' in step ? outputs.get(Number(step.cursorFromStep))! : String(step.cursorIn);
          const page = await fetcher.fetchChanges(newTriggerContext(), { token: cursorIn });
          token = page.next.token; changes = page.changes; hasMorePages = page.hasMorePages ?? false;
        }
      } catch (raised) { error = raised; }
      expect(requests, `step ${index}`).toEqual(step.requests);
      requests.length = 0;
      expectRecordedError(step, error);
      if (error !== undefined) continue;
      if ('cursorOut' in step) expect(token, `step ${index}`).toBe(step.cursorOut);
      else { expect(token.length).toBe(step.cursorOutLength); expect(sha256(token)).toBe(step.cursorOutSha256); }
      expect(Buffer.byteLength(token)).toBeLessThan(64 * 1024);
      outputs.set(index, token);
      if (step.op === 'fetch') { expect(hasMorePages).toBe(step.hasMorePages); expectRecordedChanges(step, changes); }
    }
  });
});

describe('invoiceChanged cursor', () => {
  it('moves a single second with more versions than the bound past the window, as Go does', () => {
    const window = new InvoiceWindow(1000n * second, 970n * second);
    const page = Array.from({ length: MaximumSeenInvoiceVersions + 1 }, (_, index) => [`${index}:0`, 1001n * second] as const);
    const following = window.advance(page, false);
    expect([following.since, following.from, following.seen.size]).toEqual([1002n * second, 1002n * second, 0]);
    const token = encodeInvoiceChangeCursor(realmId, 'production', following).token;
    expect(decodeInvoiceChangeCursor(token).window).toEqual(new InvoiceWindow(1002n * second, 1002n * second));
  });

  it("escapes strings as Go's json.Marshal does", () => {
    const window = new InvoiceWindow(10n * second, 0n, new Map([['1:<&>', 10n * second]]));
    const token = encodeInvoiceChangeCursor(realmId, 'production', window).token;
    expect(token).toContain('"seen":["10/1:\\u003c\\u0026\\u003e"]');
    expect(decodeInvoiceChangeCursor(token).window.seen).toEqual(new Map([['1:<&>', 10n * second]]));
  });

  // Recorded from Go's time.Parse(time.RFC3339): its layout fallback takes a one-digit hour, zone offsets of up to 24
  // hours or 60 minutes, and year 0.
  const goWatermarks: Array<[string, string]> = [
    ['2026-01-01T1:00:00Z', '2026-01-01T01:00:00Z'], ['2026-01-01T01:00:00+24:00', '2025-12-31T01:00:00Z'],
    ['2026-01-01T01:00:00+23:60', '2025-12-31T01:00:00Z'], ['2026-01-01T01:00:00-24:00', '2026-01-02T01:00:00Z'],
    ['2026-01-01T01:00:00,5Z', '2026-01-01T01:00:00Z'], ['0000-02-29T12:00:00Z', '0000-02-29T12:00:00Z'],
  ];
  const watermarkToken = (watermark: string): string => JSON.stringify({ realmId, environment: 'production', since: watermark, from: watermark });

  it.each(goWatermarks)('decodes the watermark %s as Go parses it', (watermark, encoded) => {
    const { position, window } = decodeInvoiceChangeCursor(watermarkToken(watermark));
    const reencoded = JSON.parse(encodeInvoiceChangeCursor(position.realmId, position.environment, window).token) as { since: string; from: string };
    expect([reencoded.since, reencoded.from]).toEqual([encoded, encoded]);
  });

  it.each(['2026-01-01T01:00:00z', '2026-01-01T24:00:00Z', '2026-02-29T00:00:00Z', '2026-01-01T01:00:00+25:00'])('rejects the watermark %s as Go does', watermark => {
    expect(() => decodeInvoiceChangeCursor(watermarkToken(watermark))).toThrow(/invalid watermark/);
  });

  it('matches field names, duplicates, and strings as Go does', () => {
    const token = `{"realmId":"${realmId}","environment":"production","since":"1970-01-01T00:00:10Z","SINCE":"x","since":"1970-01-01T00:00:10Z",`
      + '"from":"1970-01-01T00:00:00Z","ſEEN":["10/1:\\ud800","10/1:\\udc00"],"oﬀset":7}';
    const { position, window } = decodeInvoiceChangeCursor(token);
    expect(window.seen).toEqual(new Map([['1:�', 10n * second]]));
    expect([position.since, position.seen, window.offset]).toEqual(['1970-01-01T00:00:10Z', ['10/1:�', '10/1:�'], 0]);
  });
});
