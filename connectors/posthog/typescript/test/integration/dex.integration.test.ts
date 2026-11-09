// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { Mock } from '../../src/mock.js';
import type { CapturedEvent, InsightsPage } from '../../src/index.js';
import { ConnectionName, EventCaptureFlow } from '../../examples/provider-operations/event-capture-flow.js';
import { InsightListFlow } from '../../examples/provider-operations/insight-list-flow.js';

it('persists both manifest mocks through real Dex Workers and Clients', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const directory = await mkdtemp(join(tmpdir(), 'dex-posthog-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const mock = new Mock(ConnectionName);
  const eventFlow = new EventCaptureFlow(mock.connection());
  const insightFlow = new InsightListFlow(mock.connection());
  const registry = new Registry([eventFlow, insightFlow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const eventFlowId = `posthog-typescript-event-${randomUUID()}`;
    await client.startFlow(eventFlow, eventFlowId, {
      event: 'Signed Up', distinctId: 'member-1', timestamp: '2026-10-29T00:00:00Z',
    });
    const event = await client.waitForFlow(eventFlowId, 60_000);
    expect(event.status, event.errorMessage).toBe('completed');
    expect(event.singleOutput<CapturedEvent>()).toMatchObject({ accepted: true });
    expect(mock.captureEvent().calls()).toHaveLength(1);

    const insightFlowId = `posthog-typescript-insights-${randomUUID()}`;
    await client.startFlow(insightFlow, insightFlowId, { projectId: 42, offset: 0, limit: 20 });
    const insights = await client.waitForFlow(insightFlowId, 60_000);
    expect(insights.status, insights.errorMessage).toBe('completed');
    expect(insights.singleOutput<InsightsPage>()).toMatchObject({ insights: [{ id: 42 }] });
    expect(mock.listInsights().calls()).toHaveLength(1);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
