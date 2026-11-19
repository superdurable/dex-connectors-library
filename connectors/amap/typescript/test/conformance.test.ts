// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { AmapClient } from '../src/client.js';
import { GeocodeAddressInputSchema } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/amap/scenarios.json', import.meta.url));
const connection = { provider: 'amap', name: 'amap' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeAmap));

async function invokeAmap(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'geocodeAddress') throw new Error(`unknown Amap operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const client = new AmapClient({}, new StaticCredentialProvider([[connection, { apiKey: new SecretString('test-amap-key-123') }]]), {
      send: request => sendProviderRequest({
        ...request, url: `${baseUrl}${new URL(request.url).pathname}${new URL(request.url).search}`, addressPolicy: 'privateNetworkHTTP',
      }),
    });
    const call: Call = {
      context: { flowId: 'amap-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'amap', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.geocodeAddress().invoke(call, GeocodeAddressInputSchema.parse(scenario.input));
  });
}
