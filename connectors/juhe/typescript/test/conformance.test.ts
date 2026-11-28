// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { JuheClient } from '../src/client.js';
import { GetCurrentWeatherInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/juhe/scenarios.json', import.meta.url));
const connection = { provider: 'juhe', name: 'juhe' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeJuhe, ''));

async function invokeJuhe(scenario: Scenario, recorder: ExchangeRecorder) {
  const smallLimit = scenario.name.endsWith('-smallLimit') || scenario.name.endsWith('-oversized');
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString(scenario.name === 'getCurrentWeather-missingCredential' ? '' : 'test-juhe-api-key') }]];
  const client = new JuheClient({ baseUrl: 'http://127.0.0.1:12345', maxResponseBytes: smallLimit ? 16 : 524_288 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'juhe-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'juhe', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getCurrentWeather') return client.getCurrentWeather().invoke(call, GetCurrentWeatherInputSchema.parse(scenario.input));
  throw new Error('unknown Juhe operation ' + scenario.operation);
}
