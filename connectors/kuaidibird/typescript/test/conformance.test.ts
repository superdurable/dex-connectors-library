// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { KuaidibirdClient } from '../src/client.js';
import { QueryTrackingInputSchema, type Credentials } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/kuaidibird/scenarios.json', import.meta.url));
const connection = { provider: 'kuaidibird', name: 'kuaidibird' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeKuaidibird));

async function invokeKuaidibird(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'queryTracking') throw new Error(`unknown Kuaidibird operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const credentials: Credentials = { apiKey: new SecretString('test-api-key'), businessId: new SecretString('1237100') };
    const client = new KuaidibirdClient({ baseUrl }, new StaticCredentialProvider([[connection, credentials]]), {
      send: request => {
        if (scenario.exchanges[0]?.transportFailure) {
          recorder.record({ method: request.method, url: request.url, headers: request.headers ?? {}, body: request.body });
          throw new Error('synthetic provider transport failure');
        }
        return sendProviderRequest({ ...request, url: `${baseUrl}${new URL(request.url).pathname}`, addressPolicy: 'privateNetworkHTTP' });
      },
    });
    const call: Call = {
      context: { flowId: 'kuaidibird-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'kuaidibird', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.queryTracking().invoke(call, QueryTrackingInputSchema.parse(scenario.input));
  });
}
