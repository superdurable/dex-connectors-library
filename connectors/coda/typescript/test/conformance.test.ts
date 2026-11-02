// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { CodaClient } from '../src/client.js';
import { GetRowInputSchema, ListRowsInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/coda/scenarios.json', import.meta.url));
const connection = { provider: 'coda', name: 'coda' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeCoda));

async function invokeCoda(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new CodaClient({ baseUrl: 'http://127.0.0.1:12345/apis/v1' }, {
    resolve: async () => ({ apiToken: new SecretString('test-token') }),
  }, { send: sender });
  const call = scenarioCall(scenario, connection, 'coda', {
    flowId: 'coda-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'listRows') return client.listRows().invoke(call, ListRowsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getRow') return client.getRow().invoke(call, GetRowInputSchema.parse(scenario.input));
  throw new Error(`unknown Coda operation ${scenario.operation}`);
}
