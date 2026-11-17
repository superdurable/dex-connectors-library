// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { AsyncContext } from '@superdurable/dex';
import { MarkerSchema, type Marker } from './index.js';

/** Heartbeat field mapping each call ID of a Step attempt to its marker. */
export const CheckpointMarkersField = 'singleDispatchMarkers';

/**
 * Shared state of one Step attempt. Several SDK copies may share it, so it holds plain data only;
 * changing its shape requires a new registry key.
 */
interface StepCheckpoint {
  attempt: number;
  markers: Map<string, Marker>;
  tail: Promise<void>;
}

const stepCheckpointRegistryKey = Symbol.for('@superdurable/connectors-sdk/singleDispatchCheckpoints/v1');

/** Returns the checkpoint shared by every Fence of the context's current attempt, in any SDK copy. */
export function stepCheckpointFor(context: AsyncContext): StepCheckpoint {
  const holder = globalThis as unknown as Record<symbol, WeakMap<object, StepCheckpoint> | undefined>;
  const registry = holder[stepCheckpointRegistryKey] ??= new WeakMap<object, StepCheckpoint>();
  const existing = registry.get(context);
  if (existing && existing.attempt === context.attempt) return existing;
  const checkpoint: StepCheckpoint = { attempt: context.attempt, markers: previousAttemptMarkers(context), tail: Promise.resolve() };
  registry.set(context, checkpoint);
  return checkpoint;
}

/**
 * Persists every known marker with `marker` replacing its call ID's entry, committing it only after
 * the heartbeat succeeds. Writes queue so the last persisted value holds every committed entry.
 */
export async function recordStepCheckpoint(context: AsyncContext, checkpoint: StepCheckpoint, marker: Marker): Promise<void> {
  const write = checkpoint.tail.then(async () => {
    const markers = new Map(checkpoint.markers);
    markers.set(marker.callId, marker);
    await context.recordHeartbeat(checkpointHeartbeat(markers));
    checkpoint.markers = markers;
  });
  checkpoint.tail = write.catch(() => undefined);
  await write;
}

/**
 * Reads the checkpoint map, dropping every entry that is not a valid marker or whose key differs
 * from its `callId`. Any other value, such as a single marker from an earlier release, yields none.
 * The format is internal to the connector SDKs and may change.
 */
export function markersFromHeartbeat(heartbeat: unknown): Map<string, Marker> {
  const markers = new Map<string, Marker>();
  if (!isPlainObject(heartbeat)) return markers;
  const entries = heartbeat[CheckpointMarkersField];
  if (!isPlainObject(entries)) return markers;
  for (const [callId, entry] of Object.entries(entries)) {
    const parsed = MarkerSchema.safeParse(entry);
    if (parsed.success && parsed.data.callId === callId) markers.set(callId, parsed.data);
  }
  return markers;
}

/** Encodes the checkpoint map with call IDs in sorted order and Go's marker field order. */
function checkpointHeartbeat(markers: Map<string, Marker>): Record<string, Record<string, Marker>> {
  const entries: Record<string, Marker> = {};
  for (const callId of [...markers.keys()].sort()) {
    const marker = markers.get(callId)!;
    entries[callId] = { callId: marker.callId, attempt: marker.attempt, state: marker.state, ...(marker.providerId ? { providerId: marker.providerId } : {}), ...(marker.sendCount ? { sendCount: marker.sendCount } : {}) };
  }
  return { [CheckpointMarkersField]: entries };
}

/** An unreadable heartbeat seeds nothing, which only overcounts earlier sends. */
function previousAttemptMarkers(context: AsyncContext): Map<string, Marker> {
  try {
    return context.hasLastHeartbeatValue() ? markersFromHeartbeat(context.getLastHeartbeatValue()) : new Map();
  } catch {
    return new Map();
  }
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
