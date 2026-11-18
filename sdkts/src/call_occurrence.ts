// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { AsyncContext } from '@superdurable/dex';
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
 * Marks a Mutation call of `operation` as running in the context's attempt and returns the function
 * that ends it, or `undefined` while an earlier call of the same operation still runs.
 */
export function beginRunningMutation(context: AsyncContext, operation: OperationRef): (() => void) | undefined {
  const holder = globalThis as unknown as Record<symbol, WeakMap<object, RunningMutations> | undefined>;
  const registry = holder[runningMutationRegistryKey] ??= new WeakMap<object, RunningMutations>();
  let running = registry.get(context);
  if (!running || running.attempt !== context.attempt) {
    running = { attempt: context.attempt, operations: new Set() };
    registry.set(context, running);
  }
  const key = operationKey(operation);
  if (running.operations.has(key)) return undefined;
  const operations = running.operations;
  operations.add(key);
  return () => { operations.delete(key); };
}

/** Connector IDs cannot contain '/', so the key is unambiguous. */
function operationKey(operation: OperationRef): string {
  return `${operation.connectorId}/${operation.operationId}`;
}
