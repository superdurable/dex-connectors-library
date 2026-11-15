// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Shared harness of the poll Trigger integration tests: an in-process change-log provider, its fetcher,
// and simulated application processes that share one project object store across restarts.
import assert from 'node:assert/strict';
import { once } from 'node:events';
import { createServer } from 'node:http';
import { setTimeout as delay } from 'node:timers/promises';
import { URL } from 'node:url';
import { TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext } from '../../dist/index.js';
import { TriggerInbox, newDurableTriggerTarget, newPollCursorStore } from '../../dist/projectconfig/index.js';
import { markCursorInvalid, markRateLimited, newBindingPoller, newRunner, waitForPollRequest } from '../../dist/pollingtrigger/index.js';
import { parseRetryAfter } from '../../dist/providerhttp/index.js';

export const pollScope = { projectId: 'poll-trigger-tests', kind: 'live' };
export const pollConnection = { provider: 'changelog', name: 'local' };
export const pollDefinition = { trigger: { connectorId: 'changelog', triggerName: 'itemChanged' }, description: 'Receive a change-log item.' };

/** An in-process provider with sequence-numbered changes and scripted failures. */
export class ChangeLogProvider {
  changes = [];
  responses = [];
  requests = [];

  async start() {
    this.server = createServer((request, response) => this.serve(request, response));
    this.server.listen(0, '127.0.0.1');
    await once(this.server, 'listening');
    this.baseURL = `http://127.0.0.1:${this.server.address().port}`;
  }

  serve(request, response) {
    const url = new URL(request.url, this.baseURL);
    this.requests.push({ path: url.pathname, after: url.searchParams.get('after'), at: Date.now() });
    if (this.responses.length > 0 && url.pathname === '/changes') {
      const scripted = this.responses.shift();
      if (scripted.retryAfter) response.setHeader('Retry-After', scripted.retryAfter);
      response.writeHead(scripted.status).end();
      return;
    }
    if (url.pathname === '/latest') return writeJSON(response, { latest: this.changes.length });
    if (url.pathname !== '/changes') return void response.writeHead(404).end();
    const after = Number(url.searchParams.get('after'));
    const limit = Number(url.searchParams.get('limit'));
    if (!/^\d+$/.test(url.searchParams.get('after') ?? '') || !Number.isInteger(limit) || after > this.changes.length) return void response.writeHead(410).end();
    const end = Math.min(after + limit, this.changes.length);
    writeJSON(response, { changes: this.changes.slice(after, end), hasMore: end < this.changes.length });
  }

  /** Add changes for new item IDs and return them. */
  appendItems(prefix, count) {
    const itemIds = [];
    for (let index = 0; index < count; index++) {
      const itemId = `${prefix}-${index}`;
      this.changes.push({ itemId, sequence: this.changes.length + 1 });
      itemIds.push(itemId);
    }
    return itemIds;
  }

  script(...responses) { this.responses.push(...responses); }
  changeRequests() { return this.requests.filter(request => request.path === '/changes'); }
  /** Drop every change, so any stored cursor past zero is answered with 410 Gone. */
  truncate() { this.changes = []; }
  async close() { await new Promise(resolve => this.server.close(resolve)); }
}

function writeJSON(response, value) {
  response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify(value));
}

/** The connector-side fetcher for the change-log provider. */
export class ChangeLogFetcher {
  constructor(baseURL, pageSize) { this.baseURL = baseURL; this.pageSize = pageSize; }

  async seedCursor(context) {
    const latest = await this.get(context, '/latest');
    return { token: String(latest.latest) };
  }

  async fetchChanges(context, cursor) {
    const listed = await this.get(context, `/changes?after=${cursor.token}&limit=${this.pageSize}`);
    let next = cursor.token;
    const changes = listed.changes.map(change => {
      next = String(change.sequence);
      return { id: `${change.itemId}@${change.sequence}`, occurredAt: new Date(), payload: change };
    });
    return { changes, next: { token: next }, hasMorePages: listed.hasMore };
  }

  async get(context, path) {
    await waitForPollRequest(context);
    const response = await globalThis.fetch(this.baseURL + path, { signal: context.signal });
    if (response.status === 410) throw markCursorInvalid(new Error(`change log cursor expired: ${response.status}`));
    if (response.status === 429) throw markRateLimited(new Error(`change log rate limited: ${response.status}`), parseRetryAfter(response.headers.get('Retry-After') ?? ''));
    if (response.status !== 200) throw new Error(`change log request failed: ${response.status}`);
    return response.json();
  }
}

/** Keeps every Trigger record for assertions. */
export function newLogRecorder() {
  const records = [];
  return {
    records,
    logger: { isEnabled: () => true, log: (level, message, attributes) => records.push({ level, message, attributes: { ...attributes } }) },
    find: (message, attributes = {}) => records.filter(record => record.message === message && Object.entries(attributes).every(([key, value]) => record.attributes[key] === value)),
  };
}

/** Poll until check returns a truthy value; fails after timeoutMs. */
export async function eventually(check, message, timeoutMs = 30_000) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const value = await check();
    if (value) return value;
    if (Date.now() > deadline) assert.fail(`timed out: ${message}`);
    await delay(50);
  }
}

/** Wait until Dex knows the Trigger-started Flow, then for its completed output. */
export async function waitForItemFlow(client, flowId) {
  await eventually(() => client.describeFlow(flowId).then(() => true, () => false), `Flow ${flowId} starts`, 60_000);
  const result = await client.waitForFlow(flowId, 60_000);
  assert.equal(result.status, 'completed', flowId);
  return result.singleOutput();
}

/** One test's Dex client, provider, and project storage, kept across simulated restarts. */
export class PollEnvironment {
  constructor({ client, itemFlow, objects, provider, logs, testRunId }) {
    Object.assign(this, { client, itemFlow, objects, provider, logs, testRunId });
    this.delivered = new Map();
  }

  flowId(eventId) { return `poll-trigger-${this.testRunId}-${eventId.replaceAll('@', '-')}`; }
  itemFlowId(itemId, sequence) { return this.flowId(`${itemId}@${sequence}`); }
  inboxKey(bindingName) { return { connectorId: 'changelog', connectionName: pollConnection.name, triggerName: pollDefinition.trigger.triggerName, bindingName }; }
  deliveries(eventId) { return this.delivered.get(eventId) ?? 0; }

  /** Start the item Flow and count consumed deliveries per event across processes. */
  flowTarget() {
    const start = newDexFlowTriggerTarget(this.client, this.itemFlow, () => true, event => this.flowId(event.id), event => event.payload, { logger: this.logs.logger });
    return {
      handleTrigger: async (context, event) => {
        await start.handleTrigger(context, event);
        this.delivered.set(event.id, this.deliveries(event.id) + 1);
      },
    };
  }

  cursorStore(bindingName) { return newPollCursorStore(this.objects, pollScope, this.inboxKey(bindingName)); }
  async cursorState(bindingName) { return (await this.cursorStore(bindingName).readPollCursor()).state; }
  async pendingEvents(bindingName) { return new TriggerInbox(this.objects, pollScope, this.inboxKey(bindingName)).pending(); }
  newRunner(...pollers) { return newRunner({ connection: pollConnection, schedule: { intervalMs: 1000, jitterFraction: 0, maxBackoffMs: 2000 }, pollers, logger: this.logs.logger }); }

  /** Start one simulated application process: one runner over fresh objects bound to the shared store. */
  startProcess({ bindingName = 'main', pageSize = 100, wrapCursor } = {}) {
    const controller = new globalThis.AbortController();
    const key = this.inboxKey(bindingName);
    const target = newDurableTriggerTarget(new TriggerInbox(this.objects, pollScope, key), key, this.flowTarget(), { logger: this.logs.logger });
    const cursor = wrapCursor ? wrapCursor(this.cursorStore(bindingName), () => controller.abort()) : this.cursorStore(bindingName);
    const poller = newBindingPoller({
      definition: pollDefinition, binding: { connection: pollConnection, trigger: pollDefinition.trigger, name: bindingName },
      cursor, fetcher: new ChangeLogFetcher(this.provider.baseURL, pageSize), target,
    });
    const result = this.newRunner(poller).run(newTriggerContext(controller.signal)).then(() => undefined, error => error);
    return {
      result,
      async stop() {
        controller.abort();
        assert.ok(await result instanceof TriggerDeliveryAbortedError, 'the process stops on abort');
      },
    };
  }

  async waitForSeed(bindingName = 'main') { await eventually(() => this.cursorState(bindingName), `binding ${bindingName} seeds`, 20_000); }

  /** Run one process until it seeds the main binding's cursor, then stop it. */
  async seedAndStop() {
    const process = this.startProcess();
    await this.waitForSeed();
    await process.stop();
  }
}

/** Runs hooks around the first commit after the cursor is seeded. */
export function commitHookCursor(cursor, { beforeCommit, afterCommit }) {
  let hasFired = false;
  return {
    readPollCursor: () => cursor.readPollCursor(),
    async replacePollCursor(revision, state) {
      const shouldFire = !hasFired && state.commitCount > 1;
      hasFired ||= shouldFire;
      if (shouldFire && beforeCommit) beforeCommit();
      const record = await cursor.replacePollCursor(revision, state);
      if (shouldFire && afterCommit) afterCommit();
      return record;
    },
  };
}
