// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { HubSpotClient } from '../src/client.js';
import { GetObjectInputSchema, SearchObjectsInputSchema, UpdateObjectInputSchema, UpsertObjectInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/hubspot/scenarios.json', import.meta.url));
const connection = { provider: 'hubspot', name: 'hubspot-crm' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeHubSpot));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeHubSpot(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const credentials = new StaticCredentialProvider([[connection, { authMethodId: 'private-app-token', accessToken: new SecretString('pat-na1-test-token') }]]);
  const client = new HubSpotClient({ endpoint: 'http://127.0.0.1:44720' }, credentials, { send });
  const call = scenarioCall(scenario, connection, 'hubspot', {
    flowId: 'hubspot-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'searchObjects') return client.searchObjects().invoke(call, SearchObjectsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getObject') return client.getObject().invoke(call, GetObjectInputSchema.parse(scenario.input));
  if (scenario.operation === 'upsertObject') return productionMutation(client.upsertObject(), call, UpsertObjectInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'updateObject') return productionMutation(client.updateObject(), call, UpdateObjectInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown HubSpot operation ${scenario.operation}`);
}
