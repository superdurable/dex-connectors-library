// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { WebhookClient } from '../src/client.js';
import { SendEventInputSchema, type SendEventInput, type SendEventOutput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/superdurable/webhook/scenarios.json', import.meta.url));
const connection = { provider: 'webhook', name: 'forms' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWebhook));

async function invokeWebhook(scenario: Scenario, recorder: ExchangeRecorder) {
  if (scenario.operation !== 'sendEvent') throw new Error(`unknown webhook operation ${scenario.operation}`);
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new WebhookClient({ deliveryUrl: scenario.name === 'defect-no-delivery-url' ? '' : 'https://receiver.example/events' }, {
    resolve: async () => ({ signingSecret: new SecretString('SENTINEL-SIGNING-SECRET') }),
  }, { send: sender, now: () => new Date('2026-09-30T12:00:00Z') });
  const call = scenarioCall(scenario, connection, 'webhook', {
    flowId: 'webhook-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  const operation = client.sendEvent();
  return invokeProductionMutation(operation, scenario.idempotencyKey!, recording =>
    runMutation(recording as Mutation<SendEventInput, SendEventOutput>, call, SendEventInputSchema.parse(scenario.input)),
  error => error instanceof RetryError);
}
