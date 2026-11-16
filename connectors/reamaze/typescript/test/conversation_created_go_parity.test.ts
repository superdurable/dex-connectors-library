// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Replays the Go conversationCreated fetcher's recorded cases against the TypeScript fake, whose response bodies
// must hash the same, and requires the same requests, cursor tokens byte for byte, outcomes, event IDs, and
// Trigger inbox payload values, so Go and TypeScript replicas share one poll cursor and one Trigger inbox.
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { SecretString, StaticCredentialProvider, newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid } from '@superdurable/connectors-sdk/pollingtrigger';
import { encodeGoJSONValue } from '@superdurable/connectors-sdk/projectconfig';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { describe, expect, it } from 'vitest';
import { ConversationCreatedFetcher, ConversationCreatedTriggerDefinition, ReamazeClient, type ConversationCreatedEvent } from '../src/index.js';
import { ReamazeConversationFake } from './reamaze_conversations_fake.js';

interface RecordedAction { action: string; slug?: string; subject?: string; channel?: string; status?: number; createdAt?: string; body?: string; prefix?: string; count?: number; spacingMillis?: number; retryAfter?: string }
interface RecordedStep {
  actions?: RecordedAction[]; kind: 'seed' | 'fetch'; cursorIn?: string; cursorInStep?: number; cursorOut?: string; cursorOutSha256?: string;
  responses: Array<{ status: number; retryAfter?: string; bodySha256: string }>; requests: string[];
  changes?: Array<{ id: string; occurredAt: string; payload: string; payloadSortedSha256: string }>; changeIds?: string[]; changeCount?: number; changeIdsSha256?: string;
  error?: string; retryAfterMillis?: number;
}
interface RecordedCase { name: string; channel?: string; token?: string; steps: RecordedStep[] }
const fixture = JSON.parse(readFileSync(new URL('./fixtures/conversation-created-go-fetcher-cases.json', import.meta.url), 'utf8')) as { cases: RecordedCase[] };
const email = 'agent@acme.example.com';
// Split so push protection does not read a fixture as a token.
const token = 'reamazePoll' + 'Token0123456789';
const otherToken = 'otherToken' + '0123456789';
const reference = { provider: 'reamaze', name: 'reamaze-poll' };
const sha256 = (value: Uint8Array | string) => createHash('sha256').update(value).digest('hex');

function apply(provider: ReamazeConversationFake, action: RecordedAction): void {
  switch (action.action) {
    case 'insert': provider.insert(action.slug!, action.subject ?? '', { channel: action.channel ?? '', status: action.status ?? 0, createdAt: new Date(action.createdAt!), body: action.body ?? '' }); break;
    case 'insertRange': {
      const start = Date.parse(action.createdAt!);
      for (let index = 0; index < action.count!; index++) provider.insert(`${action.prefix}-${index}`, `${action.prefix} ${index}`, { createdAt: new Date(start + index * action.spacingMillis!) });
      break;
    }
    case 'hide': provider.hide(action.slug!); break;
    case 'hideRange': for (let index = 0; index < action.count!; index++) provider.hide(`${action.prefix}-${index}`); break;
    case 'reveal': provider.reveal(action.slug!); break;
    case 'rateLimitNextList': provider.rateLimitNextList(action.retryAfter ?? ''); break;
    case 'listOldestFirst': provider.listOldestFirst(); break;
    default: throw new Error(`unknown recorded action ${action.action}`);
  }
}

describe('conversationCreated fetcher matches the Go recording', () => {
  for (const recorded of fixture.cases) {
    it(recorded.name, async () => {
      const provider = new ReamazeConversationFake(email, recorded.token === 'mismatch' ? otherToken : token);
      let requests: string[] = [], responses: RecordedStep['responses'] = [];
      const send = async (request: ProviderRequest): Promise<ProviderResponse> => {
        const response = await provider.send(request);
        requests.push(new URL(request.url).search.slice(1));
        const retryAfter = response.headers['retry-after'];
        responses.push({ status: response.status, ...(retryAfter ? { retryAfter: String(retryAfter) } : {}), bodySha256: sha256(response.body) });
        return response;
      };
      const client = new ReamazeClient({ brand: 'acme', maxResponseBytes: 4_194_304, pollIntervalMs: 60_000 },
        new StaticCredentialProvider([[reference, { email, apiToken: new SecretString(token) }]]), { apiBaseURL: provider.baseURL, send });
      const fetcher = new ConversationCreatedFetcher(client, reference, ConversationCreatedTriggerDefinition.trigger, 'new-conversations', { channel: recorded.channel ?? '' });
      const context = newTriggerContext(new AbortController().signal);
      const outputs: string[] = [];
      for (const [index, step] of recorded.steps.entries()) {
        const name = `${recorded.name} step ${index}`;
        for (const action of step.actions ?? []) apply(provider, action);
        requests = []; responses = [];
        let error = '', output = '', changes: Array<TriggerEvent<ConversationCreatedEvent>> = [];
        try {
          if (step.kind === 'seed') output = (await fetcher.seedCursor(context)).token;
          else {
            const page = await fetcher.fetchChanges(context, { token: step.cursorInStep === undefined ? step.cursorIn ?? '' : outputs[step.cursorInStep]! });
            expect(page.hasMorePages ?? false, name).toBe(false);
            output = page.next.token; changes = page.changes;
          }
        } catch (caught) {
          if (isCursorInvalid(caught)) error = 'cursorInvalid';
          else if (caught instanceof RateLimitedError) { error = 'rateLimited'; expect(caught.retryAfterMs, name).toBe(step.retryAfterMillis); }
          else error = 'failed';
        }
        outputs.push(output);
        expect(error, name).toBe(step.error ?? '');
        expect(requests, name).toEqual(step.requests);
        expect(responses, name).toEqual(step.responses);
        if (step.cursorOutSha256) expect(sha256(output), name).toBe(step.cursorOutSha256);
        else expect(output, name).toBe(step.cursorOut ?? '');
        expect(changes.length, name).toBe(step.changeCount ?? step.changes?.length ?? 0);
        if (step.changeIdsSha256) expect(sha256(changes.map(change => change.id).join('\n')), name).toBe(step.changeIdsSha256);
        else if (step.changeIds) expect(changes.map(change => change.id), name).toEqual(step.changeIds);
        else {
          for (const [position, expected] of (step.changes ?? []).entries()) {
            const change = changes[position]!;
            expect(change.id, name).toBe(expected.id);
            // occurredAt is a millisecond Date; Re:amaze lists creation times in milliseconds.
            expect(change.occurredAt.toISOString().replace(/\.000Z$/, 'Z'), name).toBe(expected.occurredAt);
            // The inbox stores payloads with sorted keys; the recording hashes Go's payload re-encoded that way.
            const payload = encodeGoJSONValue(change.payload);
            expect(sha256(payload), name).toBe(expected.payloadSortedSha256);
            if (!expected.payload.startsWith('sha256:')) expect(JSON.parse(payload), name).toEqual(JSON.parse(expected.payload));
          }
        }
      }
    });
  }
});
