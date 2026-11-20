// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { QichachaClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/qichacha/scenarios.json', import.meta.url));
const connection = { provider: 'qichacha', name: 'qichacha' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeQichacha, ''));
async function invokeQichacha(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getEnterpriseBasicDetails') throw new Error(`unknown Qichacha operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { appKey: new SecretString('REDACTED_QCC_APP_KEY'), secretKey: new SecretString('REDACTED_QCC_SECRET') }]]);
  const client = new QichachaClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)), now: () => new Date(scenario.fixedClock ?? '2026-11-12T10:00:00Z') });
  const call: Call = { context: { flowId: 'qichacha-conformance', cancellationSignal: new AbortController().signal } as Call['context'], id: scenario.callId, connection, operation: { connectorId: 'qichacha', operationId: scenario.operation }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
  return client.getEnterpriseBasicDetails().invoke(call, scenario.input as unknown as Parameters<ReturnType<QichachaClient['getEnterpriseBasicDetails']>['invoke']>[1]);
}
