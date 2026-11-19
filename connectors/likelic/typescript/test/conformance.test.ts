// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { LikelicClient } from '../src/client.js';
import { ListRoomsInputSchema, type Credentials } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/likelic/scenarios.json', import.meta.url));
const connection = { provider: 'likelic', name: 'likelic' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeLikelic));

async function invokeLikelic(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listRooms') throw new Error(`unknown Likelic operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const credentials: Credentials = { apiKey: new SecretString('sk-0123456789abcdef0123456789abcdef0123456789abcdef'), customerCode: 'demo_company' };
    const client = new LikelicClient({}, new StaticCredentialProvider([[connection, credentials]]), {
      send: request => {
        if (scenario.exchanges[0]?.transportFailure) {
          recorder.record({ method: request.method, url: request.url, headers: request.headers ?? {}, body: request.body });
          throw new Error('synthetic provider transport failure');
        }
        return sendProviderRequest({ ...request, url: `${baseUrl}${new URL(request.url).pathname}${new URL(request.url).search}`, addressPolicy: 'privateNetworkHTTP' });
      },
    });
    const call: Call = {
      context: { flowId: 'likelic-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'likelic', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.listRooms().invoke(call, ListRoomsInputSchema.parse(scenario.input));
  });
}
