// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { MailgunClient } from '../src/client.js';
import { GetEventsInputSchema, ListDomainsInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/mailgun/scenarios.json', import.meta.url));
const connection = { provider: 'mailgun', name: 'mailgun' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeMailgun));

async function invokeMailgun(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new MailgunClient({ baseUrl: 'http://127.0.0.1' }, {
    resolve: async () => ({ apiKey: new SecretString('test-key-123') }),
  }, { send });
  const call = scenarioCall(scenario, connection, 'mailgun', { flowId: 'mailgun-conformance', cancellationSignal: new AbortController().signal }) as Call;
  if (scenario.operation === 'getEvents') return client.getEvents().invoke(call, GetEventsInputSchema.parse(scenario.input));
  if (scenario.operation === 'listDomains') return client.listDomains().invoke(call, ListDomainsInputSchema.parse(scenario.input));
  throw new Error(`unknown Mailgun operation ${scenario.operation}`);
}
