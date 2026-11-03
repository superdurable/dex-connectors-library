// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { GetEventsInputSchema, newProjectConnection, type EventsOutput } from '@superdurable/connector-mailgun';
import { ConnectionName, GetEventsFlow } from './flow/workflow.js';

const inputFile = process.argv[2];
if (!inputFile) throw new TypeError('Mailgun events input JSON file is required');
const input = GetEventsInputSchema.parse(JSON.parse(await readFile(inputFile, 'utf8')));
const project = await loadFromEnvironment();
const flow = new GetEventsFlow(newProjectConnection(project, ConnectionName));
const directory = await mkdtemp(join(tmpdir(), 'dex-mailgun-events-client-'));
const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
const client = new Client(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
try {
  const flowId = `mailgun-events-${randomUUID()}`;
  await client.startFlow(flow, flowId, input);
  const result = await client.waitForFlow(flowId, 90_000);
  if (result.status !== 'completed') throw new Error('Mailgun events Flow did not complete');
  process.stdout.write(`${JSON.stringify(result.singleOutput<EventsOutput>())}\n`);
} finally {
  await client.close();
  cache.close();
  await rm(directory, { recursive: true, force: true });
}
