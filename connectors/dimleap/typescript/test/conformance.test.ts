// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { DimleapClient } from '../src/client.js';
import { ListModelsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/dimleap/scenarios.json', import.meta.url));
const connection = { provider: 'dimleap', name: 'dimleap' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeDimleap, ''));

async function invokeDimleap(scenario: Scenario, recorder: ExchangeRecorder) {
  const apiKey = scenario.name.endsWith('missing-credentials') ? '' : 'sk-test-secret';
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString(apiKey) }]];
  const client = new DimleapClient({ maxResponseBytes: scenario.name.endsWith('oversize') ? 8 : 1_048_576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'dimleap-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'dimleap', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listModels') return client.listModels().invoke(call, ListModelsInputSchema.parse(scenario.input));
  throw new Error('unknown Dimleap operation ' + scenario.operation);
}
