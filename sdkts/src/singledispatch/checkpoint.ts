// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { AsyncContext } from '@superdurable/dex';
import { MarkerSchema, type Marker } from './index.js';

/** Heartbeat field mapping each call ID of a Step attempt to its marker. */
const CheckpointMarkersField = 'singleDispatchMarkers';

/** SDK copies share this plain data; changing its shape requires a new registry key. */
interface StepCheckpoint {
  attempt: number;
  markers: Map<string, Marker>;
  tail: Promise<void>;
}

const stepCheckpointRegistryKey = Symbol.for('@superdurable/connectors-sdk/singleDispatchCheckpoints/v1');

/** Returns the checkpoint every Fence of this attempt shares, in any SDK copy. */
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
 * Queued writes commit only after the heartbeat succeeds, so the last value has every entry.
 * `retainOnFailure` keeps a provider ID anyway; it only forces read-back.
 */
export async function recordStepCheckpoint(context: AsyncContext, checkpoint: StepCheckpoint, marker: Marker, retainOnFailure = false): Promise<void> {
  const write = checkpoint.tail.then(async () => {
    const markers = new Map(checkpoint.markers);
    markers.set(marker.callId, marker);
    try { await context.recordHeartbeat(checkpointHeartbeat(markers)); }
    catch (error) {
      if (retainOnFailure) checkpoint.markers = markers;
      throw error;
    }
    checkpoint.markers = markers;
  });
  checkpoint.tail = write.catch(() => undefined);
  await write;
}

/** Drops unreadable or mis-keyed entries; a legacy single marker becomes, and carries forward as, its call's entry. */
export function markersFromHeartbeat(heartbeat: unknown): Map<string, Marker> {
  const markers = new Map<string, Marker>();
  if (!isPlainObject(heartbeat)) return markers;
  if (!(CheckpointMarkersField in heartbeat)) {
    const legacyMarker = markerFromEntry(heartbeat);
    if (legacyMarker) markers.set(legacyMarker.callId, legacyMarker);
    return markers;
  }
  const entries = heartbeat[CheckpointMarkersField];
  if (!isPlainObject(entries)) return markers;
  for (const [callId, entry] of Object.entries(entries)) {
    const marker = markerFromEntry(entry);
    if (marker && marker.callId === callId) markers.set(callId, marker);
  }
  return markers;
}

/** A present count is exact; an absent one counts one send per attempt, excluding a notSent or canResend marker's own. */
function markerFromEntry(entry: unknown): Marker | undefined {
  const parsed = MarkerSchema.safeParse(entry);
  if (!parsed.success) return undefined;
  const marker = parsed.data;
  return { ...marker, sendCount: marker.sendCount ?? (marker.state === 'claimed' ? marker.attempt : marker.attempt - 1) };
}

/** Encodes the checkpoint map with call IDs in sorted order, Go's field order, and every entry's exact count. */
function checkpointHeartbeat(markers: Map<string, Marker>): Record<string, Record<string, Marker>> {
  const entries: Record<string, Marker> = {};
  for (const callId of [...markers.keys()].sort()) {
    const marker = markers.get(callId)!;
    entries[callId] = { callId: marker.callId, attempt: marker.attempt, state: marker.state, ...(marker.providerId ? { providerId: marker.providerId } : {}), sendCount: marker.sendCount ?? 0 };
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
