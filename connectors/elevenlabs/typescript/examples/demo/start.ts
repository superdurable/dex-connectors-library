// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { ListVoicesInputSchema, newProjectConnection, type ListVoicesOutput } from '@superdurable/connector-elevenlabs';
import { ConnectionName, ElevenLabsDemoFlow } from './flow.js';

/** Start one voice-list Flow using project-managed credentials. */
export async function startVoiceList(inputFile: string): Promise<ListVoicesOutput> {
  const input = ListVoicesInputSchema.parse(JSON.parse(await readFile(inputFile, 'utf8')));
  const project = await loadFromEnvironment();
  const flow = new ElevenLabsDemoFlow(newProjectConnection(project, ConnectionName));
  const directory = await mkdtemp(join(tmpdir(), 'dex-elevenlabs-client-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
  try {
    const flowId = `elevenlabs-voices-${randomUUID()}`;
    await client.startFlow(flow, flowId, input);
    const result = await client.waitForFlow(flowId, 90_000);
    if (result.status !== 'completed') throw new Error('ElevenLabs voice Flow did not complete');
    return result.singleOutput<ListVoicesOutput>();
  } finally {
    await client.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}

if (process.argv[1]?.endsWith('/start.ts')) {
  const inputFile = process.argv[2];
  if (!inputFile) throw new TypeError('voice input JSON file is required');
  startVoiceList(inputFile).then(output => process.stdout.write(`${JSON.stringify(output)}\n`)).catch(() => {
    console.error('ElevenLabs voice Flow could not start or complete.'); process.exitCode = 1;
  });
}
