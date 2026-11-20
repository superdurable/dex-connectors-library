// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { MeiqiaClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/meiqia/scenarios.json', import.meta.url));
const connection = { provider: 'meiqia', name: 'meiqia' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeMeiqia, ''));

async function invokeMeiqia(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getConversation') throw new Error(`unknown Meiqia operation ${scenario.operation}`);
  const isDefect = scenario.name.endsWith('-defect');
  const credentials = new StaticCredentialProvider([[connection, { accessToken: new SecretString(isDefect ? '' : 'private-token') }]]);
  const client = new MeiqiaClient({}, credentials, { send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)) });
  const call: Call = {
    context: { flowId: 'meiqia-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'meiqia', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  return client.getConversation().invoke(call, scenario.input as unknown as Parameters<ReturnType<MeiqiaClient['getConversation']>['invoke']>[1]);
}
