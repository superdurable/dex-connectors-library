// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newPollFeedCursorStore, type PollCursorRecord, type PollCursorState } from '../projectconfig/poll_cursor.js';
import { InMemoryObjectStore } from '../projectconfig/storage.js';

/**
 * Persists one poller's cursor with compare-and-swap. The PollCursorStore from LoadedProject.pollCursorStore
 * or LoadedProject.pollFeedCursorStore implements it.
 */
export interface CursorStore {
  /** Return the current record; its state is undefined until the cursor is seeded. */
  readPollCursor(): Promise<PollCursorRecord>;
  /** Write state only while the head is at expectedRevision, rejecting with ConflictError otherwise. */
  replacePollCursor(expectedRevision: string, state: PollCursorState): Promise<PollCursorRecord>;
}

/**
 * Keep one cursor in process memory with the same encoding, bounds, and compare-and-swap rules as project
 * storage. Use it in tests and in applications without project storage. A restart reseeds, so changes made
 * while the process was stopped are not delivered.
 */
export function newMemoryCursorStore(): CursorStore {
  return newPollFeedCursorStore(new InMemoryObjectStore(), { projectId: 'memory', kind: 'live' }, { connectorId: 'memory', connectionName: 'memory', feedName: 'memoryCursor' });
}
