// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { OpenWeatherMapClient, type WeatherInput } from '../src/index.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/openweathermap/scenarios.json', import.meta.url));
const connection = { provider: 'openweathermap', name: 'openweathermap' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWeather));

async function invokeWeather(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new OpenWeatherMapClient({ baseUrl: 'http://127.0.0.1:12345' }, {
    resolve: async () => ({ apiKey: new SecretString('test-key-123') }),
  }, { send: sender });
  const call = scenarioCall(scenario, connection, 'openweathermap', { flowId: 'openweathermap-conformance' }, {
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  }) as Call;
  if (scenario.operation === 'getCurrentWeather') return client.getCurrentWeather().invoke(call, scenario.input as unknown as WeatherInput);
  if (scenario.operation === 'getForecast') return client.getForecast().invoke(call, scenario.input as unknown as WeatherInput);
  throw new Error(`unknown operation ${scenario.operation}`);
}
