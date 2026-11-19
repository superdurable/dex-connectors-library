// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { RainbondClient } from '../src/client.js';
import { ListTeamsInputSchema, type Credentials } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/rainbond/scenarios.json', import.meta.url));
const connection = { provider: 'rainbond', name: 'rainbond' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeRainbond));

async function invokeRainbond(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listTeams') throw new Error(`unknown Rainbond operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const credentials: Credentials = { apiKey: new SecretString(scenario.name === 'listTeams-defect' ? '' : 'private-test-key') };
    const client = new RainbondClient({ baseUrl: "https://console.example.test", ...(scenario.name === 'listTeams-oversized' ? { maxResponseBytes: 16 } : {}) }, new StaticCredentialProvider<Credentials>([[connection, credentials]]), {
      send: request => {
        if (scenario.exchanges[0]?.transportFailure) {
          recorder.record({ method: request.method, url: request.url, headers: request.headers ?? {}, body: request.body });
          throw new Error('synthetic provider transport failure');
        }
        return sendProviderRequest({ ...request, url: `${baseUrl}/openapi/v1/teams${new URL(request.url).search}`, addressPolicy: 'privateNetworkHTTP' });
      },
    });
    const call: Call = {
      context: { flowId: 'rainbond-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'rainbond', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.listTeams().invoke(call, ListTeamsInputSchema.parse(scenario.input));
  });
}
