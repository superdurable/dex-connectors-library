// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { WebflowClient, GetCollectionItemInputSchema, ListCollectionItemsInputSchema } from '../src/index.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/webflow/scenarios.json', import.meta.url));
const connection = { provider: 'webflow', name: 'webflow' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWebflow));

async function invokeWebflow(scenario: Scenario, recorder: ExchangeRecorder) {
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const client = new WebflowClient({ baseUrl: `${baseUrl}/v2` }, new StaticCredentialProvider([[connection, { siteToken: new SecretString('test-token') }]]));
    const call: Call = {
      context: { flowId: 'webflow-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'webflow', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    if (scenario.operation === 'listCollectionItems') return client.listCollectionItems().invoke(call, ListCollectionItemsInputSchema.parse(scenario.input));
    if (scenario.operation === 'getCollectionItem') return client.getCollectionItem().invoke(call, GetCollectionItemInputSchema.parse(scenario.input));
    throw new TypeError(`unknown Webflow operation ${scenario.operation}`);
  });
}
