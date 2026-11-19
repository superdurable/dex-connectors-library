// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { NeteaseClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, SyntheticRandomSource, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/netease/scenarios.json', import.meta.url));
const connection = { provider: 'netease', name: 'netease' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeNetease, ''));

async function invokeNetease(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getUserProfile') throw new Error(`unknown operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { appKey: new SecretString("test-app-key"), appSecret: new SecretString("test-app-secret") }]]);
  const client = new NeteaseClient(scenario.name.endsWith('-oversized') ? { maxResponseBytes: 4 } : {}, credentials, { now: () => new Date(scenario.fixedClock ?? "2026-01-01T00:00:00Z"), nonce: () => new SyntheticRandomSource(scenario.randomSeed ?? "SYNTHETIC_CONFORMANCE_NETEASE_fallback").read(16), send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'netease-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'netease', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getUserProfile().invoke(call, scenario.input as unknown as Parameters<ReturnType<NeteaseClient['getUserProfile']>['invoke']>[1]);
}
