// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { FreshdeskClient } from '../src/client.js';
import { AddNoteInputSchema, CreateTicketInputSchema, GetTicketInputSchema, SearchTicketsInputSchema, UpdateTicketInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/freshworks/freshdesk/scenarios.json', import.meta.url));

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeFreshdesk, '/api/v2'));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key, recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeFreshdesk(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new FreshdeskClient({ domain: 'acme' }, { resolve: async () => ({ apiKey: new SecretString('freshdeskTestKey0123456789') }) }, { apiBaseURL: 'http://127.0.0.1:12345/api/v2', send: sender });
  const call = scenarioCall(scenario, { provider: 'freshdesk', name: 'freshdesk-test' }, 'freshdesk', context, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'searchTickets') return client.searchTickets().invoke(call, SearchTicketsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getTicket') return client.getTicket().invoke(call, GetTicketInputSchema.parse(scenario.input));
  if (scenario.operation === 'createTicket') return productionMutation(client.createTicket(), call, CreateTicketInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'updateTicket') return productionMutation(client.updateTicket(), call, UpdateTicketInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'addNote') return productionMutation(client.addNote(), call, AddNoteInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown Freshdesk operation ${scenario.operation}`);
}
