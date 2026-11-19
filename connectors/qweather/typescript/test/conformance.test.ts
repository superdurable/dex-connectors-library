// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { QWeatherClient } from '../src/client.js';
import { GetCurrentWeatherInputSchema, type Credentials } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/qweather/scenarios.json', import.meta.url));
const connection = { provider: 'qweather', name: 'qweather' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeQWeather));

async function invokeQWeather(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getCurrentWeather') throw new Error(`unknown QWeather operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const credentials: Credentials = { apiHost: new SecretString('abc123.xy.qweatherapi.com'), apiKey: new SecretString(scenario.name === 'getCurrentWeather-defect' ? '' : 'private-test-key') };
    const client = new QWeatherClient({ ...(scenario.name === 'getCurrentWeather-oversized' ? { maxResponseBytes: 16 } : {}) }, new StaticCredentialProvider<Credentials>([[connection, credentials]]), {
      send: request => {
        if (scenario.exchanges[0]?.transportFailure) {
          recorder.record({ method: request.method, url: request.url, headers: request.headers ?? {}, body: request.body });
          throw new Error('synthetic provider transport failure');
        }
        return sendProviderRequest({ ...request, url: `${baseUrl}${new URL(request.url).pathname}`, addressPolicy: 'privateNetworkHTTP' });
      },
    });
    const call: Call = {
      context: { flowId: 'qweather-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'qweather', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.getCurrentWeather().invoke(call, GetCurrentWeatherInputSchema.parse(scenario.input));
  });
}
