// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { SNSSetupView } from "../src/setup.js";
import { topicARNFromInput } from "../src/topic.js";
import { SNSConfigurationUnit } from "../src/units.js";

const scope = {kind: "trigger" as const, triggerName: "notificationReceived", bindingName: "sns-notification", flowType: "AmazonSNSNotificationIntake"};
const topicARN = "arn:aws:sns:us-east-1:123456789012:example-topic";

function renderTopic(value: Record<string, unknown>) {
  return renderToStaticMarkup(<SNSConfigurationUnit target={{kind: "configurationUnit", scope, instanceId: "notificationTopic", unitId: "topicARNInput", label: "Notification topic", description: "Open https://console.aws.amazon.com/sns > Topics; blank cannot save this binding.", required: true, bindings: [{port: "topicARN", jsonPointer: "/topicARN"}], value}} onSave={() => undefined}/>);
}

describe("SNS configuration", () => {
  it("renders provider guidance and keeps credentials out of the iframe", () => {
    const markup = renderToStaticMarkup(<SNSSetupView connection={{state: "not_configured", grantedScopes: []}}/>);
    expect(markup).toContain("https://console.aws.amazon.com/iam");
    expect(markup).toContain("never receives the key or secret");
    expect(markup).not.toContain("SENTINEL-AWS-SECRET");
  });

  it("accepts a complete ARN and rejects a blank or malformed ARN", () => {
    expect(topicARNFromInput(topicARN)).toBe(topicARN);
    expect(topicARNFromInput("  " + topicARN + "  ")).toBe(topicARN);
    for (const invalid of ["", "example-topic", "arn:aws:sns:us-east-1:123:example-topic", "arn:aws:sqs:us-east-1:123456789012:queue"]) expect(topicARNFromInput(invalid)).toBeUndefined();
  });

  it("shows the Topic path, ARN shape, blank behavior, and validation", () => {
    const configured = renderTopic({topicARN});
    expect(configured).toContain("https://console.aws.amazon.com/sns");
    expect(configured).toContain("Topics");
    expect(configured).toContain("blank cannot save");
    expect(configured).toContain(topicARN);
    expect(renderTopic({})).toContain('disabled=""');
    expect(renderTopic({topicARN: "wrong"})).toContain("12-digit AWS account ID");
  });
});
