// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
import { it } from "vitest";
import {
  RetryError,
  runMutation,
  SecretString,
  StaticCredentialProvider,
  type Call,
  type CredentialRefreshDriver,
  type Mutation,
} from "@superdurable/connectors-sdk";
import {
  ProviderTransportError,
  type ProviderRequest,
} from "@superdurable/connectors-sdk/providerhttp";
import { Client } from "../src/client.js";
import type { Credentials } from "../src/generated/models.js";
import {
  answerRecordedRequest,
  ExchangeRecorder,
  HeartbeatContext,
  invokeProductionMutation,
  loadScenarios,
  replay,
  scenarioCall,
  type Scenario,
} from "../../../../../conformance/runners/typescript/connector_scenarios.js";
const scenarios = loadScenarios(
  new URL(
    "../../../../../conformance/connectors/microsoft/outlook-calendar/scenarios.json",
    import.meta.url,
  ),
);
const connection = {
  provider: "microsoft",
  name: "outlook-calendar-scheduler",
};
const credentials: Credentials = {
  authMethodId: "microsoft-oauth",
  clientId: "fixture-client",
  clientSecret: new SecretString("fixture-secret"),
  accessToken: new SecretString("calendar-token"),
  refreshToken: new SecretString("fixture-refresh"),
};
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
it.each(scenarios)("matches Go conformance: $name", (scenario) =>
  replay(scenario, invokeAttempt, "/v1.0"),
);
async function invokeAttempt(
  scenario: Scenario,
  recorder: ExchangeRecorder,
  context: HeartbeatContext,
) {
  const driver: CredentialRefreshDriver<Credentials> = {
    refreshRequired: () => false,
    refresh: async () => {
      recorder.mismatches.push("unexpected refresh");
      throw new Error("unexpected refresh");
    },
  };
  const transport = (request: ProviderRequest) =>
    answerRecordedRequest(
      recorder,
      request,
      (before) => new ProviderTransportError(before),
    );
  const client = new Client(
    {},
    new StaticCredentialProvider([[connection, credentials]]),
    {
      refreshDriver: driver,
      transport,
      now: () => new Date("2026-03-04T09:15:00Z"),
      baseURL: "http://127.0.0.1:12345/v1.0",
    },
  );
  const call = scenarioCall(
    scenario,
    connection,
    "outlook-calendar",
    context,
  ) as Call;
  const input = scenario.input;
  if (scenario.operation === "listEvents")
    return client.listEvents().invoke(call, input as never);
  if (scenario.operation === "getEvent")
    return client.getEvent().invoke(call, input as never);
  if (scenario.operation === "queryFreeBusy")
    return client.queryFreeBusy().invoke(call, input as never);
  if (scenario.operation === "createEvent")
    return productionMutation(
      client.createEvent(),
      call,
      input as never,
      scenario.idempotencyKey!,
    );
  if (scenario.operation === "updateEvent")
    return productionMutation(
      client.updateEvent(),
      call,
      input as never,
      scenario.idempotencyKey!,
    );
  throw new Error(`unknown operation ${scenario.operation}`);
}
