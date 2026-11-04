// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { CalendlyClient } from '../src/client.js';
import { ListScheduledEventsInputSchema, GetScheduledEventInputSchema, ListEventInviteesInputSchema, CancelScheduledEventInputSchema, CreateSchedulingLinkInputSchema, CreateWebhookSubscriptionInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/calendly/scenarios.json', import.meta.url));
const connection = { provider: 'calendly', name: 'scheduling' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeCalendly));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeCalendly(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const send = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const credentials = new StaticCredentialProvider([[connection, { authMethodId: 'personal-access-token', accessToken: new SecretString('SENTINEL-CALENDLY-ACCESS-TOKEN'), webhookSigningKey: new SecretString('SENTINEL-CALENDLY-SIGNING-KEY') }]]);
  const client = new CalendlyClient({}, credentials, { send, now: () => new Date('2026-09-30T12:00:00Z') });
  const call = scenarioCall(scenario, connection, 'calendly', context) as Call;
  if (scenario.operation === 'listScheduledEvents') return client.listScheduledEvents().invoke(call, ListScheduledEventsInputSchema.parse(scenario.input));
  if (scenario.operation === 'getScheduledEvent') return client.getScheduledEvent().invoke(call, GetScheduledEventInputSchema.parse(scenario.input));
  if (scenario.operation === 'listEventInvitees') return client.listEventInvitees().invoke(call, ListEventInviteesInputSchema.parse(scenario.input));
  if (scenario.operation === 'cancelScheduledEvent') return productionMutation(client.cancelScheduledEvent(), call, CancelScheduledEventInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'createSchedulingLink') return productionMutation(client.createSchedulingLink(), call, CreateSchedulingLinkInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'createWebhookSubscription') return productionMutation(client.createWebhookSubscription(), call, CreateWebhookSubscriptionInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown Calendly operation ${scenario.operation}`);
}
