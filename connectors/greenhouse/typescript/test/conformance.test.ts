// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { GreenhouseClient } from '../src/client.js';
import { GetCandidateInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/greenhouse/scenarios.json', import.meta.url));
const connection = { provider: 'greenhouse', name: 'greenhouse' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeGreenhouse, '/v3'));

async function invokeGreenhouse(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { clientId: string; clientSecret: SecretString; accessToken: SecretString }]> = [[connection, { clientId: 'sample-client-id', clientSecret: new SecretString('sample-client-secret'), accessToken: new SecretString('sample-access-token') }]];
  const client = new GreenhouseClient({ baseUrl: 'http://127.0.0.1:12345/v3' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'greenhouse-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'greenhouse', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getCandidate') return client.getCandidate().invoke(call, GetCandidateInputSchema.parse(scenario.input));
  throw new Error(`unknown Greenhouse operation ${scenario.operation}`);
}
