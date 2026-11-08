// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { RipplingClient } from '../src/client.js';
import { GetWorkerInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/rippling/scenarios.json', import.meta.url));
const connection = { provider: 'rippling', name: 'rippling' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeRippling, '/api'));

async function invokeRippling(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiToken: SecretString }]> = [[connection, { apiToken: new SecretString('sample-rippling-api-token') }]];
  const client = new RipplingClient({ baseUrl: 'http://127.0.0.1:12345/api' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'rippling-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'rippling', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getWorker') return client.getWorker().invoke(call, GetWorkerInputSchema.parse(scenario.input));
  throw new Error(`unknown Rippling operation ${scenario.operation}`);
}
