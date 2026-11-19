// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { LixingerClient } from '../src/client.js';
import { ListStockCompaniesInputSchema, type Credentials } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/lixinger/scenarios.json', import.meta.url));
const connection = { provider: 'lixinger', name: 'lixinger' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeLixinger));

async function invokeLixinger(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listStockCompanies') throw new Error(`unknown Lixinger operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const credentials: Credentials = { token: new SecretString('private-test-token') };
    const client = new LixingerClient({}, new StaticCredentialProvider([[connection, credentials]]), {
      send: request => {
        if (scenario.exchanges[0]?.transportFailure) {
          recorder.record({ method: request.method, url: request.url, headers: request.headers ?? {}, body: request.body });
          throw new Error('synthetic provider transport failure');
        }
        return sendProviderRequest({ ...request, url: `${baseUrl}${new URL(request.url).pathname}`, addressPolicy: 'privateNetworkHTTP' });
      },
    });
    const call: Call = {
      context: { flowId: 'lixinger-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'lixinger', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.listStockCompanies().invoke(call, ListStockCompaniesInputSchema.parse(scenario.input));
  });
}
