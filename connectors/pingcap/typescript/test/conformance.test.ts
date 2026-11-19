// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { PingcapClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, SyntheticRandomSource, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
const scenarios = loadScenarios(new URL('../../../../conformance/connectors/pingcap/scenarios.json', import.meta.url));
const connection = { provider: 'pingcap', name: 'pingcap' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokePingcap, ''));
async function invokePingcap(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listProjects') throw new Error(`unknown operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { publicKey: new SecretString('test-public-key'), privateKey: new SecretString('test-private-key') }]]);
  const randomSource = new SyntheticRandomSource(scenario.randomSeed ?? 'SYNTHETIC_CONFORMANCE_PINGCAP_fallback');
  const client = new PingcapClient(scenario.name.endsWith('-oversized') ? { maxResponseBytes: 16 } : {}, credentials, { nonce: () => randomSource.read(16), send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = { context: { flowId: 'pingcap-conformance', cancellationSignal: new AbortController().signal } as Call['context'], id: scenario.callId, connection, operation: { connectorId: 'pingcap', operationId: scenario.operation }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
  return client.listProjects().invoke(call, scenario.input as unknown as Parameters<ReturnType<PingcapClient['listProjects']>['invoke']>[1]);
}
