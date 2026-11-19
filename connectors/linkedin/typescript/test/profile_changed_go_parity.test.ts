// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Replays a recording of the Go profileChanged fetcher: cursor tokens, profile hashes, claims, and event IDs
// must match Go byte for byte, so Go and TypeScript replicas share one poll cursor document.
import { readFileSync } from 'node:fs';
import { newTriggerContext } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { describe, expect, it } from 'vitest';
import { ProfileChangedTriggerDefinition } from '../src/index.js';
import { encodeProfileClaimsAsGoJSON, profileClaimsFromUserInfoBody, profileHash } from '../src/profile_changed.js';
import { ProfileFetcher } from '../src/profile_fetcher.js';
import { newFakeClient } from './linkedin_profile_fake.js';

interface FixtureStep {
  name: string; body: string; status?: number; retryAfter?: string; seed?: boolean; cursor?: string; error?: string; next?: string;
  changes?: Array<{ id: string; occurredAt: string; payload: string }>;
}
interface FixtureClaims { body: string; outcome: 'profile' | 'noSubject' | 'invalid'; profile?: string; hash?: string }
const fixture = JSON.parse(readFileSync(new URL('./fixtures/profile-changed-go-fetcher-cases.json', import.meta.url), 'utf8')) as { observedAt: string; steps: FixtureStep[]; claims: FixtureClaims[] };
const reference = { provider: 'linkedin', name: 'linkedin-watcher' };

/** Go's error outcome name for a failed fetch, as the recorder writes it. */
function goOutcome(error: unknown): string {
  if (isCursorInvalid(error)) return 'cursorInvalid';
  if (error instanceof RateLimitedError) return `rateLimited:${goDuration(error.retryAfterMs)}`;
  return error instanceof Error ? error.message : String(error);
}

function goDuration(milliseconds: number): string {
  if (milliseconds === 0) return '0s';
  const minutes = Math.floor(milliseconds / 60_000), seconds = (milliseconds % 60_000) / 1_000;
  return `${minutes ? `${minutes}m` : ''}${seconds}s`;
}

describe('profileChanged Go parity', () => {
  it('matches the Go fetcher recording byte for byte', async () => {
    let current: FixtureStep = fixture.steps[0]!;
    const send = async (request: ProviderRequest): Promise<ProviderResponse> => {
      expect(request.method).toBe('GET');
      return { status: current.status ?? 200, headers: current.retryAfter ? { 'retry-after': current.retryAfter } : {}, body: current.status ? new Uint8Array() : Buffer.from(current.body, 'base64') };
    };
    const fetcher = new ProfileFetcher(newFakeClient({ send }, reference.name, () => new Date(fixture.observedAt), 'token'), reference, ProfileChangedTriggerDefinition.trigger);
    const context = newTriggerContext();
    let cursor = '';
    for (const step of fixture.steps) {
      current = step;
      let next: Cursor | undefined, outcome: string | undefined;
      let changes: Awaited<ReturnType<ProfileFetcher['fetchChanges']>>['changes'] = [];
      try {
        if (step.seed) next = await fetcher.seedCursor(context);
        else ({ next, changes } = await fetcher.fetchChanges(context, { token: step.cursor || cursor }));
      } catch (error) { outcome = goOutcome(error); }
      expect(changes.map(change => change.id), step.name).toEqual((step.changes ?? []).map(change => change.id));
      for (const [index, change] of changes.entries()) {
        const recorded = step.changes![index]!;
        expect(change.occurredAt.toISOString().replace('.000Z', 'Z'), step.name).toBe(recorded.occurredAt);
        expect(change.payload, step.name).toEqual(JSON.parse(recorded.payload));
      }
      expect(outcome, step.name).toBe(step.error);
      if (next) { expect(next.token, step.name).toBe(step.next); cursor = next.token; }
    }
  });

  it('decodes, bounds, encodes, and hashes UserInfo claims as Go does', () => {
    for (const recorded of fixture.claims) {
      const body = Buffer.from(recorded.body, 'base64'), label = body.toString('utf8').slice(0, 120);
      let outcome: string, profile: string | undefined, hash: string | undefined;
      try {
        const claims = profileClaimsFromUserInfoBody(body);
        outcome = claims ? 'profile' : 'noSubject';
        if (claims) { profile = encodeProfileClaimsAsGoJSON(claims); hash = profileHash(claims); }
      } catch { outcome = 'invalid'; }
      expect(outcome, label).toBe(recorded.outcome);
      expect(profile, label).toBe(recorded.profile);
      expect(hash, label).toBe(recorded.hash);
    }
  });
});
