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

export function catalogDocumentUrl(isDev, baseUrl, version = "") {
  if (isDev) {
    return "/catalog.yaml";
  }
  const base = baseUrl.endsWith("/") ? baseUrl : `${baseUrl}/`;
  const versionQuery = version ? `?v=${encodeURIComponent(version)}` : "";
  return `${base}catalog.yaml${versionQuery}`;
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
      directory,
      companyDirectory: companyDirectory(directory),
      uiUnits: readCapabilities(connector.uiUnits, "ui-unit"),
      triggers: readCapabilities(connector.triggers, "trigger"),
      operations: readCapabilities(connector.operations, ""),
    };
  });
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
  }));
}

function requiredText(value, label) {
  if (typeof value !== "string" || value.trim() === "") {
    throw new Error(`catalog entry is missing ${label}`);
  }
  return value;
}
