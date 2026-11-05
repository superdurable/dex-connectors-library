// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { MixpanelClient, TrackEventInputSchema, ExportEventsInputSchema } from '../src/index.js';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/mixpanel/scenarios.json', import.meta.url));
const connection = { provider: 'mixpanel', name: 'mixpanel' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeMixpanel));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeMixpanel(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new MixpanelClient({ baseUrl: 'http://127.0.0.1:12345', exportBaseUrl: 'http://127.0.0.1:12345/api/2.0', projectId: 12345 },
    { resolve: async () => ({ serviceAccountUsername: new SecretString('service-user'), serviceAccountSecret: new SecretString('service-secret') }) },
    { send: sender });
  const call = scenarioCall(scenario, connection, 'mixpanel',
    { flowId: 'mixpanel-conformance', cancellationSignal: new AbortController().signal },
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'trackEvent') return productionMutation(client.trackEvent(), call, TrackEventInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'exportEvents') return client.exportEvents().invoke(call, ExportEventsInputSchema.parse(scenario.input));
  throw new Error(`unknown Mixpanel operation ${scenario.operation}`);
}
