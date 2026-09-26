import { parse } from "yaml";

export function companyDirectory(directory) {
  const segments = directory.split("/");
  if (segments.length < 2 || segments[0] !== "connectors" || segments[1] === "") {
    throw new Error(`connector directory must start with a company folder: ${directory}`);
  }
  return segments[1];
}

export function connectorManifestUrl(connector) {
  const tag = `${connector.directory}/${connector.version}`;
  return `https://raw.githubusercontent.com/superdurable/dex-connectors-library/${tag}/${connector.directory}/connector.yaml`;
}

export function companyLogoUrl(company, source) {
  const path = `connectors/${company}/logo.svg`;
  if (source === "local") {
    return `/${path}`;
  }
  return `https://raw.githubusercontent.com/superdurable/dex-connectors-library/main/${path}`;
}

export function catalogDocumentUrl() {
  if (import.meta.env.DEV) {
    return "/catalog.yaml";
  }
  return `${import.meta.env.BASE_URL}catalog.yaml`;
}

export function readCatalog(catalogText) {
  const document = parse(catalogText);
  const connectors = Array.isArray(document?.connectors) ? document.connectors : [];
  return connectors.map((connector) => ({
    company: String(connector.company ?? ""),
    id: String(connector.id ?? ""),
    name: String(connector.name ?? ""),
    description: String(connector.description ?? ""),
    version: String(connector.version ?? ""),
    directory: String(connector.directory ?? ""),
    triggers: readCapabilities(connector.triggers, "trigger"),
    operations: readCapabilities(connector.operations, ""),
  }));
}

export function readOperations(manifestText) {
  const document = parse(manifestText);
  return [
    ...readCapabilities(document?.spec?.triggers, "trigger"),
    ...readCapabilities(document?.spec?.operations, ""),
  ];
}

export function connectorMatchesQuery(connector, query) {
  const tokens = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (tokens.length === 0) {
    return true;
  }
  const haystack = [
    connector.company,
    connector.name,
    connector.description,
    connector.id,
    ...connector.triggers.flatMap(capabilityText),
    ...connector.operations.flatMap(capabilityText),
  ].join("\n").toLowerCase();
  return tokens.every((token) => haystack.includes(token));
}

export function catalogTotals(connectors) {
  return {
    companies: new Set(connectors.map((connector) => companyDirectory(connector.directory))).size,
    connectors: connectors.length,
    operations: connectors.reduce((total, connector) => total + connector.operations.length, 0),
    triggers: connectors.reduce((total, connector) => total + connector.triggers.length, 0),
  };
}

export function groupCatalog(connectors) {
  const groups = new Map();
  for (const connector of connectors) {
    const company = companyDirectory(connector.directory);
    const group = groups.get(company) ?? { companyDirectory: company, company: connector.company, connectors: [] };
    group.connectors.push(connector);
    groups.set(company, group);
  }
  return [...groups.values()].sort((left, right) => left.company.localeCompare(right.company));
}

function readCapabilities(values, defaultKind) {
  if (!Array.isArray(values)) {
    return [];
  }
  return values.map((value) => ({
    name: String(value?.name ?? ""),
    kind: String(value?.kind ?? defaultKind),
    description: String(value?.description ?? ""),
  }));
}

function capabilityText(capability) {
  return [capability.name, capability.kind, capability.description];
}
