// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { GowuClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, SyntheticRandomSource, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/gowu/scenarios.json', import.meta.url));
const connection = { provider: 'gowu', name: 'gowu' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeGowu, ''));
async function invokeGowu(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getAccessClient') throw new Error(`unknown Gowu operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { appKey: scenario.name.endsWith('-defect') ? '' : 'REDACTED_GOWU_APP_KEY', appSecret: new SecretString('REDACTED_GOWU_SECRET') }]]);
  const client = new GowuClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)), now: () => new Date(scenario.fixedClock ?? '2026-11-12T10:00:00Z'), nonce: () => new SyntheticRandomSource(scenario.randomSeed ?? 'SYNTHETIC_CONFORMANCE_GOWU_NONCE').read(16) });
  const call: Call = { context: { flowId: 'gowu-conformance', cancellationSignal: new AbortController().signal } as Call['context'], id: scenario.callId, connection, operation: { connectorId: 'gowu', operationId: scenario.operation }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
  return client.getAccessClient().invoke(call, scenario.input as unknown as Parameters<ReturnType<GowuClient['getAccessClient']>['invoke']>[1]);
}
