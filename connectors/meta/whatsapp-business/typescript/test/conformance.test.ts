// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { WhatsAppBusinessClient } from '../src/client.js';
import { ListPhoneNumbersInputSchema, SendTemplateMessageInputSchema, SendTextMessageInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, invokeProductionMutation, loadScenarios, replay, scenarioCall, type ExchangeRecorder, type HeartbeatContext, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/meta/whatsapp-business/scenarios.json', import.meta.url))
  .filter(scenario => scenario.operation === 'listPhoneNumbers' || scenario.operation === 'sendTemplateMessage' || scenario.operation === 'sendTextMessage');
const connection = { provider: 'whatsapp-business', name: 'whatsapp-business' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWhatsAppBusiness, '/v25.0'));

async function invokeWhatsAppBusiness(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const credentials: Array<[typeof connection, { accessToken: SecretString }]> = [[connection, { accessToken: new SecretString('test-system-user-token') }]];
  const client = new WhatsAppBusinessClient({ baseUrl: 'http://127.0.0.1:12345/v25.0' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'whatsapp-business-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'whatsapp-business', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listPhoneNumbers') return client.listPhoneNumbers().invoke(call, ListPhoneNumbersInputSchema.parse(scenario.input));
  if (scenario.operation === 'sendTextMessage') {
    const mutationCall = scenarioCall(scenario, connection, 'whatsapp-business', context) as Call;
    const input = SendTextMessageInputSchema.parse(scenario.input);
    return invokeProductionMutation(client.sendTextMessage(), scenario.idempotencyKey!,
      recording => runMutation(recording as Mutation<typeof input, { messageId: string }>, mutationCall, input), error => error instanceof RetryError);
  }
  if (scenario.operation === 'sendTemplateMessage') {
    const mutationCall = scenarioCall(scenario, connection, 'whatsapp-business', context) as Call;
    const input = SendTemplateMessageInputSchema.parse(scenario.input);
    return invokeProductionMutation(client.sendTemplateMessage(), scenario.idempotencyKey!,
      recording => runMutation(recording as Mutation<typeof input, { messageId: string }>, mutationCall, input), error => error instanceof RetryError);
  }
  throw new Error(`unknown WhatsApp Business operation ${scenario.operation}`);
}
