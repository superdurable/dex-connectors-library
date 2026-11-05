// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { ZohoDeskClient } from '../src/client.js';
import type { SearchTicketsInput, GetTicketInput, CreateTicketInput, UpdateTicketInput, AddCommentInput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/zoho/desk/scenarios.json', import.meta.url));
const connection = { provider: 'zoho', name: 'zoho-desk-test' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeZohoDesk, '/api/v1'));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key, recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}
async function invokeZohoDesk(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const credentials = new StaticCredentialProvider([[connection, {
    authMethodId: 'zoho-us-oauth', oauthClientId: '1000.GMB0YULZHJK411248S8I5GZ4CHUEX0',
    oauthClientSecret: new SecretString('zoho-client-secret'), accessToken: new SecretString('1000.zohoDeskTestAccessToken0123456789'),
    refreshToken: new SecretString('1000.zohoDeskTestRefreshToken'),
  }]]);
  const client = new ZohoDeskClient({ orgId: '2389290' }, credentials, { apiBaseURL: 'http://127.0.0.1:12345/api/v1', send: sender,
    now: () => new Date(scenario.fixedClock ?? '1970-01-01T00:00:01Z') });
  const call = scenarioCall(scenario, connection, 'zoho-desk', context, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'searchTickets') return client.searchTickets().invoke(call, scenario.input as unknown as SearchTicketsInput);
  if (scenario.operation === 'getTicket') return client.getTicket().invoke(call, scenario.input as unknown as GetTicketInput);
  if (scenario.operation === 'createTicket') return productionMutation(client.createTicket(), call, scenario.input as unknown as CreateTicketInput, scenario.idempotencyKey!);
  if (scenario.operation === 'updateTicket') return productionMutation(client.updateTicket(), call, scenario.input as unknown as UpdateTicketInput, scenario.idempotencyKey!);
  if (scenario.operation === 'addComment') return productionMutation(client.addComment(), call, scenario.input as unknown as AddCommentInput, scenario.idempotencyKey!);
  throw new Error(`unknown Zoho Desk operation ${scenario.operation}`);
}
