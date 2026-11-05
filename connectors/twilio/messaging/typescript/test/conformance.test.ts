// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { TwilioClient } from '../src/client.js';
import { GetMessageInputSchema, SendMessageInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/twilio/messaging/scenarios.json', import.meta.url));
const connection = { provider: 'twilio', name: 'twilio-test' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeTwilio, '/2010-04-01'));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeTwilio(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const isAPIKey = scenario.name.startsWith('api key ');
  const client = new TwilioClient({ endpoint: 'http://127.0.0.1:12345/2010-04-01', accountSid: 'AC00000000000000000000000000000000', defaultSender: '+14155550100' }, {
    resolve: async () => isAPIKey
      ? { authMethodId: 'api-key', apiKeySid: '  SK' + '0123456789abcdef0123456789abcdef  ', apiKeySecret: new SecretString('api-key-secret-sentinel-012345678') }
      : { authMethodId: 'auth-token', authToken: new SecretString('auth-token-sentinel-0123456789ab') },
  }, { send });
  const call = scenarioCall(scenario, connection, 'twilio-messaging', context,
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'sendMessage') return productionMutation(client.sendMessage(), call, SendMessageInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'getMessage') return client.getMessage().invoke(call, GetMessageInputSchema.parse(scenario.input));
  throw new Error(`unknown Twilio operation ${scenario.operation}`);
}
