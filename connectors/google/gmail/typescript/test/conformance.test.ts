// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type CredentialRefreshDriver, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { GmailClient } from '../src/client.js';
import { GetMessageInputSchema, ReplyToMessageInputSchema, SendMessageInputSchema, type Credentials } from '../src/models.js';
import { answerRecordedRequest, ExchangeRecorder, HeartbeatContext, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/google/gmail/scenarios.json', import.meta.url));
const connection = { provider: 'google', name: 'support' };
const credentials: Credentials = { accessToken: new SecretString('gmail-token'), primaryEmail: 'owner@example.com' };

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeGmailAttempt, '/gmail/v1'));

async function invokeGmailAttempt(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const driver: CredentialRefreshDriver<Credentials> = { refreshRequired: () => false,
    refresh: async () => { recorder.mismatches.push('unexpected credential refresh'); throw new Error('unexpected refresh'); } };
  const transport = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new GmailClient({ endpoint: 'http://127.0.0.1:12345/gmail/v1' },
    new StaticCredentialProvider([[connection, credentials]]),
    { refreshDriver: driver, transport, now: () => new Date('2026-03-04T09:15:00Z') });
  const call = scenarioCall(scenario, connection, 'gmail', context) as Call;
  if (scenario.operation === 'getMessage') return client.getMessage().invoke(call, GetMessageInputSchema.parse(scenario.input));
  if (scenario.operation === 'sendMessage') return productionMutation(client.sendMessage(), call, SendMessageInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'replyToMessage') return productionMutation(client.replyToMessage(), call, ReplyToMessageInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown operation ${scenario.operation}`);
}
