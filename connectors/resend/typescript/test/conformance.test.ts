// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { ResendClient } from '../src/client.js';
import { GetEmailInputSchema, SendEmailInputSchema, type SendEmailInput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/resend/scenarios.json', import.meta.url));
const noProgress = { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeResend));

async function invokeResend(scenario: Scenario, recorder: ExchangeRecorder) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new ResendClient({ baseUrl: 'http://127.0.0.1' }, {
    resolve: async () => ({ apiKey: new SecretString('test-secret-token') }),
  }, { send: sender });
  const call = scenarioCall(scenario, { provider: 'resend', name: 'resend' }, 'resend',
    { flowId: 'provider-test', cancellationSignal: new AbortController().signal }, noProgress) as Call;
  if (scenario.operation === 'sendEmail') {
    const operation = client.sendEmail() as Mutation<SendEmailInput, { id: string }>;
    return invokeProductionMutation(operation, scenario.idempotencyKey!,
      recording => runMutation(recording as Mutation<SendEmailInput, { id: string }>, call, SendEmailInputSchema.parse(scenario.input)),
      error => error instanceof RetryError);
  }
  if (scenario.operation === 'getEmail') return client.getEmail().invoke(call, GetEmailInputSchema.parse(scenario.input));
  throw new Error(`unknown Resend operation ${scenario.operation}`);
}
