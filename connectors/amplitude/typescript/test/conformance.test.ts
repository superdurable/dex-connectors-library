// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { AmplitudeClient } from '../src/client.js';
import { SearchUsersInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/amplitude/scenarios.json', import.meta.url));
const connection = { provider: 'amplitude', name: 'amplitude' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeAmplitude, ''));

async function invokeAmplitude(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiKey: SecretString; secretKey: SecretString }]> = [[connection, { apiKey: new SecretString('sample-amplitude-api-key'), secretKey: new SecretString('sample-amplitude-secret-key') }]];
  const client = new AmplitudeClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'amplitude-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'amplitude', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'searchUsers') return client.searchUsers().invoke(call, SearchUsersInputSchema.parse(scenario.input));
  throw new Error(`unknown Amplitude operation ${scenario.operation}`);
}
