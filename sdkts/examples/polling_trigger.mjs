// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newTriggerContext } from '../dist/index.js';
import { InMemoryObjectStore, TriggerInbox, newDurableTriggerTarget, newPollCursorStore } from '../dist/projectconfig/index.js';
import { markCursorInvalid, newBindingPoller, newRunner, waitForPollRequest } from '../dist/pollingtrigger/index.js';

// A fake provider change log; a connector would call the provider's change API instead.
const changeLog = [{ itemId: 'existing', sequence: 1 }];
const api = {
  latestSequence: async () => changeLog.length,
  listChanges: async (after, limit) => ({ changes: changeLog.slice(after, after + limit), hasMore: after + limit < changeLog.length }),
};

// The connector's fetcher: seed at the current position, then one page of changes after a cursor. Every
// event ID is a pure function of the change, so a refetch after a crash yields the same IDs.
const fetcher = {
  async seedCursor(context) {
    await waitForPollRequest(context);
    return { token: String(await api.latestSequence()) };
  },
  async fetchChanges(context, cursor) {
    const after = Number(cursor.token);
    if (!Number.isSafeInteger(after)) throw markCursorInvalid(new Error('change-log cursor is not a sequence'));
    await waitForPollRequest(context);
    const listed = await api.listChanges(after, 100);
    const changes = listed.changes.map(change => ({ id: `${change.itemId}@${change.sequence}`, occurredAt: new Date(), payload: change }));
    return { changes, next: { token: String(changes.at(-1)?.payload.sequence ?? after) }, hasMorePages: listed.hasMore };
  },
};

// The application wires one binding. These in-memory stores last only for the process; an application
// uses project.triggerInbox(key) and project.pollCursorStore(key), which persist in project storage.
const objects = new InMemoryObjectStore();
const scope = { projectId: 'example', kind: 'preview' };
const connection = { provider: 'changelog', name: 'default' };
const definition = { trigger: { connectorId: 'changelog', triggerName: 'itemChanged' }, description: 'Receive a changed item.' };
const binding = { connection, trigger: definition.trigger, name: 'sync' };
const key = { connectorId: 'changelog', connectionName: connection.name, triggerName: 'itemChanged', bindingName: 'sync' };
const delivered = [];
// Durable inbox: events are recorded here before the cursor moves past them.
const target = newDurableTriggerTarget(new TriggerInbox(objects, scope, key), key, { handleTrigger: async (_context, event) => { delivered.push(event.id); } });
const cursor = newPollCursorStore(objects, scope, key);
const poller = newBindingPoller({ definition, binding, cursor, fetcher, target });
const runner = newRunner({ connection, schedule: { intervalMs: 1000, maxBackoffMs: 60_000 }, pollers: [poller] });

const stop = new AbortController();
// Replays the inbox, seeds on first start, then polls until the signal aborts.
const running = runner.run(newTriggerContext(stop.signal)).catch(() => undefined);
try {
  for (let attempt = 0; !(await cursor.readPollCursor()).state && attempt < 300; attempt++) await new Promise(resolve => setTimeout(resolve, 10));
  changeLog.push({ itemId: 'item-2', sequence: 2 });
  runner.wakePoller('sync');
  for (let attempt = 0; delivered.length === 0 && attempt < 300; attempt++) await new Promise(resolve => setTimeout(resolve, 10));
  if (delivered.join() !== 'item-2@2') throw new Error(`polling Trigger example delivered ${JSON.stringify(delivered)}, not only the new change`);
} finally {
  stop.abort();
  await running;
}
