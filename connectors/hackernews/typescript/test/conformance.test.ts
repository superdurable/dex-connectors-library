// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { HackerNewsClient } from '../src/client.js';
import { GetItemInputSchema, ListStoryIDsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, scenarioClock, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/hackernews/scenarios.json', import.meta.url));
const connection = { provider: 'hacker-news', name: 'public' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeHackerNews));

async function invokeHackerNews(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.exchanges.some(exchange => exchange.transportFailure)) {
    return withScenarioHTTPServer(recorder, baseUrl => invokeAtBaseUrl(scenario, baseUrl,
      (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before))));
  }
  return withScenarioHTTPServer(recorder, baseUrl => invokeAtBaseUrl(scenario, baseUrl));
}

async function invokeAtBaseUrl(scenario: Scenario, baseUrl: string, send?: (request: ProviderRequest) => Promise<{ status: number; headers: Record<string, string>; body: Uint8Array }>) {
    const client = new HackerNewsClient({ baseUrl: `${baseUrl}/v0`, maxResponseBytes: 300 }, { now: scenarioClock(scenario), send });
    const call = scenarioCall(scenario, connection, 'hacker-news-daily', {
      flowId: 'hackernews-conformance', cancellationSignal: new AbortController().signal,
    }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
    if (scenario.operation === 'listStoryIDs') return client.listStoryIDs().invoke(call, ListStoryIDsInputSchema.parse(scenario.input));
    if (scenario.operation === 'getItem') return client.getItem().invoke(call, GetItemInputSchema.parse(scenario.input));
    throw new Error(`unknown operation ${scenario.operation}`);
}
