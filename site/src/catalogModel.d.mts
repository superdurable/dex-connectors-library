// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export const rawRepositoryRoot: string;
export const connectorLibraryRepositoryUrl: string;

export type CatalogPythonPackage = {
  name: string;
  tag: string;
  isPyPIPublished: boolean;
};

export type PythonInstall =
  | { isPyPIPublished: true; command: string }
  | { isPyPIPublished: false; command: string; releaseUrl: string };

export type CatalogCategory = {
  id: string;
  name: string;
  description: string;
  /** Accent hue in degrees, derived from the category's position in the catalog taxonomy. */
  hue: number;
};

export type CatalogDocument = {
  categories: CatalogCategory[];
  connectors: CatalogConnector[];
};

export type CatalogConnector = {
  company: string;
  id: string;
  name: string;
  description: string;
  /** The connector's categories, primary first; empty when the catalog predates categories. */
  categories: CatalogCategory[];
  version: string;
  languages: Partial<Record<"go" | "python" | "typescript", string>>;
  pythonPackage: CatalogPythonPackage | null;
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
export const allCategories: "all";
export const categorySearchParameter: "category";
export function readCatalogDocument(text: string): CatalogDocument;
export function readCatalog(text: string): CatalogConnector[];
export function selectedCategoryID(parameterValue: string | null, categories: CatalogCategory[]): string;
export function connectorMatchesCategory(connector: CatalogConnector, categoryID: string): boolean;
export function categoryConnectorCounts(categories: CatalogCategory[], connectors: CatalogConnector[]): Map<string, number>;
export function pythonInstall(connector: CatalogConnector): PythonInstall | null;
export function connectorMatchesQuery(connector: CatalogConnector, query: string): boolean;
export function connectorMatchesLanguage(connector: CatalogConnector, language: "all" | "go" | "python" | "typescript"): boolean;
export function capabilityMatchesLanguage(capability: CatalogCapability, language: "all" | "go" | "python" | "typescript"): boolean;
export function catalogTotals(connectors: CatalogConnector[]): CatalogTotals;
export function groupCatalog(connectors: CatalogConnector[]): CompanyGroup[];
export function readOperations(text: string): CatalogCapability[];
