// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection, type ListChannelMessagesInput } from '@superdurable/connector-slack';
import { ConnectionName, ListChannelMessagesFlow } from './flow/workflow.js';

const project = await loadFromEnvironment();
const flow = new ListChannelMessagesFlow(newProjectConnection(project, ConnectionName));
const directory = await mkdtemp(join(tmpdir(), 'dex-slack-channel-history-starter-'));
const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
const client = new Client(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
try {
  const input = JSON.parse(process.argv[2] ?? '{}') as ListChannelMessagesInput;
  const flowId = `slack-channel-history-${randomUUID()}`;
  await client.startFlow(flow, flowId, input);
  const result = await client.waitForFlow(flowId, 60_000);
  if (result.status !== 'completed') throw new Error(`Slack channel history Flow ${result.status}`);
  console.log(JSON.stringify(result.singleOutput()));
} finally {
  await client.close(); cache.close();
  await rm(directory, { recursive: true, force: true });
}
