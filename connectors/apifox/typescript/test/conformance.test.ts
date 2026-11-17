// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { ApifoxClient } from '../src/client.js';
import { GetProjectAPISummaryInputSchema } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/apifox/scenarios.json', import.meta.url));
const connection = { provider: 'apifox', name: 'apifox' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeApifox));

async function invokeApifox(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'getProjectAPISummary') throw new Error(`unknown Apifox operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const client = new ApifoxClient({}, new StaticCredentialProvider([[connection, { accessToken: new SecretString('test-secret') }]]), {
      send: request => sendProviderRequest({
        ...request, url: `${baseUrl}${new URL(request.url).pathname}`, addressPolicy: 'privateNetworkHTTP',
      }),
    });
    const call: Call = {
      context: { flowId: 'apifox-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'apifox', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return client.getProjectAPISummary().invoke(call, GetProjectAPISummaryInputSchema.parse(scenario.input));
  });
}
