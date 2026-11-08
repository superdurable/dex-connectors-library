// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { BrazeClient } from '../src/client.js';
import { GetUserProfileInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/braze/scenarios.json', import.meta.url));
const connection = { provider: 'braze', name: 'braze' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeBraze));

async function invokeBraze(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { restApiKey: SecretString }]> = [[connection, { restApiKey: new SecretString('sample-rest-key') }]];
  const client = new BrazeClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'braze-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'braze', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getUserProfile') return client.getUserProfile().invoke(call, GetUserProfileInputSchema.parse(scenario.input));
  throw new Error(`unknown Braze operation ${scenario.operation}`);
}
