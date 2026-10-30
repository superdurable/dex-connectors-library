// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { AsyncContext } from '@superdurable/dex';
import { describe, expect, it, vi } from 'vitest';
import { createCall } from '../src/credentials.js';
import { CheckpointError, DispatchNotAllowedError, Fence, InsufficientTimeError, type Marker } from '../src/singledispatch/index.js';

function fixture(attempt = 1, last?: Marker, deadline?: Date) {
  const heartbeats: Marker[] = [];
  let shouldRejectHeartbeat = false;
  const context = {
    flowId: 'flow-1', stepExecutionId: 'write:1', attempt, deadline,
    cancellationSignal: new AbortController().signal,
    hasLastHeartbeatValue: () => last !== undefined,
    getLastHeartbeatValue: () => last,
    recordHeartbeat: async (marker: Marker) => {
      if (shouldRejectHeartbeat) throw new Error('heartbeat unavailable');
      heartbeats.push(marker);
    },
  } as AsyncContext;
  const call = createCall(context, { provider: 'fixture', name: 'default' }, { connectorId: 'fixture', operationId: 'writeItem' });
  return { call, heartbeats, rejectHeartbeat: () => { shouldRejectHeartbeat = true; } };
}

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
