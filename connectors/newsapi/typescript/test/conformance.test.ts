// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { SearchArticlesOperation, type SearchArticlesInput } from '../src/index.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/newsapi/scenarios.json', import.meta.url));
const connection = { provider: 'newsapi', name: 'newsapi' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeNewsapi));

async function invokeNewsapi(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'searchArticles') throw new Error(`unknown operation ${scenario.operation}`);
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const operation = new SearchArticlesOperation(
      { baseUrl: `${baseUrl}/v2` },
      new StaticCredentialProvider([[connection, { apiKey: new SecretString('0123456789abcdef0123456789abcdef') }]]),
    );
    const call: Call = {
      context: { flowId: 'newsapi-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'newsapi', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    return operation.invoke(call, scenario.input as SearchArticlesInput);
  });
}
