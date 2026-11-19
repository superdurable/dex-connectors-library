// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { SendGridClient } from '../src/client.js';
import { GetStatsInputSchema, ListTemplatesInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, ExchangeRecorder, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/sendgrid/scenarios.json', import.meta.url));
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSendGrid));

async function invokeSendGrid(scenario: Scenario, recorder: ExchangeRecorder) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new SendGridClient({}, {
    resolve: async () => ({ apiKey: new SecretString('test-key-123') }),
  }, { send });
  const call = scenarioCall(scenario, { provider: 'sendgrid', name: 'sendgrid' }, 'sendgrid', { flowId: 'sendgrid-conformance' }, {
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  }) as Call;
  if (scenario.operation === 'getStats') return client.getStats().invoke(call, GetStatsInputSchema.parse(scenario.input));
  if (scenario.operation === 'listTemplates') return client.listTemplates().invoke(call, ListTemplatesInputSchema.parse(scenario.input));
  throw new Error(`unknown operation ${scenario.operation}`);
}
