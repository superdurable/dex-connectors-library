// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { BaiduMapsClient } from '../src/client.js';
import type { GeocodeAddressInput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/baidu/maps/scenarios.json', import.meta.url));
const connection = { provider: 'baidu-maps', name: 'address-lookup' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeBaiduMaps));

async function invokeBaiduMaps(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const credentials = new StaticCredentialProvider([[connection, { accessKey: new SecretString('test-baidu-server-ak-123') }]]);
  const client = new BaiduMapsClient({ baseUrl: 'http://127.0.0.1' }, credentials, { send });
  const call = scenarioCall(scenario, connection, 'baidu-maps', {
    flowId: 'baidu-maps-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation !== 'geocodeAddress') throw new Error(`unknown Baidu Maps operation ${scenario.operation}`);
  return client.geocodeAddress().invoke(call, scenario.input as unknown as GeocodeAddressInput);
}
