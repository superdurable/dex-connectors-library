// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { AsyncContext } from '@superdurable/dex';
import { describe, expect, it, vi } from 'vitest';
import { createCall } from '../src/credentials.js';
import { CheckpointError, DispatchNotAllowedError, Fence, InsufficientTimeError, type Marker } from '../src/singledispatch/index.js';

function fixture(attempt = 1, last?: Marker, deadline?: Date, maxSendsOverride?: number) {
  const heartbeats: Marker[] = [];
  const attemptedHeartbeats: Marker[] = [];
  let shouldRejectHeartbeat = false;
  const context = {
    flowId: 'flow-1', stepExecutionId: 'write:1', attempt, deadline,
    cancellationSignal: new AbortController().signal,
    hasLastHeartbeatValue: () => last !== undefined,
    getLastHeartbeatValue: () => last,
    recordHeartbeat: async (marker: Marker) => {
      attemptedHeartbeats.push(marker);
      if (shouldRejectHeartbeat) throw new Error('heartbeat unavailable');
      heartbeats.push(marker);
    },
  } as AsyncContext;
  const call = createCall(context, { provider: 'fixture', name: 'default' }, { connectorId: 'fixture', operationId: 'writeItem' }, maxSendsOverride === undefined ? {} : { maxSendsOverride });
  return { call, heartbeats, attemptedHeartbeats, rejectHeartbeat: () => { shouldRejectHeartbeat = true; } };
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

  it('fails closed when Dex reports a present but undecodable checkpoint', () => {
    const test = fixture();
    test.call.context.hasLastHeartbeatValue = () => true;
    test.call.context.getLastHeartbeatValue = () => undefined;
    expect(new Fence(test.call, 10_000).decide().kind).toBe('failClosed');
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

  it('writes the earlier marker bytes when bounded resend is disabled', async () => {
    const first = fixture();
    const fence = new Fence(first.call, 10_000);
    expect(fence.decide()).toMatchObject({ kind: 'sendAllowed', priorSendUpperBound: 0 });
    await fence.dispatch(() => undefined);
    await fence.recordProviderId('resource-1');
    expect(json(first.heartbeats)).toEqual(json([
      { callId: callId(first), attempt: 1, state: 'claimed' },
      { callId: callId(first), attempt: 1, state: 'claimed', providerId: 'resource-1' },
    ]));
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

  it('rejects an invalid cap and fails closed on a foreign checkpoint', async () => {
    for (const maxSends of [0, 11, 1.5, Number.NaN]) expect(() => new Fence(fixture().call, 10_000, { maxSends })).toThrow(RangeError);
    expect(() => createCall(fixture().call.context, { provider: 'fixture', name: 'default' }, { connectorId: 'fixture', operationId: 'writeItem' }, { maxSendsOverride: 11 })).toThrow(RangeError);
    const forged = fixture();
    forged.call.maxSendsOverride = 0;
    expect(() => new Fence(forged.call, 10_000, { maxSends: 3 })).toThrow(RangeError);
    const foreign = new Fence(fixture(2, { callId: 'aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa', attempt: 1, state: 'claimed', sendCount: 1 }).call, 10_000, { maxSends: 10 });
    expect(foreign.decide().kind).toBe('failClosed');
    await expect(foreign.dispatch(() => undefined)).rejects.toBeInstanceOf(DispatchNotAllowedError);
  });
});
