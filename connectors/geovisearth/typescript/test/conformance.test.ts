// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { GeovisearthClient } from '../src/client.js';
import { GetCurrentWeatherInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/geovisearth/scenarios.json', import.meta.url));
const connection = { provider: 'geovisearth', name: 'geovisearth' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeGeovisearth, ''));

async function invokeGeovisearth(scenario: Scenario, recorder: ExchangeRecorder) {
  const token = scenario.name.endsWith('missing-credentials') ? '' : 'private-test-token';
  const credentials: Array<[typeof connection, { token: SecretString }]> = [[connection, { token: new SecretString(token) }]];
  const client = new GeovisearthClient({ maxResponseBytes: scenario.name.endsWith('oversize') ? 8 : 1_048_576 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'geovisearth-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'geovisearth', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'getCurrentWeather') return client.getCurrentWeather().invoke(call, GetCurrentWeatherInputSchema.parse(scenario.input));
  throw new Error('unknown Geovisearth operation ' + scenario.operation);
}
