// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFile } from 'node:fs/promises';
import { describe, expect, it } from 'vitest';
import { MarkerSchema, decideDispatch } from '../src/singledispatch/index.js';

const callId = '5af8fafa-06ff-5b2d-bb13-04693809fabf';

describe('single-dispatch marker decisions', () => {
  it('round-trips the Go heartbeat marker fixture', async () => {
    const markers: unknown = JSON.parse(await readFile(new URL('../../conformance/single-dispatch-markers.json', import.meta.url), 'utf8'));
    const parsed = MarkerSchema.array().parse(markers);
    expect(JSON.parse(JSON.stringify(parsed))).toEqual(markers);
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
    expect(decideDispatch(callId, 2, { callId: 'aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa', attempt: 1, state: 'notSent' }).kind).toBe('failClosed');
    expect(decideDispatch(callId, 2, { callId, attempt: 1, state: 'unknown' }).kind).toBe('failClosed');
  });
});
