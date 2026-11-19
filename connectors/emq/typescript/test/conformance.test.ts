// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { EmqClient } from '../src/client.js';
import { ListDeploymentsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/emq/scenarios.json', import.meta.url));
const connection = { provider: 'emq', name: 'emq' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeEmq, ''));

async function invokeEmq(scenario: Scenario, recorder: ExchangeRecorder) {
  const apiKey = scenario.name.endsWith('missing-key') ? '' : 'test-platform-key';
  const credentials: Array<[typeof connection, { platformApiKey: SecretString; platformApiSecret: SecretString }]> = [[connection, { platformApiKey: new SecretString(apiKey), platformApiSecret: new SecretString('test-platform-secret') }]];
  const client = new EmqClient({ platformEndpoint: 'https://cloud.emqx.com/public_api/v1', maxResponseBytes: scenario.name.endsWith('oversize') ? 8 : 1048576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'emq-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'emq', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listDeployments') return client.listDeployments().invoke(call, ListDeploymentsInputSchema.parse(scenario.input));
  throw new Error(`unknown Emq operation ${scenario.operation}`);
}
