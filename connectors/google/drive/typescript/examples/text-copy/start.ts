// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '@superdurable/connector-google-drive';
import { ConnectionName, TextCopyFlow, type TextCopyInput } from './flow.js';

const source = process.argv[2];
if (!source) throw new TypeError('Input JSON file is required');
const input = JSON.parse(await readFile(source, 'utf8')) as TextCopyInput;
if (!input.sourceName || !input.copyName) throw new TypeError('sourceName and copyName are required');
const project = await loadFromEnvironment();
const flow = new TextCopyFlow(newProjectConnection(project, ConnectionName));
const directory = await mkdtemp(join(tmpdir(), 'dex-drive-copy-client-'));
const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
const client = new Client(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
try {
  const flowId = `drive-copy-${randomUUID()}`;
  await client.startFlow(flow, flowId, input);
  const result = await client.waitForFlow(flowId, 90_000);
  if (result.status !== 'completed') throw new Error('Drive text copy did not complete');
  process.stdout.write(`${JSON.stringify(result.singleOutput())}\n`);
} finally { await client.close(); cache.close(); await rm(directory, { recursive: true, force: true }); }
