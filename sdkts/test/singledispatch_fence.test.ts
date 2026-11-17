// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFile } from 'node:fs/promises';
import type { AsyncContext } from '@superdurable/dex';
import { describe, expect, it, vi } from 'vitest';
import { createCall } from '../src/credentials.js';
import { CheckpointError, DispatchNotAllowedError, Fence, InsufficientTimeError, type Marker } from '../src/singledispatch/index.js';

type CheckpointFrame = { singleDispatchMarkers: Record<string, Marker> };

/** A Marker `last` is the previous attempt's entry for that marker's call ID; `heartbeats` holds this call's recorded entries. */
function fixture(attempt = 1, last?: Marker, deadline?: Date, maxSendsOverride?: number) {
  const frames: CheckpointFrame[] = [];
  const heartbeats: Marker[] = [];
  const attemptedHeartbeats: Marker[] = [];
  let shouldRejectHeartbeat = false;
  const previous = last === undefined ? undefined : { singleDispatchMarkers: { [last.callId]: last } };
  const context = {
    flowId: 'flow-1', stepExecutionId: 'write:1', attempt, deadline,
    cancellationSignal: new AbortController().signal,
    hasLastHeartbeatValue: () => previous !== undefined,
    getLastHeartbeatValue: () => previous,
    recordHeartbeat: async (frame: CheckpointFrame) => {
      const own = frame.singleDispatchMarkers[call.id]!;
      attemptedHeartbeats.push(own);
      if (shouldRejectHeartbeat) throw new Error('heartbeat unavailable');
      frames.push(frame);
      heartbeats.push(own);
    },
  } as unknown as AsyncContext;
  const call = createCall(context, { provider: 'fixture', name: 'default' }, { connectorId: 'fixture', operationId: 'writeItem' }, maxSendsOverride === undefined ? {} : { maxSendsOverride });
  return { call, frames, heartbeats, attemptedHeartbeats, rejectHeartbeat: () => { shouldRejectHeartbeat = true; } };
}

const json = (markers: readonly Marker[]) => markers.map(marker => JSON.stringify(marker));

describe('single-dispatch live fence', () => {
  it('claims before one send and refuses another dispatch or an unproved retry', async () => {
    const { call, heartbeats } = fixture();
    const fence = new Fence(call, 10_000);
    expect(fence.decide().kind).toBe('sendAllowed');
    let sends = 0;
    await fence.dispatch(() => { sends++; expect(heartbeats[0]?.state).toBe('claimed'); return 'accepted'; });
    expect(sends).toBe(1);
    await expect(fence.dispatch(() => { sends++; })).rejects.toBeInstanceOf(DispatchNotAllowedError);
    expect(new Fence(fixture(2, heartbeats.at(-1)).call, 10_000).decide().kind).toBe('readBackRequired');
    const next = fixture(2);
    expect(new Fence(next.call, 10_000).decide().kind).toBe('readBackRequired');
  });

  it('allows a next-attempt send only after an acknowledged notSent or canResend proof', async () => {
    const first = fixture();
    const beforeSend = new Fence(first.call, 10_000);
    beforeSend.decide();
    await beforeSend.recordNotSent();
    await expect(beforeSend.dispatch(() => undefined)).rejects.toBeInstanceOf(DispatchNotAllowedError);
    const second = fixture(2, first.heartbeats.at(-1));
    const afterProof = new Fence(second.call, 10_000);
    expect(afterProof.decide().kind).toBe('sendAllowed');
    await afterProof.dispatch(() => undefined);
    await afterProof.recordCanResend();
    expect(new Fence(fixture(3, second.heartbeats.at(-1)).call, 10_000).decide().kind).toBe('sendAllowed');
  });

  it('fails closed when a claim heartbeat rejects and never sends', async () => {
    const fixtureValue = fixture();
    const fence = new Fence(fixtureValue.call, 10_000);
    fence.decide();
    fixtureValue.rejectHeartbeat();
    let sends = 0;
    await expect(fence.dispatch(() => { sends++; })).rejects.toBeInstanceOf(CheckpointError);
    expect(sends).toBe(0);
    await expect(fence.dispatch(() => { sends++; })).rejects.toBeInstanceOf(DispatchNotAllowedError);
  });

  it('uses the public Dex attempt deadline and a bounded request signal', async () => {
    const test = fixture(1, undefined, new Date(Date.now() + 50));
    const fence = new Fence(test.call, 10_000);
    fence.decide();
    await new Promise(resolve => setTimeout(resolve, 30));
    await expect(fence.dispatch(() => { throw new Error('must not send'); })).rejects.toBeInstanceOf(InsufficientTimeError);
    expect(test.heartbeats.at(-1)?.state).toBe('notSent');
  });

  it('repeats a claim during a slow request and stops after completion', async () => {
    vi.useFakeTimers();
    try {
      const test = fixture();
      const fence = new Fence(test.call, 20_000);
      fence.decide();
      let finish!: () => void;
      const gate = new Promise<void>(resolve => { finish = resolve; });
      const sent = fence.dispatch(async () => { await gate; });
      await vi.advanceTimersByTimeAsync(5_100);
      expect(test.heartbeats.filter(marker => marker.state === 'claimed')).toHaveLength(2);
      finish();
      await sent;
      await vi.advanceTimersByTimeAsync(5_100);
      expect(test.heartbeats.filter(marker => marker.state === 'claimed')).toHaveLength(2);
    } finally { vi.useRealTimers(); }
  });

  it('records a provider ID for read-back after a response is lost', async () => {
    const test = fixture();
    const fence = new Fence(test.call, 10_000);
    fence.decide();
    await fence.dispatch(() => undefined);
    await fence.recordProviderId('resource-1');
    const second = fixture(2, test.heartbeats.at(-1));
    expect(new Fence(second.call, 10_000).decide()).toMatchObject({ kind: 'readBackRequired', marker: { providerId: 'resource-1' } });
  });

  it('fails closed only when reading the heartbeat throws', () => {
    const empty = fixture();
    empty.call.context.hasLastHeartbeatValue = () => true;
    empty.call.context.getLastHeartbeatValue = () => undefined;
    expect(new Fence(empty.call, 10_000).decide().kind).toBe('sendAllowed');
    const throwing = fixture(2);
    throwing.call.context.hasLastHeartbeatValue = () => true;
    throwing.call.context.getLastHeartbeatValue = () => { throw new Error('heartbeat undecodable'); };
    expect(new Fence(throwing.call, 10_000).decide().kind).toBe('failClosed');
  });

  it('permits a canResend proof after a thrown, provably unprocessed send', async () => {
    const test = fixture();
    const fence = new Fence(test.call, 10_000);
    fence.decide();
    await expect(fence.dispatch(() => { throw new Error('provider returned 503'); })).rejects.toThrow('503');
    await fence.recordCanResend();
    expect(test.heartbeats.at(-1)?.state).toBe('canResend');
  });
});

describe('single-dispatch bounded resend fence', () => {
  const callId = (test: ReturnType<typeof fixture>) => test.call.id;

  it('omits sendCount when bounded resend is disabled', async () => {
    const first = fixture();
    const fence = new Fence(first.call, 10_000);
    expect(fence.decide()).toMatchObject({ kind: 'sendAllowed', priorSendUpperBound: 0 });
    await fence.dispatch(() => undefined);
    await fence.recordProviderId('resource-1');
    expect(json(first.heartbeats)).toEqual(json([
      { callId: callId(first), attempt: 1, state: 'claimed' },
      { callId: callId(first), attempt: 1, state: 'claimed', providerId: 'resource-1' },
    ]));
    expect(JSON.stringify(first.frames.at(-1))).toBe(`{"singleDispatchMarkers":{"${callId(first)}":{"callId":"${callId(first)}","attempt":1,"state":"claimed","providerId":"resource-1"}}}`);
    const afterLegacyProof = fixture(3, { callId: callId(first), attempt: 2, state: 'canResend', sendCount: 1 });
    const legacyFence = new Fence(afterLegacyProof.call, 10_000);
    expect(legacyFence.decide()).toMatchObject({ kind: 'sendAllowed', priorSendUpperBound: 1 });
    await legacyFence.dispatch(() => { throw new Error('provider returned 503'); }).catch(() => undefined);
    await legacyFence.recordCanResend();
    expect(json(afterLegacyProof.heartbeats)).toEqual(json([
      { callId: callId(first), attempt: 3, state: 'claimed' },
      { callId: callId(first), attempt: 3, state: 'canResend' },
    ]));
    const unproved = new Fence(fixture(2, first.heartbeats.at(-1)).call, 10_000);
    expect(unproved.decide().kind).toBe('readBackRequired');
    await expect(unproved.dispatch(() => undefined)).rejects.toBeInstanceOf(DispatchNotAllowedError);

    const notSent = fixture();
    const beforeSend = new Fence(notSent.call, 10_000);
    beforeSend.decide();
    await beforeSend.recordNotSent();
    expect(json(notSent.heartbeats)).toEqual(json([{ callId: callId(notSent), attempt: 1, state: 'notSent' }]));
  });

  it('resends after an unconfirmed claim below the cap and counts every marker', async () => {
    const first = fixture();
    const firstFence = new Fence(first.call, 10_000, { maxSends: 3 });
    firstFence.decide();
    await firstFence.dispatch(() => undefined);
    const second = fixture(2, first.heartbeats.at(-1));
    const secondFence = new Fence(second.call, 10_000, { maxSends: 3 });
    expect(secondFence.decide()).toMatchObject({ kind: 'readBackThenResendAllowed', priorSendUpperBound: 1 });
    await secondFence.dispatch(() => { expect(second.heartbeats[0]).toEqual({ callId: callId(second), attempt: 2, state: 'claimed', sendCount: 2 }); });
    await secondFence.recordProviderId('object-2');
    expect(json(second.heartbeats)).toEqual(json([
      { callId: callId(second), attempt: 2, state: 'claimed', sendCount: 2 },
      { callId: callId(second), attempt: 2, state: 'claimed', providerId: 'object-2', sendCount: 2 },
    ]));
    const third = new Fence(fixture(3, second.heartbeats.at(-1)).call, 10_000, { maxSends: 3 });
    expect(third.decide()).toMatchObject({ kind: 'readBackThenResendAllowed', priorSendUpperBound: 2, marker: { providerId: 'object-2' } });
  });

  it('refuses a resend once the cap is reached', async () => {
    const atCap = fixture(3, { callId: fixture().call.id, attempt: 2, state: 'claimed', sendCount: 2 });
    const fence = new Fence(atCap.call, 10_000, { maxSends: 2 });
    expect(fence.decide()).toMatchObject({ kind: 'readBackRequired', priorSendUpperBound: 2 });
    await expect(fence.dispatch(() => { throw new Error('must not send'); })).rejects.toBeInstanceOf(DispatchNotAllowedError);
    await expect(fence.recordNotSent()).rejects.toBeInstanceOf(DispatchNotAllowedError);
    expect(atCap.heartbeats).toEqual([]);
    const noCheckpoint = new Fence(fixture(3).call, 10_000, { maxSends: 2 });
    expect(noCheckpoint.decide()).toMatchObject({ kind: 'readBackRequired', priorSendUpperBound: 2 });
  });

  it('carries the prior count on notSent and canResend so the next attempt may send', async () => {
    const id = fixture().call.id;
    const resend = fixture(3, { callId: id, attempt: 2, state: 'claimed', sendCount: 2 });
    const fence = new Fence(resend.call, 10_000, { maxSends: 3 });
    fence.decide();
    await fence.dispatch(() => { throw new Error('provider returned 503'); }).catch(() => undefined);
    await fence.recordCanResend();
    expect(json(resend.heartbeats)).toEqual(json([
      { callId: id, attempt: 3, state: 'claimed', sendCount: 3 },
      { callId: id, attempt: 3, state: 'canResend', sendCount: 2 },
    ]));
    const next = fixture(4, resend.heartbeats.at(-1));
    const nextFence = new Fence(next.call, 10_000, { maxSends: 3 });
    expect(nextFence.decide()).toMatchObject({ kind: 'sendAllowed', priorSendUpperBound: 2 });
    await nextFence.recordNotSent();
    expect(json(next.heartbeats)).toEqual(json([{ callId: id, attempt: 4, state: 'notSent', sendCount: 2 }]));
  });

  it('never records notSent while an earlier send is unresolved', async () => {
    const id = fixture().call.id;
    const shortWindow = fixture(2, { callId: id, attempt: 1, state: 'claimed', sendCount: 1 }, new Date(Date.now() + 50));
    const fence = new Fence(shortWindow.call, 10_000, { maxSends: 2 });
    expect(fence.decide().kind).toBe('readBackThenResendAllowed');
    await new Promise(resolve => setTimeout(resolve, 30));
    await expect(fence.dispatch(() => { throw new Error('must not send'); })).rejects.toBeInstanceOf(InsufficientTimeError);
    await expect(fence.dispatch(() => { throw new Error('must not send'); })).rejects.toBeInstanceOf(DispatchNotAllowedError);
    expect(shortWindow.heartbeats).toEqual([]);

    const preSend = fixture(2, { callId: id, attempt: 1, state: 'claimed', sendCount: 1 });
    const preSendFence = new Fence(preSend.call, 10_000, { maxSends: 2 });
    preSendFence.decide();
    await expect(preSendFence.recordNotSent()).rejects.toBeInstanceOf(DispatchNotAllowedError);
    expect(preSend.heartbeats).toEqual([]);

    const failedClaim = fixture(2, { callId: id, attempt: 1, state: 'claimed', sendCount: 1 });
    const failedClaimFence = new Fence(failedClaim.call, 10_000, { maxSends: 2 });
    failedClaimFence.decide();
    failedClaim.rejectHeartbeat();
    await expect(failedClaimFence.dispatch(() => { throw new Error('must not send'); })).rejects.toBeInstanceOf(CheckpointError);
    expect(failedClaim.attemptedHeartbeats).toEqual([{ callId: id, attempt: 2, state: 'claimed', sendCount: 2 }]);
  });

  it('checkpoints notSent with the prior count when a proven retry has too short a window', async () => {
    const id = fixture().call.id;
    const test = fixture(3, { callId: id, attempt: 2, state: 'canResend', sendCount: 1 }, new Date(Date.now() + 50));
    const fence = new Fence(test.call, 10_000, { maxSends: 2 });
    expect(fence.decide()).toMatchObject({ kind: 'sendAllowed', priorSendUpperBound: 1 });
    await new Promise(resolve => setTimeout(resolve, 30));
    await expect(fence.dispatch(() => { throw new Error('must not send'); })).rejects.toBeInstanceOf(InsufficientTimeError);
    expect(test.heartbeats).toEqual([{ callId: id, attempt: 3, state: 'notSent', sendCount: 1 }]);
  });

  it('repeats the counted claim during a slow resend', async () => {
    vi.useFakeTimers();
    try {
      const id = fixture().call.id;
      const test = fixture(2, { callId: id, attempt: 1, state: 'claimed', sendCount: 1 });
      const fence = new Fence(test.call, 20_000, { maxSends: 2 });
      fence.decide();
      let finish!: () => void;
      const gate = new Promise<void>(resolve => { finish = resolve; });
      const sent = fence.dispatch(async () => { await gate; });
      await vi.advanceTimersByTimeAsync(5_100);
      finish();
      await sent;
      expect(test.heartbeats).toEqual([{ callId: id, attempt: 2, state: 'claimed', sendCount: 2 }, { callId: id, attempt: 2, state: 'claimed', sendCount: 2 }]);
    } finally { vi.useRealTimers(); }
  });

  it('applies the application cap only to an operation that resends', () => {
    const id = fixture().call.id;
    const last: Marker = { callId: id, attempt: 3, state: 'claimed', sendCount: 3 };
    expect(new Fence(fixture(4, last, undefined, 5).call, 10_000, { maxSends: 3 }).decide().kind).toBe('readBackThenResendAllowed');
    expect(new Fence(fixture(4, last).call, 10_000, { maxSends: 3 }).decide().kind).toBe('readBackRequired');
    expect(new Fence(fixture(2, { ...last, attempt: 1, sendCount: 1 }, undefined, 1).call, 10_000, { maxSends: 3 }).decide().kind).toBe('readBackRequired');
    expect(new Fence(fixture(2, { ...last, attempt: 1, sendCount: 1 }, undefined, 5).call, 10_000).decide().kind).toBe('readBackRequired');
    expect(new Fence(fixture(2, { ...last, attempt: 1, sendCount: 1 }, undefined, 5).call, 10_000, { maxSends: 1 }).decide().kind).toBe('readBackRequired');
  });

  it('rejects an invalid cap and treats a foreign entry as no marker', async () => {
    for (const maxSends of [0, 11, 1.5, Number.NaN]) expect(() => new Fence(fixture().call, 10_000, { maxSends })).toThrow(RangeError);
    expect(() => createCall(fixture().call.context, { provider: 'fixture', name: 'default' }, { connectorId: 'fixture', operationId: 'writeItem' }, { maxSendsOverride: 11 })).toThrow(RangeError);
    const forged = fixture();
    forged.call.maxSendsOverride = 0;
    expect(() => new Fence(forged.call, 10_000, { maxSends: 3 })).toThrow(RangeError);
    const foreign = new Fence(fixture(2, { callId: 'aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa', attempt: 1, state: 'claimed', sendCount: 1 }).call, 10_000, { maxSends: 10 });
    expect(foreign.decide()).toEqual({ kind: 'readBackThenResendAllowed', priorSendUpperBound: 1 });
    const foreignAtCap = new Fence(fixture(2, { callId: 'aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa', attempt: 1, state: 'notSent' }).call, 10_000);
    expect(foreignAtCap.decide()).toEqual({ kind: 'readBackRequired', priorSendUpperBound: 1 });
    await expect(foreignAtCap.dispatch(() => undefined)).rejects.toBeInstanceOf(DispatchNotAllowedError);
  });
});

/** A Dex context fake that keeps the raw heartbeat frames of one Step attempt. */
function stepContext(attempt = 1, previous?: unknown) {
  const frames: CheckpointFrame[] = [];
  const context = {
    flowId: 'flow-1', stepExecutionId: 'agent:1', attempt,
    cancellationSignal: new AbortController().signal,
    hasLastHeartbeatValue: () => previous !== undefined,
    getLastHeartbeatValue: () => previous,
    recordHeartbeat: async (frame: CheckpointFrame) => { await Promise.resolve(); frames.push(frame); },
  } as unknown as AsyncContext;
  const callFor = (operationId: string) => createCall(context, { provider: 'fixture', name: 'default' }, { connectorId: 'fixture', operationId });
  return { context, frames, callFor };
}

interface CheckpointScenario {
  name: string; attempt: number; previousHeartbeat?: unknown; heartbeats: unknown[];
  calls: { callId: string; maxSends: number; dispatch: boolean; providerId?: string; decision: string; maximumPriorSends: number }[];
}

describe('single-dispatch Go checkpoint scenarios', () => {
  it('replays every scenario with identical decisions and heartbeat bytes', async () => {
    const committed = await readFile(new URL('../../conformance/single-dispatch-markers.json', import.meta.url), 'utf8');
    const scenarios: CheckpointScenario[] = JSON.parse(committed).checkpointScenarios;
    expect(scenarios.length).toBeGreaterThanOrEqual(10);
    for (const scenario of scenarios) {
      const step = stepContext(scenario.attempt, scenario.previousHeartbeat);
      for (const scenarioCall of scenario.calls) {
        const call = Object.assign(step.callFor('writeItem'), { id: scenarioCall.callId });
        const fence = new Fence(call, 60_000, { maxSends: scenarioCall.maxSends });
        const decision = fence.decide();
        expect(decision.kind, scenario.name).toBe(scenarioCall.decision);
        expect(decision.priorSendUpperBound ?? 0, scenario.name).toBe(scenarioCall.maximumPriorSends);
        if (scenarioCall.dispatch && (decision.kind === 'sendAllowed' || decision.kind === 'readBackThenResendAllowed')) {
          await fence.dispatch(() => undefined);
          if (scenarioCall.providerId) await fence.recordProviderId(scenarioCall.providerId);
        }
      }
      expect(step.frames.map(frame => JSON.stringify(frame)), scenario.name).toEqual(scenario.heartbeats.map(frame => JSON.stringify(frame)));
    }
  });
});

describe('single-dispatch fences sharing one Step', () => {
  it('lets a Gmail send and a Slack reply both send, and keeps both entries', async () => {
    const step = stepContext();
    const gmail = step.callFor('sendMessage');
    const slack = step.callFor('postThreadReply');
    for (const call of [gmail, slack]) {
      const fence = new Fence(call, 10_000);
      expect(fence.decide().kind).toBe('sendAllowed');
      await fence.dispatch(() => undefined);
    }
    expect(Object.keys(step.frames.at(-1)!.singleDispatchMarkers).sort()).toEqual([gmail.id, slack.id].sort());

    const retry = stepContext(2, step.frames.at(-1));
    const slackRetry = new Fence(retry.callFor('postThreadReply'), 10_000, { maxSends: 3 });
    expect(slackRetry.decide()).toMatchObject({ kind: 'readBackThenResendAllowed', priorSendUpperBound: 1 });
    await slackRetry.dispatch(() => undefined);
    expect(retry.frames.at(-1)!.singleDispatchMarkers[gmail.id]).toEqual({ callId: gmail.id, attempt: 1, state: 'claimed' });
  });

  it('keeps every entry when fences in one Step run concurrently', async () => {
    const step = stepContext();
    const calls = Array.from({ length: 16 }, (_, index) => step.callFor(`write${index}`));
    await Promise.all(calls.map(async (call, index) => {
      const fence = new Fence(call, 10_000, { maxSends: 3 });
      expect(fence.decide().kind).toBe('sendAllowed');
      await fence.dispatch(() => undefined);
      await fence.recordProviderId(`object-${index}`);
    }));
    const markers = step.frames.at(-1)!.singleDispatchMarkers;
    expect(Object.keys(markers)).toEqual(calls.map(call => call.id).sort());
    calls.forEach((call, index) => expect(markers[call.id]).toEqual({ callId: call.id, attempt: 1, state: 'claimed', providerId: `object-${index}`, sendCount: 1 }));
  });

  it('keeps the shared registry on a global symbol so every SDK copy finds it', async () => {
    const step = stepContext();
    const first = new Fence(step.callFor('sendMessage'), 10_000);
    const holder = globalThis as unknown as Record<symbol, WeakMap<object, { attempt: number; markers: Map<string, unknown> }>>;
    const registry = holder[Symbol.for('@superdurable/connectors-sdk/singleDispatchCheckpoints/v1')];
    expect(registry).toBeInstanceOf(WeakMap);
    first.decide();
    await first.dispatch(() => undefined);
    // Another SDK copy would read and extend the same plain-data entry.
    expect([...registry!.get(step.context)!.markers.keys()]).toEqual([step.callFor('sendMessage').id]);
    expect(registry!.get(step.context)!.attempt).toBe(1);
  });
});
