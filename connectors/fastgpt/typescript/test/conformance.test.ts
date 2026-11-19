// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { FastGPTClient } from '../src/client.js';
import { GetDatasetDetailsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/fastgpt/scenarios.json', import.meta.url));
const connection = { provider: 'fastgpt', name: 'fastgpt' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeFastGPT, '/fastgpt'));

async function invokeFastGPT(scenario: Scenario, recorder: ExchangeRecorder) {
  const apiKey = scenario.name.endsWith('missing-key') ? '' : 'private-test-key';
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString(apiKey) }]];
  const client = new FastGPTClient({ baseUrl: 'http://127.0.0.1:12345/fastgpt', maxResponseBytes: scenario.name.endsWith('oversize') ? 8 : 2097152 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'fastgpt-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'fastgpt', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getDatasetDetails') return client.getDatasetDetails().invoke(call, GetDatasetDetailsInputSchema.parse(scenario.input));
  throw new Error(`unknown FastGPT operation ${scenario.operation}`);
}
