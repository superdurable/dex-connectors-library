// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFile } from 'node:fs/promises';
import { describe, expect, it } from 'vitest';
import { MarkerSchema, decideDispatch } from '../src/singledispatch/index.js';

const callId = '5af8fafa-06ff-5b2d-bb13-04693809fabf';
const foreignCallId = 'aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa';

describe('single-dispatch marker decisions', () => {
  it('round-trips the Go heartbeat marker fixture byte for byte', async () => {
    const committed = await readFile(new URL('../../conformance/single-dispatch-markers.json', import.meta.url), 'utf8');
    const markers: unknown[] = JSON.parse(committed);
    const parsed = MarkerSchema.array().parse(markers);
    expect(parsed).toHaveLength(8);
    expect(`${JSON.stringify(parsed, null, 2)}\n`).toBe(committed);
    parsed.forEach((marker, index) => expect(JSON.stringify(marker)).toBe(JSON.stringify(markers[index])));
    expect(parsed.slice(4)).toEqual([
      { callId, attempt: 5, state: 'claimed', sendCount: 2 },
      { callId, attempt: 6, state: 'notSent', sendCount: 2 },
      { callId, attempt: 7, state: 'canResend', sendCount: 2 },
      { callId, attempt: 8, state: 'claimed', providerId: 'object-43', sendCount: 3 },
    ]);
  });

  it('allows a first send and a proven not-sent or unprocessed retry', () => {
    expect(decideDispatch(callId, 1, undefined).kind).toBe('sendAllowed');
    expect(decideDispatch(callId, 2, { callId, attempt: 1, state: 'notSent' }).kind).toBe('sendAllowed');
    expect(decideDispatch(callId, 3, { callId, attempt: 2, state: 'canResend' }).kind).toBe('sendAllowed');
  });

  it('requires read-back after a claim or missing later checkpoint', () => {
    expect(decideDispatch(callId, 2, { callId, attempt: 1, state: 'claimed' }).kind).toBe('readBackRequired');
    expect(decideDispatch(callId, 2, undefined).kind).toBe('readBackRequired');
    expect(decideDispatch(callId, 3, { callId, attempt: 1, state: 'notSent' }).kind).toBe('readBackRequired');
    expect(decideDispatch(callId, 2, { callId, attempt: 1, state: 'canResend', providerId: 'item-1' }).kind).toBe('readBackRequired');
  });

  it('fails closed for unreadable, invalid, or foreign checkpoints', () => {
    expect(decideDispatch(callId, 1, undefined, new Error('heartbeat failed')).kind).toBe('failClosed');
    expect(decideDispatch(callId, 2, { callId: foreignCallId, attempt: 1, state: 'notSent' }).kind).toBe('failClosed');
    expect(decideDispatch(callId, 2, { callId, attempt: 1, state: 'unknown' }).kind).toBe('failClosed');
  });
});

describe('single-dispatch bounded resend', () => {
  const vectors: readonly [attempt: number, marker: unknown][] = [
    [1, undefined], [2, undefined], [5, undefined],
    [2, { callId, attempt: 1, state: 'notSent' }], [3, { callId, attempt: 2, state: 'canResend' }],
    [2, { callId, attempt: 1, state: 'claimed' }], [3, { callId, attempt: 1, state: 'notSent' }],
    [2, { callId, attempt: 1, state: 'canResend', providerId: 'item-1' }], [2, { callId, attempt: 1, state: 'claimed', providerId: 'item-1' }],
    [3, { callId, attempt: 2, state: 'claimed', sendCount: 2 }], [9, { callId, attempt: 8, state: 'claimed', providerId: 'object-43', sendCount: 3 }],
    [2, { callId, attempt: 2, state: 'claimed', sendCount: 1 }], [2, { callId, attempt: 3, state: 'notSent' }],
  ];

  it('keeps the earlier decisions when maxSends is 1', () => {
    const expectedKinds = ['sendAllowed', 'readBackRequired', 'readBackRequired', 'sendAllowed', 'sendAllowed', 'readBackRequired', 'readBackRequired',
      'readBackRequired', 'readBackRequired', 'readBackRequired', 'readBackRequired', 'readBackRequired', 'readBackRequired'];
    expect(vectors.map(([attempt, marker]) => decideDispatch(callId, attempt, marker).kind)).toEqual(expectedKinds);
    expect(vectors.map(([attempt, marker]) => decideDispatch(callId, attempt, marker, undefined, 1).kind)).toEqual(expectedKinds);
  });

  it('bounds sends without a marker by one per earlier attempt', () => {
    expect(decideDispatch(callId, 1, undefined, undefined, 3)).toEqual({ kind: 'sendAllowed', priorSendUpperBound: 0 });
    expect(decideDispatch(callId, 3, undefined, undefined, 3)).toEqual({ kind: 'readBackThenResendAllowed', priorSendUpperBound: 2 });
    expect(decideDispatch(callId, 3, undefined, undefined, 2)).toEqual({ kind: 'readBackRequired', priorSendUpperBound: 2 });
  });

  it('adds each later attempt without a checkpoint to a recorded send count', () => {
    const claimed = { callId, attempt: 2, state: 'claimed', sendCount: 2 } as const;
    expect(decideDispatch(callId, 3, claimed, undefined, 3)).toMatchObject({ kind: 'readBackThenResendAllowed', priorSendUpperBound: 2, marker: claimed });
    expect(decideDispatch(callId, 3, claimed, undefined, 2)).toMatchObject({ kind: 'readBackRequired', priorSendUpperBound: 2 });
    expect(decideDispatch(callId, 4, claimed, undefined, 3)).toMatchObject({ kind: 'readBackRequired', priorSendUpperBound: 3 });
    expect(decideDispatch(callId, 4, claimed, undefined, 4)).toMatchObject({ kind: 'readBackThenResendAllowed', priorSendUpperBound: 3 });
    expect(decideDispatch(callId, 5, { callId, attempt: 2, state: 'notSent', sendCount: 1 }, undefined, 4)).toMatchObject({ kind: 'readBackThenResendAllowed', priorSendUpperBound: 3 });
  });

  it('reads a legacy marker as a claim for every attempt up to its own', () => {
    expect(decideDispatch(callId, 2, { callId, attempt: 1, state: 'claimed' }, undefined, 2)).toMatchObject({ kind: 'readBackThenResendAllowed', priorSendUpperBound: 1 });
    expect(decideDispatch(callId, 3, { callId, attempt: 2, state: 'claimed' }, undefined, 2)).toMatchObject({ kind: 'readBackRequired', priorSendUpperBound: 2 });
    expect(decideDispatch(callId, 4, { callId, attempt: 2, state: 'notSent' }, undefined, 3)).toMatchObject({ kind: 'readBackThenResendAllowed', priorSendUpperBound: 2 });
    expect(decideDispatch(callId, 4, { callId, attempt: 2, state: 'canResend' }, undefined, 2)).toMatchObject({ kind: 'readBackRequired', priorSendUpperBound: 2 });
    expect(decideDispatch(callId, 3, { callId, attempt: 2, state: 'claimed', sendCount: 0 }, undefined, 3)).toMatchObject({ kind: 'readBackThenResendAllowed', priorSendUpperBound: 2 });
  });

  it('keeps a proven not-sent or unprocessed retry send-allowed and carries its count', () => {
    expect(decideDispatch(callId, 2, { callId, attempt: 1, state: 'notSent' }, undefined, 3)).toMatchObject({ kind: 'sendAllowed', priorSendUpperBound: 0 });
    expect(decideDispatch(callId, 7, { callId, attempt: 6, state: 'notSent', sendCount: 2 }, undefined, 3)).toMatchObject({ kind: 'sendAllowed', priorSendUpperBound: 2 });
    expect(decideDispatch(callId, 8, { callId, attempt: 7, state: 'canResend', sendCount: 2 }, undefined, 2)).toMatchObject({ kind: 'sendAllowed', priorSendUpperBound: 2 });
  });

  it('reads back a provider ID and permits a resend only below the cap', () => {
    const accepted = { callId, attempt: 8, state: 'claimed', providerId: 'object-43', sendCount: 3 } as const;
    expect(decideDispatch(callId, 9, accepted, undefined, 4)).toMatchObject({ kind: 'readBackThenResendAllowed', priorSendUpperBound: 3, marker: { providerId: 'object-43' } });
    expect(decideDispatch(callId, 9, accepted, undefined, 3)).toMatchObject({ kind: 'readBackRequired', priorSendUpperBound: 3, marker: { providerId: 'object-43' } });
    expect(decideDispatch(callId, 2, { callId, attempt: 1, state: 'canResend', providerId: 'item-1' }, undefined, 2)).toMatchObject({ kind: 'readBackThenResendAllowed', priorSendUpperBound: 0 });
  });

  it('never resends from a checkpoint of the current or a later attempt', () => {
    expect(decideDispatch(callId, 2, { callId, attempt: 2, state: 'claimed', sendCount: 1 }, undefined, 10)).toMatchObject({ kind: 'readBackRequired', priorSendUpperBound: 1 });
    expect(decideDispatch(callId, 2, { callId, attempt: 3, state: 'notSent' }, undefined, 10)).toMatchObject({ kind: 'readBackRequired', priorSendUpperBound: 2 });
  });

  it('fails closed for foreign or invalid checkpoints and invalid caps', () => {
    expect(decideDispatch(callId, 2, { callId: foreignCallId, attempt: 1, state: 'claimed', sendCount: 1 }, undefined, 10).kind).toBe('failClosed');
    for (const sendCount of [-1, 1.5, '2', null]) expect(decideDispatch(callId, 2, { callId, attempt: 1, state: 'claimed', sendCount }, undefined, 10).kind).toBe('failClosed');
    expect(decideDispatch(callId, 2, undefined, new Error('heartbeat failed'), 10).kind).toBe('failClosed');
    for (const maxSends of [0, 11, 2.5, Number.NaN]) expect(decideDispatch(callId, 1, undefined, undefined, maxSends)).toMatchObject({ kind: 'failClosed', error: expect.any(TypeError) });
    expect(decideDispatch(callId, 2, { callId, attempt: 1, state: 'claimed' }, new Error('heartbeat failed'), 3).priorSendUpperBound).toBeUndefined();
  });
});
