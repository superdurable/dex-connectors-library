// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache, type BlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { WeatherInputSchema, newProjectConnection, type CurrentWeatherOutput } from '@superdurable/connector-openweathermap';
import { ConnectionName, WeatherFlow } from './flow/workflow.js';

async function startWeather(): Promise<void> {
  const inputFile = process.argv[2];
  if (!inputFile) throw new TypeError('OpenWeatherMap weather input JSON file is required');
  const input = WeatherInputSchema.parse(JSON.parse(await readFile(inputFile, 'utf8')));
  const project = await loadFromEnvironment();
  const flow = new WeatherFlow(newProjectConnection(project, ConnectionName));
  const directory = await mkdtemp(join(tmpdir(), 'dex-openweathermap-weather-client-'));
  let cache: BlobCache | undefined;
  let client: Client | undefined;
  try {
    cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
    client = new Client(new Registry([flow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
    const flowId = `openweathermap-weather-${randomUUID()}`;
    await client.startFlow(flow, flowId, input);
    const result = await client.waitForFlow(flowId, 90_000);
    if (result.status !== 'completed') throw new Error('OpenWeatherMap weather Flow did not complete');
    process.stdout.write(`${JSON.stringify(result.singleOutput<CurrentWeatherOutput>())}\n`);
  } finally {
    try { await client?.close(); } finally {
      cache?.close();
      await rm(directory, { recursive: true, force: true });
    }
  }
}

try { await startWeather(); } catch {
  console.error('OpenWeatherMap weather Flow could not start or complete.');
  process.exitCode = 1;
}
