// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { AsyncContext } from '@superdurable/dex';
import type { Call } from './credentials.js';
import type { OperationRef } from './types.js';

/** SDK copies share this plain data; changing its shape requires a new registry key. */
interface CallOccurrences {
  attempt: number;
  counts: Map<string, number>;
}

const callOccurrenceRegistryKey = Symbol.for('@superdurable/connectors-sdk/callOccurrences/v1');

/** Returns the one-based ordinal of this call of `operation` in the context's current attempt. */
export function nextCallOccurrence(context: AsyncContext, operation: OperationRef): number {
  const holder = globalThis as unknown as Record<symbol, WeakMap<object, CallOccurrences> | undefined>;
  const registry = holder[callOccurrenceRegistryKey] ??= new WeakMap<object, CallOccurrences>();
  let occurrences = registry.get(context);
  if (!occurrences || occurrences.attempt !== context.attempt) {
    occurrences = { attempt: context.attempt, counts: new Map() };
    registry.set(context, occurrences);
  }
  const key = operationKey(operation);
  const occurrence = (occurrences.counts.get(key) ?? 0) + 1;
  occurrences.counts.set(key, occurrence);
  return occurrence;
}

/** SDK copies share this plain data; changing its shape requires a new registry key. */
interface RunningMutations {
  attempt: number;
  operations: Set<string>;
}

const runningMutationRegistryKey = Symbol.for('@superdurable/connectors-sdk/runningMutations/v1');

/**
 * SDK copies share this plain data; changing its shape requires a new registry key. Keys are
 * construction ordinals from `createCall`: a refused call that never began frees its ordinal.
 */
interface MutationOccurrences {
  attempt: number;
  operations: Map<string, { freedOrdinals: Set<number>; begunOccurrences: Map<number, number>; ordinalHoldingOccurrence: Map<number, number> }>;
}

const mutationOccurrenceRegistryKey = Symbol.for('@superdurable/connectors-sdk/mutationOccurrences/v1');

/** A running Mutation: the occurrence its call ID runs with, or `undefined` to keep it, and its end. */
export interface BegunMutation {
  kind: 'begun';
  occurrence: number | undefined;
  end: () => void;
}

/** Why a Mutation call was refused before invocation; nothing was sent. */
export interface MutationRefusal {
  kind: 'concurrent' | 'occurrenceHeld';
}

/** The construction ordinal and rebuilder of a `createCall` call; application-built calls are absent. */
const callOccurrenceBuilders = new WeakMap<Call, { ordinal: number; buildForOccurrence: (occurrence: number) => Call }>();

/** Lets `runMutation` give a `createCall` call the call ID of its shifted occurrence. */
export function registerCallOccurrenceBuilder(call: Call, ordinal: number, buildForOccurrence: (occurrence: number) => Call): void {
  callOccurrenceBuilders.set(call, { ordinal, buildForOccurrence });
}

/**
 * Marks a Mutation call of `operation` as running in the context's attempt and returns the occurrence
 * its call ID runs with. As in Go's beginMutationOccurrence, a call refused as concurrent before it
 * ever began consumes no occurrence: later `createCall` calls shift down past its ordinal. A call keeps
 * its first begun occurrence on later runs, and a call whose occurrence another call holds is refused.
 */
export function beginMutationOccurrence(context: AsyncContext, operation: OperationRef, call: Call): BegunMutation | MutationRefusal {
  const holder = globalThis as unknown as Record<symbol, WeakMap<object, RunningMutations> | WeakMap<object, MutationOccurrences> | undefined>;
  const runningRegistry = (holder[runningMutationRegistryKey] ??= new WeakMap<object, RunningMutations>()) as WeakMap<object, RunningMutations>;
  let running = runningRegistry.get(context);
  if (!running || running.attempt !== context.attempt) {
    running = { attempt: context.attempt, operations: new Set() };
    runningRegistry.set(context, running);
  }
  const occurrenceRegistry = (holder[mutationOccurrenceRegistryKey] ??= new WeakMap<object, MutationOccurrences>()) as WeakMap<object, MutationOccurrences>;
  let occurrences = occurrenceRegistry.get(context);
  if (!occurrences || occurrences.attempt !== context.attempt) {
    occurrences = { attempt: context.attempt, operations: new Map() };
    occurrenceRegistry.set(context, occurrences);
  }
  const key = operationKey(operation);
  let state = occurrences.operations.get(key);
  if (!state) {
    state = { freedOrdinals: new Set(), begunOccurrences: new Map(), ordinalHoldingOccurrence: new Map() };
    occurrences.operations.set(key, state);
  }
  const ordinal = callOccurrenceBuilders.get(call)?.ordinal;
  if (running.operations.has(key)) {
    if (ordinal !== undefined && !state.begunOccurrences.has(ordinal)) state.freedOrdinals.add(ordinal);
    return { kind: 'concurrent' };
  }
  let occurrence: number | undefined;
  if (ordinal !== undefined) {
    occurrence = state.begunOccurrences.get(ordinal);
    if (occurrence === undefined) {
      state.freedOrdinals.delete(ordinal);
      let freedBelow = 0;
      for (const freed of state.freedOrdinals) if (freed < ordinal) freedBelow++;
      occurrence = ordinal - freedBelow;
      if ((state.ordinalHoldingOccurrence.get(occurrence) ?? ordinal) !== ordinal) return { kind: 'occurrenceHeld' };
      state.begunOccurrences.set(ordinal, occurrence);
      state.ordinalHoldingOccurrence.set(occurrence, ordinal);
    }
  }
  const operations = running.operations;
  operations.add(key);
  return { kind: 'begun', occurrence, end: () => { operations.delete(key); } };
}

/** Return `call` with the call ID of `occurrence` when it differs, otherwise `call` itself. */
export function callForBegunOccurrence(call: Call, occurrence: number | undefined): Call {
  if (occurrence === undefined) return call;
  const rebuilt = callOccurrenceBuilders.get(call)?.buildForOccurrence(occurrence);
  return rebuilt && rebuilt.id !== call.id ? rebuilt : call;
}

/** Connector IDs cannot contain '/', so the key is unambiguous. */
function operationKey(operation: OperationRef): string {
  return `${operation.connectorId}/${operation.operationId}`;
}
