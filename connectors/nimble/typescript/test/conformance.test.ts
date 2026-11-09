// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { NimbleClient } from '../src/client.js';
import { GetContactInputSchema, ListContactIDsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/nimble/scenarios.json', import.meta.url));
const connection = { provider: 'nimble', name: 'nimble' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeNimble, '/api/v1'));

async function invokeNimble(scenario: Scenario, recorder: ExchangeRecorder) {
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = scenario.name === 'listContactIDs-defect' ? [] : [[connection, { apiKey: new SecretString('test-token') }]];
  const client = new NimbleClient({ baseUrl: 'http://127.0.0.1:12345/api/v1' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'nimble-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'nimble', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listContactIDs') return client.listContactIDs().invoke(call, ListContactIDsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getContact') return client.getContact().invoke(call, GetContactInputSchema.parse(scenario.input));
  throw new Error(`unknown Nimble operation ${scenario.operation}`);
}
