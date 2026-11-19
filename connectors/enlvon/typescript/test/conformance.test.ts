// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { EnlvonClient } from '../src/client.js';
import { SearchPoliciesInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/enlvon/scenarios.json', import.meta.url));
const connection = { provider: 'enlvon', name: 'enlvon' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeEnlvon, ''));

async function invokeEnlvon(scenario: Scenario, recorder: ExchangeRecorder) {
  const apiKey = scenario.name.endsWith('missing-credentials') ? '' : 'enl_test-secret';
  const credentials: Array<[typeof connection, { apiKey: SecretString }]> = [[connection, { apiKey: new SecretString(apiKey) }]];
  const client = new EnlvonClient({ maxResponseBytes: scenario.name.endsWith('oversize') ? 8 : 2097152 }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'enlvon-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'enlvon', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'searchPolicies') return client.searchPolicies().invoke(call, SearchPoliciesInputSchema.parse(scenario.input));
  throw new Error(`unknown Enlvon operation ${scenario.operation}`);
}
