// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { SQSClient } from '../src/client.js';
import { ChangeMessageVisibilityInputSchema, DeleteMessageInputSchema, ReceiveMessagesInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/amazon/sqs/scenarios.json', import.meta.url));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSQS));

async function invokeSQS(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new SQSClient({ region: 'eu-west-1', endpoint: 'http://127.0.0.1:12345',
    defaultQueueUrl: 'http://127.0.0.1:12345/123456789012/orders' }, {
    resolve: async () => ({ accessKeyId: new SecretString('AKIASQSCONNECTORTEST'),
      secretAccessKey: new SecretString('sqs-connector-test/secret+value') }),
  }, { send: sender, now: () => new Date('2026-10-30T00:00:00Z') });
  const call = scenarioCall(scenario, { provider: 'amazon-sqs', name: 'sqs-test' }, 'amazon-sqs', context, {
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  }) as Call;
  if (scenario.operation === 'receiveMessages') return productionMutation(client.receiveMessages(), call, ReceiveMessagesInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'deleteMessage') return productionMutation(client.deleteMessage(), call, DeleteMessageInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'changeMessageVisibility') return productionMutation(client.changeMessageVisibility(), call, ChangeMessageVisibilityInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown operation ${scenario.operation}`);
}
