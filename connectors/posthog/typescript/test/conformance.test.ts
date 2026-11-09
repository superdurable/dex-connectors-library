// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { PostHogClient, CaptureEventInputSchema, ListInsightsInputSchema } from '../src/index.js';
import {
  answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall,
  type Scenario,
} from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/posthog/scenarios.json', import.meta.url));
const connection = { provider: 'posthog', name: 'posthog' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokePostHog));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokePostHog(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new PostHogClient({ captureUrl: 'http://127.0.0.1:12345', appUrl: 'http://127.0.0.1:12345' },
    { resolve: async () => ({ projectToken: new SecretString('phc_synthetic-token'),
      personalApiKey: new SecretString('phx_synthetic-key') }) }, { send: sender });
  const call = scenarioCall(scenario, connection, 'posthog',
    { flowId: 'posthog-conformance', cancellationSignal: new AbortController().signal },
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'captureEvent') return productionMutation(
    client.captureEvent(), call, CaptureEventInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'listInsights') return client.listInsights().invoke(call, ListInsightsInputSchema.parse(scenario.input));
  throw new Error(`unknown PostHog operation ${scenario.operation}`);
}
