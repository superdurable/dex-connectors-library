// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { SuxinClient } from '../src/client.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/suxin/scenarios.json', import.meta.url));
const connection = { provider: 'suxin', name: 'suxin' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSuxin, ''));

async function invokeSuxin(scenario: Scenario, recorder: ExchangeRecorder) {
  const apiKey = scenario.name.endsWith('defect') ? '' : 'sk_test_123';
  const credentials: Array<[typeof connection, { apiKey: SecretString; appId: SecretString }]> = [[connection, { apiKey: new SecretString(apiKey), appId: new SecretString('app_test_123') }]];
  const client = new SuxinClient({ maxResponseBytes: scenario.name.endsWith('oversized') ? 8 : 1048576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'suxin-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'suxin', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listOrganizations') return client.listOrganizations().invoke(call);
  throw new Error(`unknown Suxin operation ${scenario.operation}`);
}
