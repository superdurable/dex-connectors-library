// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { QuanshiClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, SyntheticRandomSource, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/quanshi/scenarios.json', import.meta.url));
const connection = { provider: 'quanshi', name: 'quanshi' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeQuanshi, ''));
async function invokeQuanshi(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listRooms') throw new Error(`unknown Quanshi operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { appId: new SecretString(scenario.name.endsWith('-defect') ? '' : 'REDACTED_APP_ID'), appSecret: new SecretString('REDACTED_APP_SECRET') }]]);
  const client = new QuanshiClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)), now: () => new Date(scenario.fixedClock ?? '2026-11-19T12:00:00Z'), nonce: () => new SyntheticRandomSource(scenario.randomSeed ?? 'SYNTHETIC_CONFORMANCE_QUANSHI').read(16) });
  const call: Call = { context: { flowId: 'quanshi-conformance', cancellationSignal: new AbortController().signal } as Call['context'], id: scenario.callId, connection, operation: { connectorId: 'quanshi', operationId: scenario.operation }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
  return client.listRooms().invoke(call, scenario.input as unknown as Parameters<ReturnType<QuanshiClient['listRooms']>['invoke']>[1]);
}
