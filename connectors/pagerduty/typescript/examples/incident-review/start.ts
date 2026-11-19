// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { GetIncidentInputSchema, ListIncidentsInputSchema, newProjectConnection, type Incident, type IncidentPage } from '@superdurable/connector-pagerduty';
import { ConnectionName, IncidentLookupFlow, IncidentReviewFlow } from './flow/workflow.js';

async function startIncidentReview(): Promise<void> {
  const inputFile = process.argv[2];
  if (!inputFile) throw new TypeError('PagerDuty input JSON file is required');
  const source: unknown = JSON.parse(await readFile(inputFile, 'utf8'));
  const isLookup = typeof source === 'object' && source !== null && 'id' in source;
  const project = await loadFromEnvironment();
  const connection = newProjectConnection(project, ConnectionName);
  const reviewFlow = new IncidentReviewFlow(connection);
  const lookupFlow = new IncidentLookupFlow(connection);
  const directory = await mkdtemp(join(tmpdir(), 'dex-pagerduty-client-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(new Registry([reviewFlow, lookupFlow]), cache, { serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801' });
  try {
    const flowId = `pagerduty-review-${randomUUID()}`;
    if (isLookup) await client.startFlow(lookupFlow, flowId, GetIncidentInputSchema.parse(source));
    else await client.startFlow(reviewFlow, flowId, ListIncidentsInputSchema.parse(source));
    const result = await client.waitForFlow(flowId, 90_000);
    if (result.status !== 'completed') throw new Error('PagerDuty incident review did not complete');
    process.stdout.write(`${JSON.stringify(isLookup ? result.singleOutput<Incident>() : result.singleOutput<IncidentPage>())}\n`);
  } finally { await client.close(); cache.close(); await rm(directory, { recursive: true, force: true }); }
}
try { await startIncidentReview(); } catch { console.error('PagerDuty incident review could not complete.'); process.exitCode = 1; }
