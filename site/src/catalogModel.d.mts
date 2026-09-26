export const rawRepositoryRoot: string;

export type CatalogConnector = {
  company: string;
  id: string;
  name: string;
  description: string;
  version: string;
  directory: string;
  companyDirectory: string;
  triggers: CatalogOperation[];
  operations: CatalogOperation[];
};

export type CompanyGroup = {
  company: string;
  companyDirectory: string;
  connectors: CatalogConnector[];
};

export type CatalogOperation = {
  name: string;
  kind: string;
  description: string;
};

export type CatalogTotals = {
  companies: number;
  connectors: number;
  triggers: number;
  operations: number;
};

export function companyDirectory(directory: string): string;
export function connectorManifestUrl(directory: string, version: string): string;
export function companyLogoUrl(company: string, origin: "local" | "published"): string;
export function catalogDocumentUrl(isDev: boolean, baseUrl: string): string;
export function readCatalog(text: string): CatalogConnector[];
export function connectorMatchesQuery(connector: CatalogConnector, query: string): boolean;
export function catalogTotals(connectors: CatalogConnector[]): CatalogTotals;
export function groupCatalog(connectors: CatalogConnector[]): CompanyGroup[];
export function readOperations(text: string): CatalogOperation[];
