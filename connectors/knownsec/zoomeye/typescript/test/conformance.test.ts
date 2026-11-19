// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ZoomEyeClient } from '../src/client.js';
import { GetAccountQuotaInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/knownsec/zoomeye/scenarios.json', import.meta.url));
const connection = { provider: 'zoomeye', name: 'zoomeye' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeZoomEye, ''));

async function invokeZoomEye(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString(scenario.name.endsWith('-defect') ? '' : 'test-zoomeye-api-key') }]];
  const client = new ZoomEyeClient({ baseUrl: 'http://127.0.0.1:12345', maxResponseBytes: 1_048_576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'zoomeye-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'zoomeye', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getAccountQuota') return client.getAccountQuota().invoke(call, GetAccountQuotaInputSchema.parse(scenario.input));
  throw new Error('unknown ZoomEye operation ' + scenario.operation);
}
