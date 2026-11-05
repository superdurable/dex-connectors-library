// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { CalendarClient } from '../src/client.js';
import type { CreateEventInput, GetEventInput, ListEventsInput, QueryFreeBusyInput, UpdateEventInput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/google/calendar/scenarios.json', import.meta.url));
const connection = { provider: 'google', name: 'google-calendar-test' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeCalendar));

async function invokeCalendar(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const credentials = new StaticCredentialProvider([[connection, { authMethodId: 'google-oauth', accessToken: new SecretString('calendar-token') }]]);
  const client = new CalendarClient({ endpoint: 'http://127.0.0.1' }, credentials, { send });
  const call = scenarioCall(scenario, connection, 'google-calendar', { flowId: 'calendar-flow', cancellationSignal: new AbortController().signal }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  switch (scenario.operation) {
    case 'listEvents': return client.listEvents().invoke(call, scenario.input as unknown as ListEventsInput);
    case 'getEvent': return client.getEvent().invoke(call, scenario.input as unknown as GetEventInput);
    case 'createEvent': return client.createEvent().invoke(call, scenario.input as unknown as CreateEventInput);
    case 'updateEvent': return client.updateEvent().invoke(call, scenario.input as unknown as UpdateEventInput);
    case 'queryFreeBusy': return client.queryFreeBusy().invoke(call, scenario.input as unknown as QueryFreeBusyInput);
    default: throw new Error(`unknown Calendar operation ${scenario.operation}`);
  }
}
