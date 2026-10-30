// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import { CallIDSchema, type CallID } from '../types.js';

/** Last known write state of one Dex Execute attempt. */
export const MarkerStateSchema = z.enum(['notSent', 'claimed', 'canResend']);
/** Last known write state of one Dex Execute attempt. */
export type MarkerState = z.infer<typeof MarkerStateSchema>;

/** Secret-free heartbeat marker shared with the Go single-dispatch fence. */
export const MarkerSchema = z.strictObject({ callId: CallIDSchema, attempt: z.number().int().positive(), state: MarkerStateSchema, providerId: z.string().min(1).optional() });
/** Secret-free heartbeat checkpoint for one call and attempt. */
export type Marker = z.infer<typeof MarkerSchema>;

/** Action allowed before credentials or provider lookups. */
export type DecisionKind = 'sendAllowed' | 'readBackRequired' | 'failClosed';

/** Decision from the most recent heartbeat checkpoint. */
export interface Decision {
  /** Allowed action. */ kind: DecisionKind;
  /** Prior valid checkpoint, when available. */ marker?: Marker;
  /** Safe reason for an unreadable or invalid checkpoint. */ error?: Error;
}

/** Decide whether this attempt may dispatch from a prior heartbeat value. */
export function decideDispatch(callId: CallID, attempt: number, lastHeartbeat: unknown, readError?: Error): Decision {
  if (!CallIDSchema.safeParse(callId).success || !Number.isSafeInteger(attempt) || attempt < 1) return { kind: 'failClosed', error: new TypeError('single dispatch requires a call and positive attempt') };
  if (readError) return { kind: 'failClosed', error: readError };
  if (lastHeartbeat === undefined || lastHeartbeat === null) return { kind: attempt === 1 ? 'sendAllowed' : 'readBackRequired' };
  const parsed = MarkerSchema.safeParse(lastHeartbeat);
  if (!parsed.success) return { kind: 'failClosed', error: new TypeError('invalid single-dispatch checkpoint') };
  const marker = parsed.data;
  if (marker.callId !== callId) return { kind: 'failClosed', error: new TypeError('foreign single-dispatch checkpoint') };
  if (marker.attempt === attempt - 1 && (marker.state === 'notSent' || marker.state === 'canResend') && !marker.providerId) return { kind: 'sendAllowed', marker };
  return { kind: 'readBackRequired', marker };
}

export * from './fence.js';
