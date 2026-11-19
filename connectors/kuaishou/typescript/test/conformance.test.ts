// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { KuaishouClient } from '../src/client.js';
import { ListUserVideosInputSchema, type Credentials } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/kuaishou/scenarios.json', import.meta.url));
const connection = { provider: 'kuaishou', name: 'kuaishou' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeKuaishou));

async function invokeKuaishou(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listUserVideos') throw new Error(`unknown Kuaishou operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const credentials: Credentials = { accessToken: new SecretString('token456'), appID: 'app123' };
    const client = new KuaishouClient({ baseUrl }, new StaticCredentialProvider([[connection, credentials]]), {
      send: request => {
        if (scenario.exchanges[0]?.transportFailure) {
          recorder.record({ method: request.method, url: request.url, headers: request.headers ?? {}, body: request.body });
          throw new Error('synthetic provider transport failure');
        }
        return sendProviderRequest({ ...request, url: `${baseUrl}${new URL(request.url).pathname}${new URL(request.url).search}`, addressPolicy: 'privateNetworkHTTP' });
      },
    });
    const call: Call = {
      context: { flowId: 'kuaishou-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'kuaishou', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.listUserVideos().invoke(call, ListUserVideosInputSchema.parse(scenario.input));
  });
}
