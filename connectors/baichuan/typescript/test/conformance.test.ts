// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { BaichuanClient } from '../src/client.js';
import { ListKnowledgeBasesInputSchema, type Credentials } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/baichuan/scenarios.json', import.meta.url));
const connection = { provider: 'baichuan', name: 'baichuan' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeBaichuan));

async function invokeBaichuan(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'listKnowledgeBases') throw new Error(`unknown Baichuan operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const credentialEntries: Array<[typeof connection, Credentials]> = scenario.name === 'listKnowledgeBases-defect' ? [] : [[connection, { apiKey: new SecretString('test-baichuan-key-123') }]];
    const client = new BaichuanClient({ baseUrl, ...(scenario.name === 'listKnowledgeBases-oversized' ? { maxResponseBytes: 30 } : {}) }, new StaticCredentialProvider<Credentials>(credentialEntries), {
      send: request => {
        if (scenario.exchanges[0]?.transportFailure) {
          recorder.record({ method: request.method, url: request.url, headers: request.headers ?? {}, body: request.body });
          throw new Error('synthetic pre-dispatch transport failure');
        }
        return sendProviderRequest(request);
      },
    });
    const call: Call = {
      context: { flowId: 'baichuan-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'baichuan', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.listKnowledgeBases().invoke(call, ListKnowledgeBasesInputSchema.parse(scenario.input));
  });
}
