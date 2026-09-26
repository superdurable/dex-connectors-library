export type CatalogCapability = {
  name: string;
  kind: string;
  description: string;
};

export type CatalogConnector = {
  company: string;
  id: string;
  name: string;
  description: string;
  version: string;
  directory: string;
  triggers: CatalogCapability[];
  operations: CatalogCapability[];
};

export type CatalogGroup = {
  companyDirectory: string;
  company: string;
  connectors: CatalogConnector[];
};

export type CatalogTotals = {
  companies: number;
  connectors: number;
  operations: number;
  triggers: number;
};

export function companyDirectory(directory: string): string;
export function connectorManifestUrl(connector: CatalogConnector): string;
export function companyLogoUrl(company: string, source: "local" | "published"): string;
export function catalogDocumentUrl(): string;
export function readCatalog(catalogText: string): CatalogConnector[];
export function readOperations(manifestText: string): CatalogCapability[];
export function connectorMatchesQuery(connector: CatalogConnector, query: string): boolean;
export function catalogTotals(connectors: CatalogConnector[]): CatalogTotals;
export function groupCatalog(connectors: CatalogConnector[]): CatalogGroup[];
