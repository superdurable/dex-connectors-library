// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { ZendeskClient } from '../src/client.js';
import { CreateTicketInputSchema, GetTicketInputSchema, SearchTicketsInputSchema, UpdateTicketInputSchema, type CreateTicketInput, type UpdateTicketInput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/zendesk/support/scenarios.json', import.meta.url));
const connection = { provider: 'zendesk', name: 'zendesk-support' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeZendesk, '/api/v2'));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeZendesk(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new ZendeskClient({ subdomain: 'acme' }, { resolve: async () => ({ email: 'agent@acme.example.com', apiToken: new SecretString('zendeskTestToken0123456789abcdef') }) },
    { apiBaseURL: 'http://127.0.0.1:12345/api/v2', send: sender, now: () => new Date('2026-01-28T08:45:00Z') });
  const call = scenarioCall(scenario, connection, 'zendesk-support', { flowId: 'zendesk-flow', cancellationSignal: new AbortController().signal },
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'searchTickets') return client.searchTickets().invoke(call, SearchTicketsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getTicket') return client.getTicket().invoke(call, GetTicketInputSchema.parse(scenario.input));
  if (scenario.operation === 'createTicket') return productionMutation(client.createTicket(), call, CreateTicketInputSchema.parse(scenario.input) as CreateTicketInput, scenario.idempotencyKey!);
  if (scenario.operation === 'updateTicket') return productionMutation(client.updateTicket(), call, UpdateTicketInputSchema.parse(scenario.input) as UpdateTicketInput, scenario.idempotencyKey!);
  throw new Error(`unknown Zendesk operation ${scenario.operation}`);
}
