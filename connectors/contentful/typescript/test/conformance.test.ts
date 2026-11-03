// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ContentfulClient } from '../src/client.js';
import { GetEntryInputSchema, ListEntriesInputSchema } from '../src/generated/models.js';
import { ExchangeRecorder, loadScenarios, replay, withScenarioHTTPServer, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/contentful/scenarios.json', import.meta.url));
const connection = { provider: 'contentful', name: 'contentful' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeContentful));
it('preserves the large integer from the Go-recorded response', async () => {
  const scenario = scenarios.find(candidate => candidate.name === 'getEntry-large-integer');
  if (!scenario) throw new Error('missing large integer conformance scenario');
  const recorder = new ExchangeRecorder(scenario);
  const attempt = await invokeContentful(scenario, recorder);
  recorder.finish();
  if (attempt.kind !== 'branch') throw new Error('expected a successful Query branch');
  if (!JSON.stringify(attempt.value).includes('"large":9007199254740994')) throw new Error('provider integer changed');
});

async function invokeContentful(scenario: Scenario, recorder: ExchangeRecorder) {
  return withScenarioHTTPServer(recorder, async baseUrl => {
    const client = new ContentfulClient({ baseUrl }, new StaticCredentialProvider([[connection, { deliveryToken: new SecretString('test-token') }]]));
    const call: Call = {
      context: { flowId: 'contentful-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'contentful', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    if (scenario.operation === 'getEntry') return client.getEntry().invoke(call, GetEntryInputSchema.parse(scenario.input));
    if (scenario.operation === 'listEntries') return client.listEntries().invoke(call, ListEntriesInputSchema.parse(scenario.input));
    throw new Error(`unknown Contentful operation ${scenario.operation}`);
  });
}
