// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { SecretString, newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid } from '@superdurable/connectors-sdk/pollingtrigger';
import { encodeTriggerEvent } from '@superdurable/connectors-sdk/projectconfig';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { InvoiceChangeFetcher, MaximumSeenInvoiceChanges, XeroClient, decodeInvoiceChangedEventPayload, parseInvoiceChangedTriggerConfiguration, seenInvoiceChangeKey, type InvoiceChangedEvent } from '../src/index.js';
import { decodeInvoiceChangeCursor, encodeInvoiceChangeCursor, pruneSeenInvoiceChanges } from '../src/invoice_change_cursor.js';
import { BaseTimeMs, XeroInvoiceFake } from './invoice_fake.js';

type Step = Record<string, unknown> & { op: string };
// Inert test credentials, matching the Go recorder's test client.
const accessToken = 'xero-trigger-' + 'token';
const oauthAccessToken = 'xero-oauth-' + 'token';
const tenantId = '70784a63-d24b-46a9-a4db-0e70a274b056';
const connection = { provider: 'xero', name: 'xero-invoice-changes' };
const scenarios = (JSON.parse(readFileSync(new URL('./fixtures/go-invoice-change-cases.json', import.meta.url), 'utf8')) as { scenarios: { name: string; steps: Step[] }[] }).scenarios;
const sha256 = (text: string): string => createHash('sha256').update(text).digest('hex');

function newFakeClient(fake: XeroInvoiceFake, requests: string[], oauth: boolean): XeroClient {
  const credentials = oauth
    ? { authMethodId: 'xero-oauth', clientId: '0F1E2D3C4B5A69788796A5B4C3D2E1F0', clientSecret: new SecretString('client-' + 'secret'), accessToken: new SecretString(oauthAccessToken) }
    : { authMethodId: 'custom-connection', clientId: '0F1E2D3C4B5A69788796A5B4C3D2E1F0', clientSecret: new SecretString('client-' + 'secret'), accessToken: new SecretString(accessToken) };
  const source = { resolve: async () => credentials, resolveWithRefresh: async () => credentials };
  const send = async (request: ProviderRequest) => {
    const url = new URL(request.url);
    requests.push(`${request.method} ${url.pathname}${url.search} ims=${request.headers?.['If-Modified-Since'] ?? ''} tenant=${request.headers?.['Xero-Tenant-Id'] ?? ''}`);
    return fake.send(request);
  };
  return new XeroClient({ pollIntervalMs: 30_000, ...(oauth ? { organisation: tenantId } : {}) }, source, { send, localProviderURL: 'http://127.0.0.1:1' });
}

function applyProviderStep(fake: XeroInvoiceFake, step: Step): boolean {
  const text = (name: string): string => String(step[name] ?? '');
  const number = (name: string): number => Number(step[name]);
  switch (step.op) {
    case 'upsert': fake.upsert(text('id'), text('number'), text('type'), text('status'), text('total'), number('stepMs')); return true;
    case 'upsertMany':
      for (let offset = 0; offset < number('count'); offset++) fake.upsert(text('idPrefix') + String(offset).padStart(4, '0'), '', 'ACCREC', 'DRAFT', '1.00', number('stepMs'));
      return true;
    case 'setStatus': fake.setStatus(text('id'), text('status')); return true;
    case 'advance': fake.advanceClock(number('ms')); return true;
    case 'rateLimit': fake.rateLimitNext(number('count'), text('problem'), text('retryAfter')); return true;
    case 'fail': fake.failNext(number('count'), number('status')); return true;
    case 'limitPageSize': fake.limitPageSize(number('size')); return true;
    default: return false;
  }
}

function expectRecordedChanges(step: Step, changes: TriggerEvent<InvoiceChangedEvent>[]): void {
  if ('changeCount' in step) {
    expect(changes).toHaveLength(Number(step.changeCount));
    expect(sha256(changes.map(change => change.id).join('\n'))).toBe(step.changeIdsSha256);
    return;
  }
  const recorded = step.changes as { eventId: string; occurredAt: string; payload: unknown }[];
  expect(changes.map(change => change.id)).toEqual(recorded.map(change => change.eventId));
  changes.forEach((change, index) => {
    const stored = JSON.parse(encodeTriggerEvent(change).toString('utf8')) as { occurredAt: string; payload: unknown };
    expect(stored.occurredAt).toBe(recorded[index]!.occurredAt);
    expect(stored.payload).toEqual(recorded[index]!.payload);
    expect(decodeInvoiceChangedEventPayload(JSON.stringify(recorded[index]!.payload))).toEqual(change.payload);
  });
}

function expectRecordedError(step: Step, error: unknown): void {
  if (step.error === 'cursorInvalid') expect(isCursorInvalid(error), String(error)).toBe(true);
  else if (step.error === 'rateLimited') { expect(error).toBeInstanceOf(RateLimitedError); expect((error as RateLimitedError).retryAfterMs).toBe(Number(step.retryAfterSeconds) * 1000); }
  else if (step.error === 'failed') expect(error !== undefined && !isCursorInvalid(error) && !(error instanceof RateLimitedError)).toBe(true);
  else expect(error).toBeUndefined();
  if (error) { expect(String(error)).not.toContain(accessToken); expect(String(error)).not.toContain(oauthAccessToken); }
}

describe('invoiceChanged fetcher replays the Go recording', () => {
  it.each(scenarios.map(scenario => [scenario.name, scenario] as const))('%s', async (_name, scenario) => {
    const fake = new XeroInvoiceFake();
    const requests: string[] = [];
    const clients = new Map([false, true].map(oauth => [oauth, newFakeClient(fake, requests, oauth)]));
    const outputs = new Map<number, string>();
    for (const [index, step] of scenario.steps.entries()) {
      if (applyProviderStep(fake, step)) continue;
      const fetcher = new InvoiceChangeFetcher(clients.get(step.oauth === true)!, connection, parseInvoiceChangedTriggerConfiguration(step.configuration ?? {}));
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
      if (step.op === 'fetch') { expect(hasMorePages, `step ${index}`).toBe(step.hasMorePages); expectRecordedChanges(step, changes); }
    }
  });
});

describe('invoiceChanged cursor', () => {
  it('prunes delivered changes to the window and keeps the newest bounded set', () => {
    const since = BaseTimeMs + 3_600_000;
    const old = seenInvoiceChangeKey('a', since - 60_000), recent = seenInvoiceChangeKey('b', since - 1_000);
    expect(pruneSeenInvoiceChanges([recent, old, recent], since)).toEqual([recent]);
    const many = Array.from({ length: MaximumSeenInvoiceChanges + 5 }, (_, index) => seenInvoiceChangeKey('c', since + index));
    const pruned = pruneSeenInvoiceChanges(many, since);
    expect(pruned).toHaveLength(MaximumSeenInvoiceChanges);
    expect(pruned.at(-1)).toBe(many.at(-1));
    const token = encodeInvoiceChangeCursor({ filter: 'type=;statuses=', tenantId: '', since, page: 0, seen: pruned }).token;
    expect(token.length).toBeLessThan(64 * 1024);
    expect(decodeInvoiceChangeCursor(token).seen).toEqual(pruned);
  });

  it('decodes an escaped lone surrogate as U+FFFD, as Go does', () => {
    const position = decodeInvoiceChangeCursor('{"filter":"type=;statuses=\\ud800","since":10,"seen":["\\udc00"]}');
    expect([position.filter, position.seen]).toEqual(['type=;statuses=�', ['�']]);
  });

  it('folds keys only as Go does: the long s and Kelvin sign, never the dotted or dotless i', () => {
    const position = decodeInvoiceChangeCursor('{"f\\u0131lter":"a","tenant\\u0130d":"t","\\u017fince":5}');
    expect([position.filter, position.tenantId, position.since]).toEqual(['', '', 5]);
  });
});
