// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { CalendarClient } from '../src/client.js';
import type { CreateEventInput, GetEventInput, ListEventsInput, QueryFreeBusyInput, UpdateEventInput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/google/calendar/scenarios.json', import.meta.url));
const connection = { provider: 'google', name: 'google-calendar-test' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeCalendar));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeCalendar(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const credentials = new StaticCredentialProvider([[connection, { authMethodId: 'google-oauth', accessToken: new SecretString('calendar-token') }]]);
  const client = new CalendarClient({ endpoint: 'http://127.0.0.1' }, credentials, { send });
  const call = scenarioCall(scenario, connection, 'google-calendar', { flowId: 'calendar-flow', cancellationSignal: new AbortController().signal }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  switch (scenario.operation) {
    case 'listEvents': return client.listEvents().invoke(call, scenario.input as unknown as ListEventsInput);
    case 'getEvent': return client.getEvent().invoke(call, scenario.input as unknown as GetEventInput);
    case 'createEvent': return productionMutation(client.createEvent(), call, scenario.input as unknown as CreateEventInput, scenario.idempotencyKey!);
    case 'updateEvent': return productionMutation(client.updateEvent(), call, scenario.input as unknown as UpdateEventInput, scenario.idempotencyKey!);
    case 'queryFreeBusy': return client.queryFreeBusy().invoke(call, scenario.input as unknown as QueryFreeBusyInput);
    default: throw new Error(`unknown Calendar operation ${scenario.operation}`);
  }
}
