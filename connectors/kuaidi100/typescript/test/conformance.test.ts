// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { Kuaidi100Client } from '../src/client.js';
import { QueryTrackingInputSchema, type Credentials } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/kuaidi100/scenarios.json', import.meta.url));
const connection = { provider: 'kuaidi100', name: 'kuaidi100' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeKuaidi100));

async function invokeKuaidi100(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'queryTracking') throw new Error(`unknown Kuaidi100 operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const credentials: Credentials = { customer: new SecretString('test-customer'), key: new SecretString('test-key') };
    const client = new Kuaidi100Client({ baseUrl }, new StaticCredentialProvider([[connection, credentials]]), {
      send: request => {
        if (scenario.exchanges[0]?.transportFailure) {
          recorder.record({ method: request.method, url: request.url, headers: request.headers ?? {}, body: request.body });
          throw new Error('synthetic provider transport failure');
        }
        return sendProviderRequest({ ...request, url: `${baseUrl}${new URL(request.url).pathname}`, addressPolicy: 'privateNetworkHTTP' });
      },
    });
    const call: Call = {
      context: { flowId: 'kuaidi100-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'kuaidi100', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.queryTracking().invoke(call, QueryTrackingInputSchema.parse(scenario.input));
  });
}
