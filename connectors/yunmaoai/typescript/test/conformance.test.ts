// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { YunMaoAIClient } from '../src/client.js';
import { ListModelsInputSchema, type Credentials } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/yunmaoai/scenarios.json', import.meta.url));
const connection = { provider: 'yunmaoai', name: 'yunmaoai' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeYunMaoAIAPI));

async function invokeYunMaoAIAPI(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listModels') throw new Error(`unknown YunMaoAI operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const credentials: Credentials = { apiKey: new SecretString(scenario.name === 'listModels-defect' ? '' : 'test-secret') };
    const client = new YunMaoAIClient({ ...(scenario.name === 'listModels-oversized' ? { maxResponseBytes: 16 } : {}) }, new StaticCredentialProvider<Credentials>([[connection, credentials]]), {
      send: request => {
        if (scenario.exchanges[0]?.transportFailure) {
          recorder.record({ method: request.method, url: request.url, headers: request.headers ?? {}, body: request.body });
          throw new Error('synthetic provider transport failure');
        }
        return sendProviderRequest({ ...request, url: `${baseUrl}/v1/models`, addressPolicy: 'privateNetworkHTTP' });
      },
    });
    const call: Call = {
      context: { flowId: 'yunmaoai-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'yunmaoai', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.listModels().invoke(call, ListModelsInputSchema.parse(scenario.input));
  });
}
