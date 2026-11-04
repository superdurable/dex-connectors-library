// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { StripeClient } from '../src/client.js';
import { CreateCheckoutSessionInputSchema, CreateRefundInputSchema, GetCheckoutSessionInputSchema, GetPaymentIntentInputSchema, type CreateACHCheckoutSessionInput, type CreateRefundInput } from '../src/models.js';
import { noProgress } from './call.js';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/stripe/scenarios.json', import.meta.url));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeStripe, '/v1'));

async function invokeStripe(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new StripeClient({ endpoint: 'http://127.0.0.1:12345/v1' }, {
    resolve: async () => ({ authMethodId: 'api-key-only', secretKey: new SecretString('sk_test_example') }),
  }, { send: sender, now: () => new Date('2026-10-30T00:00:00Z') });
  const call = scenarioCall(scenario, { provider: 'stripe', name: 'payments' }, 'stripe',
    { flowId: 'stripe-conformance' }, noProgress) as Call;
  if (scenario.operation === 'createCardCheckoutSession') return productionMutation(client.createCardCheckoutSession(), call, CreateCheckoutSessionInputSchema.parse(scenario.input) as CreateACHCheckoutSessionInput, scenario.idempotencyKey!);
  if (scenario.operation === 'createACHCheckoutSession') return productionMutation(client.createACHCheckoutSession(), call, CreateCheckoutSessionInputSchema.parse(scenario.input) as CreateACHCheckoutSessionInput, scenario.idempotencyKey!);
  if (scenario.operation === 'getCheckoutSession') return client.getCheckoutSession().invoke(call, GetCheckoutSessionInputSchema.parse(scenario.input));
  if (scenario.operation === 'getPaymentIntent') return client.getPaymentIntent().invoke(call, GetPaymentIntentInputSchema.parse(scenario.input));
  if (scenario.operation === 'createRefund') return productionMutation(client.createRefund(), call, CreateRefundInputSchema.parse(scenario.input) as CreateRefundInput, scenario.idempotencyKey!);
  throw new Error(`unknown operation ${scenario.operation}`);
}
