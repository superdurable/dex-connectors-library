// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { newTriggerContext, SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { isCursorInvalid, RateLimitedError } from '@superdurable/connectors-sdk/pollingtrigger';
import { expect, it } from 'vitest';
import { LinkedInClient, ProfileChangedTriggerDefinition, ProfileFetcher } from '../src/index.js';

interface RecordedStep { step: { name: string; operation: string; status: number; body?: string; retryAfter?: string }; result: { input?: string; next?: string; events?: Array<{ id: string; occurredAt: string; payload: string }>; error?: { kind: string; message?: string; retryAfterSeconds?: number }; requests?: Array<unknown> } }
const fixture = JSON.parse(readFileSync(new URL('../../python/tests/fixtures/profile-changed-go-fetcher-cases.json', import.meta.url), 'utf8')) as { observedAt: string; steps: RecordedStep[] };
const connection = { provider: 'linkedin', name: 'watcher' };
const credentials = new StaticCredentialProvider([[connection, { accessToken: new SecretString('one-use-token') }]]);
const context = newTriggerContext();

it.each(fixture.steps)('matches Go profile fetcher: $step.name', async ({ step, result }) => {
  const requests: string[] = [];
  const client = new LinkedInClient({ userInfoUrl: 'http://127.0.0.1/v2/userinfo' }, credentials, {
    now: () => new Date(fixture.observedAt),
    send: async request => {
      requests.push(request.method);
      expect(new URL(request.url).pathname).toBe('/v2/userinfo');
      expect(request.headers?.Authorization).toBe('Bearer one-use-token');
      return { status: step.status, headers: step.retryAfter ? { 'retry-after': step.retryAfter } : {}, body: new TextEncoder().encode(step.body ?? '') };
    },
  });
  const fetcher = new ProfileFetcher(client, connection, ProfileChangedTriggerDefinition.trigger);
  let actual: Awaited<ReturnType<typeof fetcher.fetchChanges>> | Awaited<ReturnType<typeof fetcher.seedCursor>> | undefined;
  let error: unknown;
  try { actual = step.operation === 'seed' ? await fetcher.seedCursor(context) : await fetcher.fetchChanges(context, { token: result.input ?? '' }); }
  catch (caught) { error = caught; }
  if (result.error) {
    expect(error).toBeInstanceOf(Error);
    if (result.error.kind === 'cursorInvalid') expect(isCursorInvalid(error)).toBe(true);
    else if (result.error.kind === 'rateLimited') expect((error as RateLimitedError).retryAfterMs).toBe((result.error.retryAfterSeconds ?? 0) * 1000);
    else expect((error as Error).message).toBe(result.error.message);
  } else {
    expect(error).toBeUndefined();
    if (!actual) throw new Error('expected a cursor');
    expect('next' in actual ? actual.next.token : actual.token).toBe(result.next);
    if ('changes' in actual) expect(actual.changes.map(event => ({ id: event.id, occurredAt: event.occurredAt.toISOString().replace('.000Z', 'Z'), payload: event.payload }))).toEqual((result.events ?? []).map(event => ({ id: event.id, occurredAt: event.occurredAt, payload: JSON.parse(event.payload) })));
  }
  expect(requests.length).toBe(result.requests?.length ?? 0);
});
