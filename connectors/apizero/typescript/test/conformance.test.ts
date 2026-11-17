// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { ApiZeroClient } from '../src/client.js';
import { GetAccountInfoInputSchema, type Credentials } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/apizero/scenarios.json', import.meta.url));
const connection = { provider: 'apizero', name: 'apizero' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeApiZero));

async function invokeApiZero(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getAccountInfo') throw new Error(`unknown ApiZero operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const credentials: Credentials = { apiKey: new SecretString(scenario.name === 'getAccountInfo-defect' ? 'invalid' : 'sk_live_test-secret') };
    const client = new ApiZeroClient({ ...(scenario.name === 'getAccountInfo-oversized' ? { maxResponseBytes: 16 } : {}) }, new StaticCredentialProvider<Credentials>([[connection, credentials]]), {
      send: request => {
        if (scenario.exchanges[0]?.transportFailure) {
          recorder.record({ method: request.method, url: request.url, headers: request.headers ?? {}, body: request.body });
          throw new Error('synthetic pre-dispatch transport failure');
        }
        return sendProviderRequest({ ...request, url: `${baseUrl}/api/user-info`, addressPolicy: 'privateNetworkHTTP' });
      },
    });
    const call: Call = {
      context: { flowId: 'apizero-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'apizero', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.getAccountInfo().invoke(call, GetAccountInfoInputSchema.parse(scenario.input));
  });
}
