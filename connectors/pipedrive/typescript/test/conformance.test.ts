// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { PipedriveClient } from '../src/client.js';
import { CreateObjectInputSchema, GetObjectInputSchema, ListObjectsInputSchema, SearchObjectsInputSchema, UpdateObjectInputSchema, UpsertObjectInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/pipedrive/scenarios.json', import.meta.url));
const connection = { provider: 'pipedrive', name: 'pipedrive-crm' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokePipedrive));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokePipedrive(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const credentials = new StaticCredentialProvider([[connection, { authMethodId: 'api-token', apiToken: new SecretString('pipedrive-unit-test-api-token') }]]);
  const client = new PipedriveClient({ endpoint: 'http://127.0.0.1:12345' }, credentials, { send });
  const call = scenarioCall(scenario, connection, 'pipedrive', context) as Call;
  if (scenario.operation === 'searchObjects') return client.searchObjects().invoke(call, SearchObjectsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getObject') return client.getObject().invoke(call, GetObjectInputSchema.parse(scenario.input));
  if (scenario.operation === 'listObjects') return client.listObjects().invoke(call, ListObjectsInputSchema.parse(scenario.input));
  if (scenario.operation === 'upsertObject') return productionMutation(client.upsertObject(), call, UpsertObjectInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'createObject') return productionMutation(client.createObject(), call, CreateObjectInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'updateObject') return productionMutation(client.updateObject(), call, UpdateObjectInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown Pipedrive operation ${scenario.operation}`);
}
