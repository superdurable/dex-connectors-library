// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { CloudflareClient } from '../src/client.js';
import { ListZonesInputSchema, PurgeCacheByURLsInputSchema, type PurgeCacheByURLsInput, type CachePurge } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/cloudflare/scenarios.json', import.meta.url));
const connection = { provider: 'cloudflare', name: 'cloudflare' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeCloudflare));

async function invokeCloudflare(scenario: Scenario, recorder: ExchangeRecorder) {
  const client = new CloudflareClient({ baseUrl: 'http://127.0.0.1:12345' }, {
    resolve: async () => ({ apiToken: new SecretString(scenario.name === 'listZones-defect' ? '' : 'test-apitoken') }),
  }, { send: (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call = scenarioCall(scenario, connection, 'cloudflare', {
    flowId: 'provider-test', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'listZones') return client.listZones().invoke(call, ListZonesInputSchema.parse(scenario.input));
  if (scenario.operation === 'purgeCacheByURLs') {
    const operation: Mutation<PurgeCacheByURLsInput, CachePurge> = client.purgeCacheByURLs();
    return invokeProductionMutation(operation, scenario.idempotencyKey!,
      recording => runMutation(recording as Mutation<PurgeCacheByURLsInput, CachePurge>, call, PurgeCacheByURLsInputSchema.parse(scenario.input)),
      error => error instanceof RetryError);
  }
  throw new Error(`unknown Cloudflare operation ${scenario.operation}`);
}
