// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, invokeProductionMutation, loadScenarios, replay, scenarioCall, type ExchangeRecorder, type HeartbeatContext, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { ContactsClient } from '../src/client.js';
import { CreateContactInputSchema, GetContactInputSchema, ListContactsInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/google/contacts/scenarios.json', import.meta.url));
const connection = { provider: 'google-contacts', name: 'google-contacts' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeContacts));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeContacts(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const credentials = [[connection, {
    oauthClientId: 'test-client', oauthClientSecret: new SecretString('test-client-secret'),
    accessToken: new SecretString('test-contacts-token'), refreshToken: new SecretString('test-refresh-token'),
  }]] as const;
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new ContactsClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider(credentials), { send });
  const call = scenarioCall(scenario, connection, 'google-contacts', context,
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  switch (scenario.operation) {
    case 'listContacts': return client.listContacts().invoke(call, ListContactsInputSchema.parse(scenario.input));
    case 'getContact': return client.getContact().invoke(call, GetContactInputSchema.parse(scenario.input));
    case 'createContact': return productionMutation(client.createContact(), call, CreateContactInputSchema.parse(scenario.input), scenario.idempotencyKey!);
    default: throw new Error(`unsupported Contacts operation ${scenario.operation}`);
  }
}
