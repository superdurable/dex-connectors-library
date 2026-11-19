// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { QingflowClient } from '../src/client.js';
import { ListApplicationsInputSchema, type Credentials } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/qingflow/scenarios.json', import.meta.url));
const connection = { provider: 'qingflow', name: 'qingflow' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeQingflow));

async function invokeQingflow(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listApplications') throw new Error(`unknown Qingflow operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const credentials: Credentials = { accessToken: new SecretString(scenario.name === 'listApplications-defect' ? '' : 'private-test-key') };
    const client = new QingflowClient({ ...(scenario.name === 'listApplications-oversized' ? { maxResponseBytes: 16 } : {}) }, new StaticCredentialProvider<Credentials>([[connection, credentials]]), {
      send: request => {
        if (scenario.exchanges[0]?.transportFailure) {
          recorder.record({ method: request.method, url: request.url, headers: request.headers ?? {}, body: request.body });
          throw new Error('synthetic provider transport failure');
        }
        return sendProviderRequest({ ...request, url: `${baseUrl}/app`, addressPolicy: 'privateNetworkHTTP' });
      },
    });
    const call: Call = {
      context: { flowId: 'qingflow-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'qingflow', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.listApplications().invoke(call, ListApplicationsInputSchema.parse(scenario.input));
  });
}
