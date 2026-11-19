// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ShowAPIClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/showapi/scenarios.json', import.meta.url));
const connection = { provider: 'showapi', name: 'showapi' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeShowAPI, ''));

async function invokeShowAPI(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'lookupAreaCodes') throw new Error(`unknown ShowAPI operation ${scenario.operation}`);
  const credentials = new StaticCredentialProvider([[connection, { appKey: new SecretString('secretKey') }]]);
  const client = new ShowAPIClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'showapi-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'showapi', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.lookupAreaCodes().invoke(call, scenario.input as unknown as Parameters<ReturnType<ShowAPIClient['lookupAreaCodes']>['invoke']>[1]);
}
