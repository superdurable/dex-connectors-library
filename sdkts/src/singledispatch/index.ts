// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import { MaximumMaxSends, isValidMaxSends } from '../operation.js';
import { CallIDSchema, type CallID } from '../types.js';
import { markersFromHeartbeat } from './checkpoint.js';

/** Last known write state of one Dex Execute attempt. */
export const MarkerStateSchema = z.enum(['notSent', 'claimed', 'canResend']);
/** Last known write state of one Dex Execute attempt. */
export type MarkerState = z.infer<typeof MarkerStateSchema>;

/**
 * Secret-free checkpoint entry shared with the Go single-dispatch fence. A Fence records it as its
 * call ID's entry in the Step attempt's heartbeat.
 *
 * `sendCount` is the number of possibly effective sends this call ID made up to and including the
 * marker's attempt. Only a Fence with a send cap of at least 2 writes it, and omits it when zero.
 * A marker without it, or with zero, is read as a legacy marker. Field order matches the Go
 * encoding: `providerId`, when present, precedes `sendCount`.
 */
export const MarkerSchema = z.strictObject({
  callId: CallIDSchema, attempt: z.number().int().positive(), state: MarkerStateSchema,
  providerId: z.string().min(1).optional(), sendCount: z.number().int().nonnegative().optional(),
});
/** Secret-free heartbeat checkpoint for one call and attempt. */
export type Marker = z.infer<typeof MarkerSchema>;

/**
 * Action allowed before credentials or provider lookups.
 *
 * - `sendAllowed`: attempt one, or the immediately preceding attempt proved its own send never
 *   happened or was not processed, so Dispatch may send once without read-back. When
 *   `priorSendUpperBound` is above zero, a send from an earlier bounded-resend attempt may still
 *   be effective; the claim counts it.
 * - `readBackThenResendAllowed`: an earlier send may be effective but the send cap is not reached.
 *   The connector reads back when it can and selects the happy path on confirmed success; a
 *   definite "not found", an inconclusive read-back, or no read-back support permits one Dispatch.
 *   Only a Fence or decision with `maxSends` of at least 2 returns it.
 * - `readBackRequired`: read back only; Dispatch is refused and an unconfirmed outcome is uncertain.
 * - `failClosed`: the checkpoint cannot be trusted; Dispatch is refused.
 */
export type DecisionKind = 'sendAllowed' | 'readBackThenResendAllowed' | 'readBackRequired' | 'failClosed';

/** Decision from the most recent heartbeat checkpoint. */
export interface Decision {
  /** Allowed action. */ kind: DecisionKind;
  /** Prior valid checkpoint, when available. */ marker?: Marker;
  /** Safe reason for an unreadable or invalid checkpoint. */ error?: Error;
  /**
   * Upper bound of possibly effective sends this call ID made before the current attempt. It is
   * present on every decision except `failClosed`; with a cap of at least 2, a Dispatch claim
   * records it plus one.
   */
  priorSendUpperBound?: number;
}

/**
 * Decide whether this attempt may dispatch from the previous attempt's whole heartbeat value.
 *
 * Only this call ID's entry in the checkpoint map counts. A missing entry or an unreadable value,
 * including a single marker from an earlier release, counts as no marker: attempt one may send,
 * and a later attempt assumes each earlier attempt sent once. `maxSends` is the send cap from 1
 * to 10. The default 1 disables bounded resend. An invalid call ID, attempt, or cap, a read
 * error, and an own entry with an impossible send count fail closed. The heartbeat format is
 * internal to the connector SDKs and may change.
 */
export function decideDispatch(callId: CallID, attempt: number, lastHeartbeat: unknown, readError?: Error, maxSends = 1): Decision {
  if (!CallIDSchema.safeParse(callId).success || !Number.isSafeInteger(attempt) || attempt < 1) return { kind: 'failClosed', error: new TypeError('single dispatch requires a call and positive attempt') };
  if (!isValidMaxSends(maxSends)) return { kind: 'failClosed', error: new TypeError(`single-dispatch maxSends must be an integer from 1 to ${MaximumMaxSends}`) };
  if (readError) return { kind: 'failClosed', error: readError };
  const marker = markersFromHeartbeat(lastHeartbeat).get(callId);
  if (!marker) {
    if (attempt === 1) return { kind: 'sendAllowed', priorSendUpperBound: 0 };
    return { kind: decideReadBack(attempt - 1, maxSends), priorSendUpperBound: attempt - 1 };
  }
  if (!hasPossibleSendCount(marker)) return { kind: 'failClosed', marker, error: new TypeError('invalid single-dispatch checkpoint') };
  const markerSendCount = sendCountOfMarker(marker);
  // A marker from this or a later attempt would make the gap negative and undercount sends.
  if (marker.attempt >= attempt) return { kind: 'readBackRequired', marker, priorSendUpperBound: markerSendCount };
  const priorSendUpperBound = markerSendCount + (attempt - 1 - marker.attempt);
  if (marker.attempt === attempt - 1 && (marker.state === 'notSent' || marker.state === 'canResend') && !marker.providerId) return { kind: 'sendAllowed', marker, priorSendUpperBound };
  return { kind: decideReadBack(priorSendUpperBound, maxSends), marker, priorSendUpperBound };
}

function decideReadBack(priorSendUpperBound: number, maxSends: number): 'readBackThenResendAllowed' | 'readBackRequired' {
  return maxSends >= 2 && priorSendUpperBound < maxSends ? 'readBackThenResendAllowed' : 'readBackRequired';
}

/** Rejects a send count that one send per attempt could not produce, as Go does. */
function hasPossibleSendCount(marker: Marker): boolean {
  const sendCount = marker.sendCount ?? 0;
  return marker.state === 'claimed' ? sendCount <= marker.attempt : sendCount <= marker.attempt - 1;
}

/** A legacy marker counts its own claim, or every earlier attempt for notSent and canResend. */
function sendCountOfMarker(marker: Marker): number {
  if (marker.sendCount) return marker.sendCount;
  return marker.state === 'claimed' ? marker.attempt : marker.attempt - 1;
}

export * from './fence.js';
