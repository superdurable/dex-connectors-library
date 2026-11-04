// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { PagerDutyClient } from '../src/client.js';
import { GetIncidentInputSchema, ListIncidentsInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/pagerduty/scenarios.json', import.meta.url));
const connection = { provider: 'pagerduty', name: 'pagerduty' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokePagerDuty));

async function invokePagerDuty(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new PagerDutyClient({ baseUrl: 'http://127.0.0.1:12345' }, {
    resolve: async () => ({ apiKey: new SecretString('test-secret-token') }),
  }, { send: sender });
  const call = scenarioCall(scenario, connection, 'pagerduty', { flowId: 'pagerduty-conformance', cancellationSignal: new AbortController().signal },
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'listIncidents') return client.listIncidents().invoke(call, ListIncidentsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getIncident') return client.getIncident().invoke(call, GetIncidentInputSchema.parse(scenario.input));
  throw new Error(`unknown PagerDuty operation ${scenario.operation}`);
}
