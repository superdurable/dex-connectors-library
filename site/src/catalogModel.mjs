// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { parse } from "yaml";

export const rawRepositoryRoot =
  "https://raw.githubusercontent.com/superdurable/dex-connectors-library";

export function companyDirectory(directory) {
  if (!directory.startsWith("connectors/")) {
    throw new Error(`connector directory must start with connectors/: ${directory}`);
  }
  const company = directory.slice("connectors/".length).split("/")[0];
  if (!company || company === "." || company === "..") {
    throw new Error(`connector directory is missing a company: ${directory}`);
  }
  return company;
}

export function connectorManifestUrl(directory, version) {
  return `${rawRepositoryRoot}/${directory}/${version}/${directory}/connector.yaml`;
}

export function companyLogoUrl(company, origin) {
  if (origin === "local") {
    return `/connectors/${company}/logo.svg`;
  }
  return `${rawRepositoryRoot}/main/connectors/${company}/logo.svg`;
}

export function catalogDocumentUrl(isDev, baseUrl) {
  if (isDev) {
    return "/catalog.yaml";
  }
  const base = baseUrl.endsWith("/") ? baseUrl : `${baseUrl}/`;
  return `${base}catalog.yaml`;
}

export function readCatalog(text) {
  const document = parse(text);
  if (!document || !Array.isArray(document.connectors)) {
    throw new Error("catalog is missing connectors");
  }
  return document.connectors.map((connector) => {
    const directory = requiredText(connector.directory, "directory");
    return {
      company: requiredText(connector.company, "company"),
      id: requiredText(connector.id, "id"),
      name: requiredText(connector.name, "name"),
      description: requiredText(connector.description, "description"),
      version: requiredText(connector.version, "version"),
      languages: readLanguages(connector.languages, connector.version),
      directory,
      companyDirectory: companyDirectory(directory),
      uiUnits: readCapabilities(connector.uiUnits, "ui-unit"),
      triggers: readCapabilities(connector.triggers, "trigger"),
      operations: readCapabilities(connector.operations, ""),
    };
  });
}

function readLanguages(value, goVersion) {
  const languages = value ?? { go: goVersion };
  if (typeof languages !== "object" || Array.isArray(languages)) {
    throw new Error("catalog languages must be an object");
  }
  for (const [language, version] of Object.entries(languages)) {
    if (!["go", "python", "typescript"].includes(language) || !/^v\d+\.\d+\.\d+$/.test(version)) {
      throw new Error(`invalid catalog language: ${language}`);
    }
  }
  return languages;
}

export function connectorMatchesQuery(connector, query) {
  const tokens = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  if (tokens.length === 0) {
    return true;
  }
  const haystack = [
    connector.company,
    connector.name,
    connector.description,
    connector.id,
    ...connector.uiUnits.flatMap((capability) => [capability.name, capability.description, capability.kind]),
    ...connector.triggers.flatMap((capability) => [capability.name, capability.description, capability.kind]),
    ...connector.operations.flatMap((capability) => [capability.name, capability.description, capability.kind]),
  ]
    .join("\n")
    .toLowerCase();
  return tokens.every((token) => haystack.includes(token));
}

export function connectorMatchesLanguage(connector, language) {
  return language === "all" || Object.hasOwn(connector.languages, language);
}

export function catalogTotals(connectors) {
  const companies = new Set();
  let uiUnits = 0;
  let triggers = 0;
  let operations = 0;
  for (const connector of connectors) {
    companies.add(connector.company);
    uiUnits += connector.uiUnits.length;
    triggers += connector.triggers.length;
    operations += connector.operations.length;
  }
  return {
    companies: companies.size,
    connectors: connectors.length,
    uiUnits,
    triggers,
    operations,
  };
}

export function groupCatalog(connectors) {
  const groups = [];
  const byCompany = new Map();
  for (const connector of connectors) {
    let group = byCompany.get(connector.company);
    if (!group) {
      group = {
        company: connector.company,
        companyDirectory: connector.companyDirectory,
        connectors: [],
      };
      byCompany.set(connector.company, group);
      groups.push(group);
    }
    group.connectors.push(connector);
  }
  return groups;
}

export function readOperations(text) {
  const document = parse(text);
  const spec = document?.spec ?? {};
  const triggers = Array.isArray(spec.triggers) ? spec.triggers : [];
  const operations = Array.isArray(spec.operations) ? spec.operations : [];
  const listed = [
    ...triggers.map((operation) => ({ ...operation, kind: operation.kind || "trigger" })),
    ...operations,
  ];
  if (listed.length === 0) {
    throw new Error("connector manifest is missing operations");
  }
  return listed.map((operation) => ({
    name: requiredText(operation.name, "operation name"),
    kind: requiredText(operation.kind, "operation kind"),
    description: requiredText(operation.description, "operation description"),
    languages: ["go", ...["python", "typescript"].filter((language) =>
      spec.languages?.[language]?.[operation.kind === "trigger" ? "triggers" : "operations"]?.includes(operation.name))],
  }));
}

function readCapabilities(value, fixedKind) {
  if (value == null) {
    return [];
  }
  if (!Array.isArray(value)) {
    throw new Error("catalog capabilities must be a list");
  }
  return value.map((capability) => ({
    name: requiredText(capability.name, "capability name"),
    kind: fixedKind || requiredText(capability.kind, "operation kind"),
    description: requiredText(capability.description, "capability description"),
    languages: readCapabilityLanguages(capability.languages),
  }));
}

function readCapabilityLanguages(value) {
  if (value == null) {
    return ["go"];
  }
  if (!Array.isArray(value) || value.length === 0 || value[0] !== "go" ||
      value.some((language) => !["go", "python", "typescript"].includes(language)) ||
      new Set(value).size !== value.length ||
      value.join(",") !== ["go", "python", "typescript"].filter((language) => value.includes(language)).join(",")) {
    throw new Error("catalog capability languages must be sorted, unique, and include go");
  }
  return value;
}

function requiredText(value, label) {
  if (typeof value !== "string" || value.trim() === "") {
    throw new Error(`catalog entry is missing ${label}`);
  }
  return value;
}
