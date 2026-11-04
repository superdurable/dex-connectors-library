// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { GorgiasClient } from '../src/client.js';
import { AddNoteInputSchema, CreateTicketInputSchema, FindCustomerByEmailInputSchema, GetTicketInputSchema, SearchTicketsInputSchema, UpdateTicketInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/gorgias/scenarios.json', import.meta.url));
const connection = { provider: 'gorgias', name: 'gorgias-test' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeGorgias, '/api'));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeGorgias(scenario: Scenario, recorder: ExchangeRecorder, context: unknown) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new GorgiasClient({ domain: 'acme' }, {
    resolve: async () => ({ email: 'agent@acme.example.com', apiKey: new SecretString('gorgiasTestKey0123456789abcdef') }),
  }, { apiBaseURL: 'http://127.0.0.1:12345/api', send: sender, now: () => new Date('2026-11-02T00:00:00Z') });
  const call = scenarioCall(scenario, connection, 'gorgias', context, {
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  }) as Call;
  if (scenario.operation === 'searchTickets') return client.searchTickets().invoke(call, SearchTicketsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getTicket') return client.getTicket().invoke(call, GetTicketInputSchema.parse(scenario.input));
  if (scenario.operation === 'findCustomerByEmail') return client.findCustomerByEmail().invoke(call, FindCustomerByEmailInputSchema.parse(scenario.input));
  if (scenario.operation === 'createTicket') return productionMutation(client.createTicket(), call, CreateTicketInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'updateTicket') return productionMutation(client.updateTicket(), call, UpdateTicketInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'addNote') return productionMutation(client.addNote(), call, AddNoteInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown Gorgias operation ${scenario.operation}`);
}
