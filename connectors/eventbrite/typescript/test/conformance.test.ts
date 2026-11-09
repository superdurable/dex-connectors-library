// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { EventbriteClient } from '../src/client.js';
import { GetAttendeeInputSchema, ListEventsInputSchema, ListOrganizationsInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, loadScenarios, replay, type ExchangeRecorder, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/eventbrite/scenarios.json', import.meta.url));
const connection = { provider: 'eventbrite', name: 'eventbrite' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeEventbrite, '/v3'));

async function invokeEventbrite(scenario: Scenario, recorder: ExchangeRecorder) {
    const client = new EventbriteClient({ baseUrl: 'http://127.0.0.1:12345/v3' }, new StaticCredentialProvider([[connection, { privateToken: new SecretString('test-token') }]]), {
      send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
    });
    const call: Call = {
      context: { flowId: 'eventbrite-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
      id: scenario.callId, connection, operation: { connectorId: 'eventbrite', operationId: scenario.operation },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    };
    if (scenario.operation === 'listOrganizations') return client.listOrganizations().invoke(call, ListOrganizationsInputSchema.parse(scenario.input));
    if (scenario.operation === 'listEvents') return client.listEvents().invoke(call, ListEventsInputSchema.parse(scenario.input));
    if (scenario.operation === 'getAttendee') return client.getAttendee().invoke(call, GetAttendeeInputSchema.parse(scenario.input));
    throw new Error(`unknown Eventbrite operation ${scenario.operation}`);
}
