// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { ZoomClient } from '../src/client.js';
import type { CreateMeetingInput, GetMeetingInput, ListMeetingsInput, ListPastMeetingParticipantsInput, UpdateMeetingInput } from '../src/generated/models.js';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/zoom/scenarios.json', import.meta.url));
const noProgress = { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false };

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key, recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeZoom, '/v2'));

async function invokeZoom(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const credentials = { accessToken: new SecretString('zoom-test-access-token'), oauthClientId: 'client-id',
    oauthClientSecret: new SecretString('client-secret'), refreshToken: new SecretString('refresh-token') };
  const source = { resolve: async () => credentials, resolveWithRefresh: async () => credentials };
  const client = new ZoomClient({ endpoint: 'http://127.0.0.1:12345/v2' }, source, { send: sender });
  const call = scenarioCall(scenario, { provider: 'zoom', name: 'zoom-test' }, 'zoom', context, noProgress) as Call;
  if (scenario.operation === 'listMeetings') return client.listMeetings().invoke(call, scenario.input as ListMeetingsInput);
  if (scenario.operation === 'getMeeting') return client.getMeeting().invoke(call, scenario.input as unknown as GetMeetingInput);
  if (scenario.operation === 'createMeeting') return productionMutation(client.createMeeting(), call, scenario.input as unknown as CreateMeetingInput, scenario.idempotencyKey!);
  if (scenario.operation === 'updateMeeting') return productionMutation(client.updateMeeting(), call, scenario.input as unknown as UpdateMeetingInput, scenario.idempotencyKey!);
  if (scenario.operation === 'listPastMeetingParticipants') return client.listPastMeetingParticipants().invoke(call, scenario.input as unknown as ListPastMeetingParticipantsInput);
  throw new TypeError(`unknown Zoom operation ${scenario.operation}`);
}
