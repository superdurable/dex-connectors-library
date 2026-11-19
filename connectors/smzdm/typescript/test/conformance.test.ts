// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { SMZDMClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
const scenarios = loadScenarios(new URL('../../../../conformance/connectors/smzdm/scenarios.json', import.meta.url));
const connection = { provider: 'smzdm', name: 'smzdm' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSMZDM, ''));
async function invokeSMZDM(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'searchProducts') throw new Error(`unknown operation ${scenario.operation}`);
  const invalid = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { appKey: new SecretString(invalid ? '' : '123456789'), appSecret: new SecretString(invalid ? '' : '88888888') }]]);
  const client = new SMZDMClient({}, credentials, { now: () => new Date(1731510000 * 1000), send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = { context: { flowId: 'smzdm-conformance', cancellationSignal: new AbortController().signal } as Call['context'], id: scenario.callId, connection, operation: { connectorId: 'smzdm', operationId: scenario.operation }, reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };
  return client.searchProducts().invoke(call, scenario.input as unknown as Parameters<ReturnType<SMZDMClient['searchProducts']>['invoke']>[1]);
}
