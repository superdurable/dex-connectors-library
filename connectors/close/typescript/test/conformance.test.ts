// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { CloseClient } from '../src/client.js';
import { GetLeadInputSchema, ListLeadsInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/close/scenarios.json', import.meta.url));
const connection = { provider: 'close', name: 'close' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeClose));

async function invokeClose(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new CloseClient({ baseUrl: 'http://127.0.0.1:21201/api/v1' }, {
    resolve: async () => ({ apiKey: new SecretString('test-secret-key') }),
  }, { send: sender });
  const call = scenarioCall(scenario, connection, 'close', { cancellationSignal: new AbortController().signal }) as Call;
  if (scenario.operation === 'listLeads') return client.listLeads().invoke(call, ListLeadsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getLead') return client.getLead().invoke(call, GetLeadInputSchema.parse(scenario.input));
  throw new Error(`unknown Close operation ${scenario.operation}`);
}
