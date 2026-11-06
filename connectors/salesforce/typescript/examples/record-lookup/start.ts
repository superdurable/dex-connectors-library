// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { Client, Registry, openBlobCache } from '@superdurable/dex';
import { loadFromEnvironment } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '@superdurable/connector-salesforce';
import { ConnectionName, SalesforceRecordLookupFlow } from './flow.js';

/** Start one Salesforce lookup and return its terminal Dex status. */
export async function startSalesforceLookup(recordId: string): Promise<string> {
  if (!recordId.trim()) throw new TypeError('Salesforce record ID is required');
  const project = await loadFromEnvironment();
  const flow = new SalesforceRecordLookupFlow(newProjectConnection(project, ConnectionName));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'salesforce-example-client-cache-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const client = new Client(registry, cache, {
    serverAddress: process.env.DEX_FLOW_SERVICE_ADDRESS ?? '127.0.0.1:8801',
  });
  try {
    const flowId = `salesforce-example-${randomUUID()}`;
    await client.startFlow(flow, flowId, { sObjectType: 'Contact', recordId, fields: ['Id', 'Email'] });
    const result = await client.waitForFlow(flowId, 30_000);
    return result.status;
  } finally {
    await client.close();
    cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  startSalesforceLookup(process.argv[2] ?? '').then(status => { process.stdout.write(`Salesforce Flow status: ${status}\n`); })
    .catch(() => { process.stderr.write('Salesforce example lookup failed\n'); process.exitCode = 1; });
}
