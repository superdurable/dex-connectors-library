// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../../src/index.js';
import { CaiyunCurrentWeatherFlow, ConnectionName } from './flow.js';

const project = await loadFromEnvironment();
const flow = new CaiyunCurrentWeatherFlow(newProjectConnection(project, ConnectionName));
const blobCache = openBlobCache({ directory: process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-caiyun-current-weather-blobs'), maxBytes: 1 << 30 });
const client = new Client(new Registry([flow]), blobCache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
try {
  const flowId = `caiyun-current-weather-${randomUUID()}`;
  const longitude = Number(process.argv[2]);
  const latitude = Number(process.argv[3]);
  if (!Number.isFinite(longitude) || !Number.isFinite(latitude)) throw new TypeError('Pass longitude and latitude as numeric arguments');
  await client.startFlow(flow, flowId, { longitude, latitude });
  const result = await client.waitForFlow(flowId, 60_000);
  console.log(JSON.stringify({ flowId, status: result.status, output: result.status === 'completed' ? result.singleOutput() : undefined }));
} finally { await client.close(); blobCache.close(); }
