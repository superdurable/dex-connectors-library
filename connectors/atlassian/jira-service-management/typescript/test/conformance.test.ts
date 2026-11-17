// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { JiraServiceManagementClient } from '../src/client.js';
import { AddCommentInputSchema, CreateTicketInputSchema, FindCustomerByEmailInputSchema, GetTicketInputSchema, SearchTicketsInputSchema, TransitionTicketInputSchema, UpdateTicketInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/atlassian/jira-service-management/scenarios.json', import.meta.url));
const connection = { provider: 'atlassian', name: 'jsm-test' };
const noProgress = { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeJira));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key, recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeJira(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new JiraServiceManagementClient({ endpoint: 'http://127.0.0.1:12345', cloudId: '11223344-a1b2-4b33-8c44-def123456789' }, new StaticCredentialProvider([[connection, {
    accessToken: new SecretString('jsm-access-token'), refreshToken: new SecretString('inert-refresh-token'), oauthClientId: 'inert-client-id', oauthClientSecret: new SecretString('inert-client-secret'),
  }]]), { send, now: () => new Date('2026-10-30T00:00:00Z') });
  const call = scenarioCall(scenario, connection, 'jira-service-management', context, noProgress) as Call;
  if (scenario.operation === 'findCustomerByEmail') return client.findCustomerByEmail().invoke(call, FindCustomerByEmailInputSchema.parse(scenario.input));
  if (scenario.operation === 'searchTickets') return client.searchTickets().invoke(call, SearchTicketsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getTicket') return client.getTicket().invoke(call, GetTicketInputSchema.parse(scenario.input));
  if (scenario.operation === 'createTicket') return productionMutation(client.createTicket(), call, CreateTicketInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'updateTicket') return productionMutation(client.updateTicket(), call, UpdateTicketInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'transitionTicket') return productionMutation(client.transitionTicket(), call, TransitionTicketInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'addComment') return productionMutation(client.addComment(), call, AddCommentInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown operation ${scenario.operation}`);
}
