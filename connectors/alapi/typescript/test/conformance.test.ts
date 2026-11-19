// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { ALAPIClient } from '../src/client.js';
import { LookupIPLocationInputSchema } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/alapi/scenarios.json', import.meta.url));
const connection = { provider: 'alapi', name: 'alapi' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeALAPI));

async function invokeALAPI(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'lookupIPLocation') throw new Error(`unknown ALAPI operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const config = scenario.name === 'lookupIPLocation-oversized' ? { maxResponseBytes: 16 } : {};
    const client = new ALAPIClient(config, new StaticCredentialProvider([[connection, { token: new SecretString('test-token') }]]), {
      send: request => {
        if (scenario.exchanges[0]?.transportFailure) {
          recorder.record({ method: request.method, url: request.url, headers: request.headers ?? {}, body: request.body });
          throw new Error('synthetic provider transport failure');
        }
        return sendProviderRequest({ ...request, url: `${baseUrl}/api/ip`, addressPolicy: 'privateNetworkHTTP' });
      },
    });
    const call: Call = {
      context: { flowId: 'alapi-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'alapi', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.lookupIPLocation().invoke(call, LookupIPLocationInputSchema.parse(scenario.input));
  });
}
