// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { ListOutboundMessagesInputSchema, newProjectConnection, type MessagePage } from '../../src/index.js';
import { ConnectionName, OutboundLookupFlow } from './flow.js';

try {
  const inputFile = process.argv[2];
  if (!inputFile) throw new TypeError('Postmark input JSON file is required');
  const input = ListOutboundMessagesInputSchema.parse(JSON.parse(await readFile(inputFile, 'utf8')));
  const project = await loadFromEnvironment();
  const flow = new OutboundLookupFlow(newProjectConnection(project, ConnectionName));
  const directory = await mkdtemp(join(tmpdir(), 'dex-postmark-client-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
  try {
    const flowID = `postmark-outbound-${randomUUID()}`;
    await client.startFlow(flow, flowID, input);
    const result = await client.waitForFlow(flowID, 90_000);
    if (result.status !== 'completed') throw new Error('Postmark outbound lookup did not complete');
    process.stdout.write(`${JSON.stringify(result.singleOutput<MessagePage>())}\n`);
  } finally {
    await client.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
} catch {
  console.error('Postmark outbound lookup could not start or complete.');
  process.exitCode = 1;
}
