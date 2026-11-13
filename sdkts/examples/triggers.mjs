// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { once } from 'node:events';
import { createServer } from 'node:http';
import { StaticCredentialProvider, newTrigger, newTriggerContext } from '../dist/index.js';
import { InMemoryObjectStore, TriggerInbox, newDurableTriggerTarget } from '../dist/projectconfig/index.js';
import { RelaySignatureHeader, RelayTimestampHeader, signRelayedRequest, verifyRelaySignature } from '../dist/relaysignature/index.js';
import { WebhookEndpoint } from '../dist/webhooktrigger/index.js';

// A connector's webhook endpoint verifies each request, decodes one event, and records it before answering 200.
const connection = { provider: 'sample', name: 'workspace' };
const scopeToken = 'example-scope-token';
const endpoint = new WebhookEndpoint({
  connectorId: 'sample', triggerName: 'itemCreated', connection, maxBodyBytes: 1 << 20,
  credentials: new StaticCredentialProvider([[connection, {}]]),
  verifyRequest: async request => {
    const verification = verifyRelaySignature(scopeToken, request.headers, request.body, request.receivedAt);
    if (!verification.isValid) throw new Error(`relayed request refused: ${verification.refusal}`);
  },
  decodeEvent: request => {
    const body = JSON.parse(Buffer.from(request.body).toString('utf8'));
    return { id: body.eventId, occurredAt: new Date(body.occurredAt), payload: { itemId: body.itemId } };
  },
});

// The inbox keeps every acknowledged event until the application target consumes it. This in-memory store
// lasts only for the process; an application uses project.triggerInbox(key), which persists.
const delivered = [];
const key = { connectorId: 'sample', connectionName: 'workspace', triggerName: 'itemCreated', bindingName: 'start' };
const inbox = new TriggerInbox(new InMemoryObjectStore(), { projectId: 'example', kind: 'preview' }, key);
const target = newDurableTriggerTarget(inbox, key, { handleTrigger: async (_context, event) => { delivered.push(event.payload.itemId); } });
const definition = { trigger: { connectorId: 'sample', triggerName: 'itemCreated' }, description: 'Receive a new item.' };
const runner = newTrigger({ definition, binding: { connection, trigger: definition.trigger, name: 'start' }, source: endpoint.newSource(), target });

const server = createServer((request, response) => { void endpoint.serveHTTP(request, response); });
server.listen(0, '127.0.0.1');
await once(server, 'listening');
const stop = new AbortController();
const running = runner.run(newTriggerContext(stop.signal)).catch(() => undefined);
try {
  const body = Buffer.from(JSON.stringify({ eventId: 'Ev1', occurredAt: '2025-10-12T00:00:00Z', itemId: 'item-1' }));
  const now = new Date();
  const response = await fetch(`http://127.0.0.1:${server.address().port}/`, {
    method: 'POST', body,
    headers: { [RelayTimestampHeader]: String(Math.floor(now.getTime() / 1000)), [RelaySignatureHeader]: signRelayedRequest(scopeToken, now, body) },
  });
  if (response.status !== 200) throw new Error(`webhook answered ${response.status}`);
  for (let attempt = 0; delivered.length === 0 && attempt < 100; attempt++) await new Promise(resolve => setTimeout(resolve, 10));
  if (delivered[0] !== 'item-1' || (await inbox.pending()).length !== 0) throw new Error('Trigger example did not deliver the event');
} finally {
  stop.abort();
  await running;
  server.close();
}
