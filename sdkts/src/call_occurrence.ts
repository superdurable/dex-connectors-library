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
  // Connector IDs cannot contain '/', so the key is unambiguous.
  const key = `${operation.connectorId}/${operation.operationId}`;
  const occurrence = (occurrences.counts.get(key) ?? 0) + 1;
  occurrences.counts.set(key, occurrence);
  return occurrence;
}
