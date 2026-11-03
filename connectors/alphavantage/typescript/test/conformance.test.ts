// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { Client, type StockInput } from '../src/index.js';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/alphavantage/scenarios.json', import.meta.url));
const connection = { provider: 'alphavantage', name: 'alphavantage' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeAlphaVantage));

async function invokeAlphaVantage(scenario: Scenario, recorder: ExchangeRecorder) {
  const client = new Client({ baseUrl: 'http://127.0.0.1:21101' },
    new StaticCredentialProvider([[connection, { apiKey: new SecretString('test-key-123') }]]),
    request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)));
  const call = scenarioCall(scenario, connection, 'alphavantage',
    { flowId: 'alphavantage-conformance', cancellationSignal: new AbortController().signal },
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'getGlobalQuote') return client.getGlobalQuote().invoke(call, scenario.input as unknown as StockInput);
  if (scenario.operation === 'getTimeSeriesDaily') return client.getTimeSeriesDaily().invoke(call, scenario.input as unknown as StockInput);
  throw new Error(`unknown operation ${scenario.operation}`);
}
