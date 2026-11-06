// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { Client, ListProjectsInputSchema, ListTimeEntriesInputSchema } from '../src/index.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/harvest/scenarios.json', import.meta.url));
const connection = { provider: 'harvest', name: 'harvest' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeHarvest));

async function invokeHarvest(scenario: Scenario, recorder: ExchangeRecorder) {
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const client = new Client({ baseUrl, accountId: '123' }, new StaticCredentialProvider([[connection, { apiKey: new SecretString('test-secret-key') }]]));
    const call = scenarioCall(scenario, connection, 'harvest', {
      flowId: 'harvest-conformance', cancellationSignal: new AbortController().signal,
    }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
    if (scenario.operation === 'listProjects') return client.listProjects().invoke(call, ListProjectsInputSchema.parse(scenario.input));
    if (scenario.operation === 'listTimeEntries') return client.listTimeEntries().invoke(call, ListTimeEntriesInputSchema.parse(scenario.input));
    throw new TypeError(`unknown Harvest operation ${scenario.operation}`);
  });
}
