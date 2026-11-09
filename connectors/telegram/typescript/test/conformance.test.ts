// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { TelegramClient, GetChatInputSchema, SendMessageInputSchema } from '../src/index.js';
import {
  answerRecordedRequest, invokeProductionMutation, loadScenarios, replay, scenarioCall,
  type ExchangeRecorder, type HeartbeatContext, type Scenario,
} from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/telegram/scenarios.json', import.meta.url));
const connection = { provider: 'telegram', name: 'telegram' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTelegram));

async function invokeTelegram(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new TelegramClient({ baseUrl: 'http://127.0.0.1:12345' },
    { resolve: async () => ({ botToken: new SecretString('123456:fake_test_token') }) },
    { send: sender });
  const call = scenarioCall(scenario, connection, 'telegram', context,
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'sendMessage') {
    const input = SendMessageInputSchema.parse(scenario.input);
    return invokeProductionMutation(client.sendMessage(), scenario.idempotencyKey!,
      recording => runMutation(recording as Mutation<typeof input, { chatId: number; messageId: number; date: number }>,
        call, input), error => error instanceof RetryError);
  }
  if (scenario.operation === 'getChat') return client.getChat().invoke(call, GetChatInputSchema.parse(scenario.input));
  throw new Error(`unknown Telegram operation ${scenario.operation}`);
}
