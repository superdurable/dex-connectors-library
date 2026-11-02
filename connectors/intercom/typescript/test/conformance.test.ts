// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from "vitest";
import {
  RetryError,
  runMutation,
  SecretString,
  type Call,
  type Mutation,
} from "@superdurable/connectors-sdk";
import {
  ProviderTransportError,
  type ProviderRequest,
} from "@superdurable/connectors-sdk/providerhttp";
import {
  answerRecordedRequest,
  ExchangeRecorder,
  invokeProductionMutation,
  loadScenarios,
  replay,
  scenarioCall,
  type HeartbeatContext,
  type Scenario,
} from "../../../../conformance/runners/typescript/connector_scenarios.js";
import { IntercomClient } from "../src/client.js";
import {
  SearchConversationsInputSchema,
  GetConversationInputSchema,
  ReplyToConversationInputSchema,
  UpdateConversationStateInputSchema,
  FindContactByEmailInputSchema,
} from "../src/generated/models.js";

const scenarios = loadScenarios(
  new URL(
    "../../../../conformance/connectors/intercom/scenarios.json",
    import.meta.url,
  ),
);
const progress = {
  reportProgress() {},
  writeText() {},
  hasProgressStream: () => false,
  hasTextStream: () => false,
};

it.each(scenarios)("matches Go conformance: $name", (scenario) =>
  replay(scenario, invokeIntercom),
);

function productionMutation<Input, Output>(
  operation: Mutation<Input, Output>,
  call: Call,
  input: Input,
  key: string,
) {
  return invokeProductionMutation(
    operation,
    key,
    (recording) =>
      runMutation(recording as Mutation<Input, Output>, call, input),
    (error) => error instanceof RetryError,
  );
}

async function invokeIntercom(
  scenario: Scenario,
  recorder: ExchangeRecorder,
  context: HeartbeatContext,
) {
  const sender = (request: ProviderRequest) =>
    answerRecordedRequest(
      recorder,
      request,
      (before) => new ProviderTransportError(before),
    );
  const client = new IntercomClient(
    {},
    {
      resolve: async () => ({
        accessToken: new SecretString(
          "dG9rOjBiMjY3intercomTestToken0123456789",
        ),
      }),
    },
    {
      endpoint: "http://127.0.0.1:0",
      send: sender,
      now: () => new Date("2026-10-30T00:00:00Z"),
    },
  );
  const call = scenarioCall(
    scenario,
    { provider: "intercom", name: "intercom-test" },
    "intercom",
    context,
    progress,
  ) as Call;
  if (scenario.operation === "searchConversations")
    return client
      .searchConversations()
      .invoke(call, SearchConversationsInputSchema.parse(scenario.input));
  if (scenario.operation === "getConversation")
    return client
      .getConversation()
      .invoke(call, GetConversationInputSchema.parse(scenario.input));
  if (scenario.operation === "replyToConversation")
    return productionMutation(
      client.replyToConversation(),
      call,
      ReplyToConversationInputSchema.parse(scenario.input),
      scenario.idempotencyKey!,
    );
  if (scenario.operation === "updateConversationState")
    return productionMutation(
      client.updateConversationState(),
      call,
      UpdateConversationStateInputSchema.parse(scenario.input),
      scenario.idempotencyKey!,
    );
  if (scenario.operation === "findContactByEmail")
    return client
      .findContactByEmail()
      .invoke(call, FindContactByEmailInputSchema.parse(scenario.input));
  throw new Error(`unknown Intercom operation ${scenario.operation}`);
}
