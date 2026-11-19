// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { IFLYTEKClient } from '../src/client.js';
import { CreateChatCompletionInputSchema, type Credentials } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/iflytek/scenarios.json', import.meta.url));
const connection = { provider: 'iflytek', name: 'iflytek' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeIFLYTEK));

async function invokeIFLYTEK(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'createChatCompletion') throw new Error(`unknown IFLYTEK operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const credentials: Credentials = { apiPassword: new SecretString('spark-test-password') };
    const client = new IFLYTEKClient({ ...(scenario.name === 'createChatCompletion-oversized' ? { maxResponseBytes: 16 } : {}) }, new StaticCredentialProvider<Credentials>([[connection, credentials]]), {
      send: request => {
        if (scenario.exchanges[0]?.transportFailure) {
          recorder.record({ method: request.method, url: request.url, headers: request.headers ?? {}, body: request.body });
          throw new Error('synthetic provider transport failure');
        }
        return sendProviderRequest({ ...request, url: `${baseUrl}/v1/chat/completions`, addressPolicy: 'privateNetworkHTTP' });
      },
    });
    const call: Call = {
      context: { flowId: 'iflytek-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'iflytek', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.createChatCompletion().invoke(call, CreateChatCompletionInputSchema.parse(scenario.input));
  });
}
