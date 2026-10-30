// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export const rawRepositoryRoot: string;

export type CatalogConnector = {
  company: string;
  id: string;
  name: string;
  description: string;
  version: string;
  languages: Partial<Record<"go" | "python" | "typescript", string>>;
  directory: string;
  companyDirectory: string;
  uiUnits: CatalogCapability[];
  triggers: CatalogCapability[];
  operations: CatalogCapability[];
};

export type CompanyGroup = {
  company: string;
  companyDirectory: string;
  connectors: CatalogConnector[];
};

export type CatalogCapability = {
  name: string;
  kind: string;
  description: string;
  languages: Array<"go" | "python" | "typescript">;
};

export type CatalogTotals = {
  companies: number;
  connectors: number;
  uiUnits: number;
  triggers: number;
  operations: number;
};

export function companyDirectory(directory: string): string;
export function connectorManifestUrl(directory: string, version: string): string;
export function companyLogoUrl(company: string, origin: "local" | "published"): string;
export function catalogDocumentUrl(isDev: boolean, baseUrl: string): string;
export function readCatalog(text: string): CatalogConnector[];
export function connectorMatchesQuery(connector: CatalogConnector, query: string): boolean;
export function connectorMatchesLanguage(connector: CatalogConnector, language: "all" | "go" | "python" | "typescript"): boolean;
export function capabilityMatchesLanguage(capability: CatalogCapability, language: "all" | "go" | "python" | "typescript"): boolean;
export function catalogTotals(connectors: CatalogConnector[]): CatalogTotals;
export function groupCatalog(connectors: CatalogConnector[]): CompanyGroup[];
export function readOperations(text: string): CatalogCapability[];
