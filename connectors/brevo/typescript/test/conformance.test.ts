// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { BrevoClient } from '../src/client.js';
import { CreateOrUpdateContactInputSchema, GetContactInputSchema, type CreateOrUpdateContactInput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/brevo/scenarios.json', import.meta.url));
const connection = { provider: 'brevo', name: 'brevo' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeBrevo));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeBrevo(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new BrevoClient({ baseUrl: 'http://127.0.0.1:12345/v3' }, {
    resolve: async () => ({ apiKey: new SecretString('brevo-test-key') }),
  }, { send: sender });
  const call = scenarioCall(scenario, connection, 'brevo', { flowId: 'brevo-conformance' },
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'createOrUpdateContact') return productionMutation(client.createOrUpdateContact(), call, CreateOrUpdateContactInputSchema.parse(scenario.input) as CreateOrUpdateContactInput, scenario.idempotencyKey!);
  if (scenario.operation === 'getContact') return client.getContact().invoke(call, GetContactInputSchema.parse(scenario.input));
  throw new Error(`unknown Brevo operation ${scenario.operation}`);
}
