// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { AgoraClient } from '../src/client.js';
import { ListRunningAgentsInputSchema, type Credentials } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/agora/scenarios.json', import.meta.url));
const connection = { provider: 'agora', name: 'agora' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeAgora));

async function invokeAgora(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listRunningAgents') throw new Error(`unknown Agora operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const credentialEntries: Array<[typeof connection, Credentials]> = scenario.name === 'listRunningAgents-defect' ? [] : [[connection, {
      appId: '1234567890abcdef1234567890abcdef', customerId: 'customer-id', customerSecret: new SecretString('customer-secret'),
    }]];
    const client = new AgoraClient({ baseUrl }, new StaticCredentialProvider<Credentials>(credentialEntries), {
      send: request => {
        if (scenario.exchanges[0]?.transportFailure) {
          recorder.record({ method: request.method, url: request.url, headers: request.headers ?? {}, body: request.body });
          throw new Error('synthetic provider transport failure');
        }
        return sendProviderRequest({ ...request, addressPolicy: 'privateNetworkHTTP' });
      },
    });
    const call: Call = {
      context: { flowId: 'agora-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'agora', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.listRunningAgents().invoke(call, ListRunningAgentsInputSchema.parse(scenario.input));
  });
}
