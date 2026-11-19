// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { PolyvClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/polyv/scenarios.json', import.meta.url));
const connection = { provider: 'polyv', name: 'polyv' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokePolyv, ''));

async function invokePolyv(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listChannels') throw new Error(`unknown operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, scenario.name.endsWith("-defect") ? { appId: "", appSecret: new SecretString("") } : { appId: "testappid", appSecret: new SecretString("test-polyv-secret") }]]);
  const client = new PolyvClient(scenario.name.endsWith('-oversized') ? { maxResponseBytes: 8 } : {}, credentials, { now: () => new Date(scenario.fixedClock ?? "2026-01-01T00:00:00Z"), send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'polyv-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'polyv', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.listChannels().invoke(call, scenario.input as unknown as Parameters<ReturnType<PolyvClient['listChannels']>['invoke']>[1]);
}
