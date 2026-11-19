// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { KunlunClient } from '../src/client.js';
import { GetTaskStatusInputSchema, type Credentials } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/kunlun/scenarios.json', import.meta.url));
const connection = { provider: 'kunlun', name: 'kunlun' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeKunlun));

async function invokeKunlun(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getTaskStatus') throw new Error(`unknown Kunlun operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const credentials: Credentials = { apiKey: new SecretString('secret-api-key') };
    const client = new KunlunClient({}, new StaticCredentialProvider([[connection, credentials]]), {
      send: request => {
        if (scenario.exchanges[0]?.transportFailure) {
          recorder.record({ method: request.method, url: request.url, headers: request.headers ?? {}, body: request.body });
          throw new Error('synthetic provider transport failure');
        }
        return sendProviderRequest({ ...request, url: `${baseUrl}${new URL(request.url).pathname}`, addressPolicy: 'privateNetworkHTTP' });
      },
    });
    const call: Call = {
      context: { flowId: 'kunlun-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'kunlun', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.getTaskStatus().invoke(call, GetTaskStatusInputSchema.parse(scenario.input));
  });
}
