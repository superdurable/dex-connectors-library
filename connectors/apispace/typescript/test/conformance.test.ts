// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { APISpaceClient } from '../src/client.js';
import { GetCurrentWeatherInputSchema, type Credentials } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/apispace/scenarios.json', import.meta.url));
const connection = { provider: 'apispace', name: 'apispace' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeAPISpace));

async function invokeAPISpace(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getCurrentWeather') throw new Error(`unknown APISpace operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const credentials: Credentials = { apiToken: new SecretString('test-token') };
    const client = new APISpaceClient({ baseUrl, ...(scenario.name === 'getCurrentWeather-oversized' ? { maxResponseBytes: 16 } : {}) }, new StaticCredentialProvider<Credentials>([[connection, credentials]]), {
      send: request => {
        if (scenario.exchanges[0]?.transportFailure) {
          recorder.record({ method: request.method, url: request.url, headers: request.headers ?? {}, body: request.body });
          throw new Error('synthetic provider transport failure');
        }
        return sendProviderRequest(request);
      },
    });
    const call: Call = {
      context: { flowId: 'apispace-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'apispace', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.getCurrentWeather().invoke(call, GetCurrentWeatherInputSchema.parse(scenario.input));
  });
}
