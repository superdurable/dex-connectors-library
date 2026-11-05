// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { Client } from '../src/client.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/smartsheet/scenarios.json', import.meta.url));
const connection = { provider: 'smartsheet', name: 'smartsheet' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSmartsheet));

async function invokeSmartsheet(scenario: Scenario, recorder: ExchangeRecorder) {
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const client = new Client({ baseUrl: `${baseUrl}/2.0` }, new StaticCredentialProvider([[connection, { apiKey: new SecretString('test-secret-key') }]]));
    const call: Call = {
      context: { flowId: 'smartsheet-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'smartsheet', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    if (scenario.operation === 'listSheets') return client.listSheets().invoke(call, scenario.input as { page?: number; pageSize?: number });
    if (scenario.operation === 'getSheet') return client.getSheet().invoke(call, scenario.input as { sheetId: number });
    throw new Error(`unknown operation ${scenario.operation}`);
  });
}
