// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export const rawRepositoryRoot: string;
export const connectorLibraryRepositoryUrl: string;

/** A language a connector release can list. */
export type CatalogLanguage = "go" | "python" | "typescript" | "java" | "rust";
/** Every CatalogLanguage in catalog display order, Go first. */
export const catalogLanguages: CatalogLanguage[];

/** Maven and Gradle dependency snippets of a Java package. */
export type JavaInstall = { gradle: string; maven: string };

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
  languages: Partial<Record<CatalogLanguage, string>>;
  pythonPackage: CatalogPythonPackage | null;
  /** The registry package name of each listed non-Go language; a Java package is its groupId:artifactId. */
  languagePackages: Partial<Record<Exclude<CatalogLanguage, "go">, string>>;
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
  languages: CatalogLanguage[];
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
export function languageConnectorCounts(connectors: CatalogConnector[]): Map<"all" | CatalogLanguage, number>;
export function pythonInstall(connector: CatalogConnector): PythonInstall | null;
export function javaInstall(connector: CatalogConnector): JavaInstall | null;
export function rustInstall(connector: CatalogConnector): string | null;
export function connectorMatchesQuery(connector: CatalogConnector, query: string): boolean;
export function connectorMatchesLanguage(connector: CatalogConnector, language: "all" | CatalogLanguage): boolean;
export function capabilityMatchesLanguage(capability: CatalogCapability, language: "all" | CatalogLanguage): boolean;
export function catalogTotals(connectors: CatalogConnector[]): CatalogTotals;
export function groupCatalog(connectors: CatalogConnector[]): CompanyGroup[];
export function readOperations(text: string): CatalogCapability[];
