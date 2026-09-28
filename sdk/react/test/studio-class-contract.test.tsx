// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
// @vitest-environment happy-dom

import { act, type ReactElement } from "react";
import { createRoot } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it } from "vitest";

import {
  ModelPicker,
  ModelPickerStudioApp,
  StudioButton,
  StudioField,
  StudioHeader,
  StudioNotice,
  StudioSurface,
  connectorStudioClassNames,
  connectorStudioHostAPIVersion,
  connectorStudioStyles,
  connectorStudioThemeTokenNames,
  isConnectorStudioStylesheet,
  type ConnectorStudioConfigurationUnitTarget,
  type ConnectorStudioHostReady,
  type ModelListing,
} from "../src/index.js";

(globalThis as {IS_REACT_ACT_ENVIRONMENT?: boolean}).IS_REACT_ACT_ENVIRONMENT = true;

interface StudioStyleRule {
  selectors: string[];
  declarations: string;
}

const contractClassNames = new Set<string>(connectorStudioClassNames);
const baseSelectors = new Set([":root", ":root[data-theme='dark']", "html", "body"]);

const modelPickerTarget = (unitId = "modelPicker"): ConnectorStudioConfigurationUnitTarget => ({
  kind: "configurationUnit",
  scope: {kind: "operation", operationId: "generateText", flowType: "Summarize", stepType: "GenerateSummary"},
  instanceId: "model", unitId, label: "Summary model", required: false,
  bindings: [{port: "model", jsonPointer: "/model"}], value: {model: "gemini-missing"},
});

const everyModelOptionShape: ModelListing = {
  isTruncated: true,
  models: [
    {id: "gemini-3.8-flash", label: "Gemini 3.8 Flash", detail: "1M tokens", badges: ["structured output"]},
    {id: "gemini-3.5-flash-lite"},
    {id: "text-embedding-005", label: "Text Embedding", isHiddenByDefault: true},
  ],
};

const hostReady = (target: ConnectorStudioHostReady["target"]): ConnectorStudioHostReady => ({
  type: "connector.host.ready",
  protocolVersion: connectorStudioHostAPIVersion,
  sessionNonce: "nonce",
  connectorId: "gemini",
  capabilities: [],
  connection: {state: "connected", grantedScopes: []},
  target,
});

const mountedContainers: {unmount(): void}[] = [];

afterEach(() => {
  for (const container of mountedContainers.splice(0)) container.unmount();
  document.getElementById("dex-connector-studio-theme")?.remove();
});

async function renderInDocument(element: ReactElement, afterRender?: () => void): Promise<HTMLElement> {
  const container = document.createElement("div");
  document.body.append(container);
  const reactRoot = createRoot(container);
  mountedContainers.push({unmount: () => { act(() => reactRoot.unmount()); container.remove(); }});
  await act(async () => { reactRoot.render(element); });
  if (afterRender) await act(async () => { afterRender(); });
  return container;
}

function classNamesInMarkup(markup: string): string[] {
  return [...markup.matchAll(/class="([^"]*)"/g)].flatMap(([, classes]) => classes.split(/\s+/).filter(Boolean));
}

function classNamesInDocument(container: HTMLElement): string[] {
  return [...container.querySelectorAll("[class]")].flatMap((element) => [...element.classList]);
}

function sendHostReady(ready: ConnectorStudioHostReady): void {
  window.dispatchEvent(new MessageEvent("message", {data: ready, source: window.parent}));
}

function parseStudioStyleRules(stylesheet: string): StudioStyleRule[] {
  const rules = [...stylesheet.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map(([, selectorList, declarations]) => ({
    selectors: selectorList.split(",").map((selector) => selector.trim()),
    declarations,
  }));
  expect(stylesheet.replace(/[^{}]+\{[^{}]*\}/g, "").trim(), "text outside flat rules").toBe("");
  return rules;
}

function selectorClassNames(selector: string): string[] {
  return [...selector.matchAll(/\.(-?[A-Za-z_][\w-]*)/g)].map(([, className]) => className);
}

describe("Studio class contract", () => {
  it("lists each class once, and every one is a studio-* class", () => {
    expect(contractClassNames.size).toBe(connectorStudioClassNames.length);
    for (const className of connectorStudioClassNames) expect(className).toMatch(/^studio-[a-z]+(?:-[a-z]+)*$/);
    expect(Object.isFrozen(connectorStudioClassNames)).toBe(true);
  });

  it("is exactly the set of classes the shared components render", async () => {
    const rendered = new Set<string>();
    const primitives = renderToStaticMarkup(<StudioSurface label="Surface">
      <StudioHeader description="Description" iconUrl="./icon.svg" title="Title"/>
      <StudioField hint="Hint" label="Field"><input type="text"/></StudioField>
      <StudioButton>Secondary</StudioButton>
      <StudioButton variant="primary">Primary</StudioButton>
      {(["info", "success", "error", "attention"] as const).map((tone) => <StudioNotice key={tone} tone={tone}>{tone}</StudioNotice>)}
    </StudioSurface>);
    for (const className of classNamesInMarkup(primitives)) rendered.add(className);

    const loadedPicker = await renderInDocument(<ModelPicker
      loadModels={async () => everyModelOptionShape} onSave={async () => ({})} providerName="Gemini" target={modelPickerTarget()}/>);
    const showAll = loadedPicker.querySelector<HTMLInputElement>(".studio-checkbox input");
    expect(showAll, "show-all checkbox").not.toBeNull();
    await act(async () => { showAll!.click(); });
    for (const className of classNamesInDocument(loadedPicker)) rendered.add(className);

    const failedPicker = await renderInDocument(<ModelPicker
      loadModels={async () => { throw new Error("offline"); }} onSave={async () => ({})} providerName="Gemini" target={modelPickerTarget()}/>);
    expect(failedPicker.textContent).toContain("could not be listed");
    for (const className of classNamesInDocument(failedPicker)) rendered.add(className);

    const loadModels = async () => everyModelOptionShape;
    for (const target of [{kind: "connection"} as const, modelPickerTarget("unknownUnit")]) {
      const bundle = await renderInDocument(
        <ModelPickerStudioApp connectorId="gemini" iconUrl="./icon.svg" loadModels={loadModels} providerName="Gemini"/>,
        () => sendHostReady(hostReady(target)));
      expect(bundle.textContent).not.toContain("Waiting for Dex Web");
      for (const className of classNamesInDocument(bundle)) rendered.add(className);
    }
    const waitingBundle = renderToStaticMarkup(<ModelPickerStudioApp connectorId="gemini" loadModels={loadModels} providerName="Gemini"/>);
    for (const className of classNamesInMarkup(waitingBundle)) rendered.add(className);

    expect([...rendered].filter((className) => !contractClassNames.has(className))).toEqual([]);
    expect(connectorStudioClassNames.filter((className) => !rendered.has(className)), "contract classes no component renders").toEqual([]);
  });

  it("installs the host stylesheet a ready message carries in a model picker bundle", async () => {
    const stylesheet = `${connectorStudioStyles}\n.studio-button { border-radius: 999px; }\n`;
    await renderInDocument(
      <ModelPickerStudioApp connectorId="gemini" loadModels={async () => ({models: []})} providerName="Gemini"/>,
      () => sendHostReady({...hostReady({kind: "connection"}), stylesheet}));
    expect(document.getElementById("dex-connector-studio-theme")?.textContent).toBe(stylesheet);
  });
});

describe("connectorStudioStyles", () => {
  const rules = parseStudioStyleRules(connectorStudioStyles);

  it("styles every contract class and no class outside the contract", () => {
    const styledClassNames = new Set(rules.flatMap((rule) => rule.selectors.flatMap(selectorClassNames)));
    expect(connectorStudioClassNames.filter((className) => !styledClassNames.has(className))).toEqual([]);
    expect([...styledClassNames].filter((className) => !contractClassNames.has(className))).toEqual([]);
  });

  it("follows the host stylesheet authoring rules", () => {
    expect(isConnectorStudioStylesheet(connectorStudioStyles)).toBe(true);
    for (const selector of rules.flatMap((rule) => rule.selectors)) {
      const leadingClassName = selector.match(/^\.(-?[A-Za-z_][\w-]*)/)?.[1];
      expect(baseSelectors.has(selector) || (leadingClassName !== undefined && contractClassNames.has(leadingClassName)), selector).toBe(true);
    }
    for (const forbidden of ["@", "url(", "image-set(", "expression(", "\\", "!important", "</"]) {
      expect(connectorStudioStyles.toLowerCase(), forbidden).not.toContain(forbidden);
    }
    const tokenNames = new Set<string>(connectorStudioThemeTokenNames);
    for (const [, name] of connectorStudioStyles.matchAll(/var\(\s*(--[\w-]+)/g)) expect(tokenNames.has(name), name).toBe(true);
    for (const rule of rules) {
      const declaresTokens = /(?:^|;)\s*--/.test(rule.declarations);
      if (declaresTokens) expect(rule.selectors.every((selector) => selector.startsWith(":root")), rule.selectors.join(", ")).toBe(true);
    }
  });
});
