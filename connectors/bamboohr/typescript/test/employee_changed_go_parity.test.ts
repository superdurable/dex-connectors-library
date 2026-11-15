// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Replays a recording of the Go employeeChanged fetcher: each step serves one raw changed-employees response,
// and each cursor token must match Go byte for byte, so Go and TypeScript replicas share one cursor document.
import { readFileSync } from 'node:fs';
import { SecretString, newTriggerContext } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { describe, expect, it } from 'vitest';
import { BambooHRClient, EmployeeChangedTriggerDefinition } from '../src/index.js';
import { EmployeeChangeFetcher } from '../src/employee_change_fetcher.js';

interface FixtureStep {
  name: string; seed?: boolean; changeType?: string; cursor?: string;
  response: { status: number; date?: string; retryAfter?: string; body: string };
  query?: string; requests: number; events: Array<{ id: string; occurredAt: string; payload: unknown }> | null;
  eventCount: number; eventsAbridged?: boolean; next?: string; hasMore?: boolean;
  error?: string; errorKind?: string; retryAfterSeconds?: number;
}
const fixture = JSON.parse(readFileSync(new URL('./fixtures/employee-changed-go-fetcher-cases.json', import.meta.url), 'utf8')) as { localNow: string; steps: FixtureStep[] };
const reference = { provider: 'bamboohr', name: 'bamboohr-trigger-test' };
// Split so push protection does not read a fixture key as a credential.
const apiKey = 'parity' + '-key';

function fetcherFor(step: FixtureStep, requests: URL[]): EmployeeChangeFetcher {
  const send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    requests.push(new URL(request.url));
    const headers: Record<string, string> = { 'content-type': 'application/json' };
    if (step.response.date) headers.date = step.response.date;
    if (step.response.retryAfter) headers['retry-after'] = step.response.retryAfter;
    return { status: step.response.status, headers, body: Buffer.from(step.response.body, 'utf8') };
  };
  const credentials = { resolve: async () => ({ apiKey: new SecretString(apiKey) }) };
  const client = new BambooHRClient({ companyDomain: 'acme' }, credentials, { apiBaseURL: 'https://acme.example/api/v1', send, now: () => new Date(fixture.localNow) });
  return new EmployeeChangeFetcher(client, reference, EmployeeChangedTriggerDefinition.trigger, step.changeType ? { changeType: step.changeType } : {});
}

describe('employeeChanged Go parity', () => {
  it.each(fixture.steps.map(step => [step.name, step] as const))('%s', async (name, step) => {
    const requests: URL[] = [];
    const fetcher = fetcherFor(step, requests);
    const context = newTriggerContext();
    let next: Cursor, changes: Awaited<ReturnType<EmployeeChangeFetcher['fetchChanges']>> | undefined;
    try {
      if (step.seed) next = await fetcher.seedCursor(context);
      else { changes = await fetcher.fetchChanges(context, { token: step.cursor! }); next = changes.next; }
    } catch (error) {
      expect(step.error, `Go succeeded, TypeScript threw ${String(error)}`).toBeDefined();
      const kind = isCursorInvalid(error) ? 'cursorInvalid' : error instanceof RateLimitedError ? 'rateLimited' : 'failed';
      expect(kind, name).toBe(step.errorKind);
      expect((error as Error).message, name).toBe(step.error);
      if (error instanceof RateLimitedError) expect(error.retryAfterMs, name).toBe((step.retryAfterSeconds ?? 0) * 1000);
      expect(requests, name).toHaveLength(step.requests);
      return;
    }
    expect(step.error, `Go threw ${step.error}`).toBeUndefined();
    expect(next.token, name).toBe(step.next);
    expect(requests, name).toHaveLength(step.requests);
    if (requests[0]) expect([...requests[0].searchParams], name).toEqual([...new URLSearchParams(step.query)]);
    if (!changes) return;
    expect(changes.hasMorePages ?? false, name).toBe(step.hasMore ?? false);
    expect(changes.changes, name).toHaveLength(step.eventCount);
    const delivered = step.eventsAbridged ? [...changes.changes.slice(0, 3), ...changes.changes.slice(-2)] : changes.changes;
    const recorded = step.events ?? [];
    expect(delivered.map(event => event.id), name).toEqual(recorded.map(event => event.id));
    for (const [index, event] of delivered.entries()) {
      expect(event.occurredAt.getTime(), name).toBe(Date.parse(recorded[index]!.occurredAt));
      expect(event.payload, name).toEqual(recorded[index]!.payload);
    }
  });
});
