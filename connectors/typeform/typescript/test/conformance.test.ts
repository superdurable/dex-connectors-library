// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { runMutation, RetryError, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { TypeformClient } from '../src/client.js';
import { GetFormInputSchema, ListFormsInputSchema, ListResponsesInputSchema, UpsertWebhookInputSchema, type UpsertWebhookInput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/typeform/scenarios.json', import.meta.url));
const connection = { provider: 'typeform', name: 'forms' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTypeform));
function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key, recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}
async function invokeTypeform(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new TypeformClient({}, { resolve: async () => ({ accessToken: new SecretString('SENTINEL-TYPEFORM-ACCESS-TOKEN'), webhookSecret: new SecretString('SENTINEL-TYPEFORM-WEBHOOK-SECRET') }) }, { send: sender });
  const call = scenarioCall(scenario, connection, 'typeform', { flowId: 'typeform-conformance', cancellationSignal: new AbortController().signal }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'listForms') return client.listForms().invoke(call, ListFormsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getForm') return client.getForm().invoke(call, GetFormInputSchema.parse(scenario.input));
  if (scenario.operation === 'listResponses') return client.listResponses().invoke(call, ListResponsesInputSchema.parse(scenario.input));
  if (scenario.operation === 'upsertWebhook') return productionMutation(client.upsertWebhook(), call, UpsertWebhookInputSchema.parse(scenario.input) as UpsertWebhookInput, scenario.idempotencyKey!);
  throw new Error(`unknown operation ${scenario.operation}`);
}
