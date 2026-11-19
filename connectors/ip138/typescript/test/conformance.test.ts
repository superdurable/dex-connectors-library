// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { IP138Client } from '../src/client.js';
import { GetTokenUsageInputSchema, type Credentials } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/ip138/scenarios.json', import.meta.url));
const connection = { provider: 'ip138', name: 'ip138' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeIP138));

async function invokeIP138(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getTokenUsage') throw new Error(`unknown IP138 operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const credentials: Credentials = { token: new SecretString(scenario.name === 'getTokenUsage-defect' ? '' : 'test-secret') };
    const client = new IP138Client({ ...(scenario.name === 'getTokenUsage-oversized' ? { maxResponseBytes: 16 } : {}) }, new StaticCredentialProvider<Credentials>([[connection, credentials]]), {
      send: request => {
        if (scenario.exchanges[0]?.transportFailure) {
          recorder.record({ method: request.method, url: request.url, headers: request.headers ?? {}, body: request.body });
          throw new Error('synthetic provider transport failure');
        }
        return sendProviderRequest({ ...request, url: `${baseUrl}/status/${new URL(request.url).search}`, addressPolicy: 'privateNetworkHTTP' });
      },
    });
    const call: Call = {
      context: { flowId: 'ip138-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'ip138', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.getTokenUsage().invoke(call, GetTokenUsageInputSchema.parse(scenario.input));
  });
}
