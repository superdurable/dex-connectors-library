// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { savedChangeType } from "../src/change-types.js";
import { BambooHRSetupView } from "../src/setup.js";
import { BambooHRConfigurationUnit } from "../src/units.js";

const triggerScope = {kind: "trigger" as const, triggerName: "employeeChanged", bindingName: "employee-changes", flowType: "BambooHREmployeeChange"};

function changeTypeTarget(value: Record<string, unknown>): ConnectorStudioConfigurationUnitTarget {
  return {
    kind: "configurationUnit", scope: triggerScope, instanceId: "changeType", unitId: "changeTypePicker", label: "Employee changes",
    description: "Choose Inserted to start this Flow only for new hires.", required: false,
    bindings: [{port: "changeType", jsonPointer: "/changeType"}], value,
  };
}

function render(target: ConnectorStudioConfigurationUnitTarget) {
  return renderToStaticMarkup(<BambooHRConfigurationUnit onSave={() => undefined} target={target}/>);
}

describe("BambooHR configuration units", () => {
  it("renders every change type with its guidance and the saved choice", () => {
    const markup = render(changeTypeTarget({changeType: "updated"}));
    expect(markup).toContain("Employee changes");
    expect(markup).toContain("Choose Inserted to start this Flow only for new hires.");
    expect(markup.match(/type="radio"/g)).toHaveLength(4);
    for (const label of ["Every change", "Inserted", "Updated", "Deleted", "including terminations"]) {
      expect(markup).toContain(label);
    }
    expect(markup.match(/checked=""/g)).toHaveLength(1);
    expect(markup).toMatch(/type="radio" name="changeType" checked=""\/><span class="studio-option-label">Updated/);
    expect(markup).toContain("only each employee&#x27;s latest change");
    expect(markup).toContain("starts at the current time");
    expect(markup).not.toContain("disabled");
  });

  it("selects every change when nothing or an unknown value is saved", () => {
    for (const value of [{}, {changeType: "all"}, {changeType: 3}]) {
      expect(render(changeTypeTarget(value))).toMatch(/type="radio" name="changeType" checked=""\/><span class="studio-option-label">Every change/);
    }
    expect(savedChangeType("inserted")).toBe("inserted");
    expect(savedChangeType("Inserted")).toBe("");
  });

  it("reports an unknown unit instead of rendering a guess", () => {
    expect(render({...changeTypeTarget({}), unitId: "fieldPicker"})).toContain("Unsupported BambooHR configuration unit: fieldPicker");
  });
});

describe("BambooHR connection surface", () => {
  it("points credentials to the host form and never renders a credential field", () => {
    const markup = renderToStaticMarkup(<BambooHRSetupView connection={{state: "connected", grantedScopes: []}}/>);
    expect(markup).toContain("BambooHR API key");
    expect(markup).toContain("Connected.");
    expect(markup).not.toContain("<input");
    expect(renderToStaticMarkup(<BambooHRSetupView connection={{state: "not_configured", grantedScopes: []}}/>)).toContain("Not connected yet.");
  });
});
