// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { MailchimpClient } from '../src/client.js';
import { GetMemberInputSchema, UpsertMemberInputSchema, UpdateMemberTagsInputSchema, SendCampaignInputSchema, type UpsertMemberInput, type UpdateMemberTagsInput, type SendCampaignInput } from '../src/generated/models.js';
import { ListMembersInputSchema } from '../src/schema.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/intuit/mailchimp/scenarios.json', import.meta.url));
const connection = { provider: 'mailchimp', name: 'mailchimp-test' };

it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeMailchimp, '/3.0'));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}
async function invokeMailchimp(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new MailchimpClient({}, { resolve: async () => ({ apiKey: new SecretString('TESTKEY1-us6') }) },
    { apiBaseURL: 'http://127.0.0.1:12345/3.0', send: sender });
  const call = scenarioCall(scenario, connection, 'mailchimp', context,
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'getMember') return client.getMember().invoke(call, GetMemberInputSchema.parse(scenario.input));
  if (scenario.operation === 'listMembers') return client.listMembers().invoke(call, ListMembersInputSchema.parse(scenario.input));
  if (scenario.operation === 'upsertMember') return productionMutation(client.upsertMember(), call, UpsertMemberInputSchema.parse(scenario.input) as UpsertMemberInput, scenario.idempotencyKey!);
  if (scenario.operation === 'updateMemberTags') return productionMutation(client.updateMemberTags(), call, UpdateMemberTagsInputSchema.parse(scenario.input) as UpdateMemberTagsInput, scenario.idempotencyKey!);
  if (scenario.operation === 'sendCampaign') return productionMutation(client.sendCampaign(), call, SendCampaignInputSchema.parse(scenario.input) as SendCampaignInput, scenario.idempotencyKey!);
  throw new Error(`unknown Mailchimp operation ${scenario.operation}`);
}
