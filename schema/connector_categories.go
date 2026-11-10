// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package schema

import "strconv"

// ConnectorCategory is one entry of the fixed connector category taxonomy that manifests reference from
// metadata.categories and the public directory site uses for navigation.
type ConnectorCategory struct {
	// ID is the stable kebab-case identifier written in manifests and catalog URLs.
	ID string `yaml:"id" json:"id"`
	// DisplayName is the human-readable name shown by the directory site.
	DisplayName string `yaml:"name" json:"name"`
	// Description lists the kinds of connectors the category groups.
	Description string `yaml:"description" json:"description"`
}

// MinimumConnectorCategories and MaximumConnectorCategories bound the length of metadata.categories.
const (
	MinimumConnectorCategories = 1
	MaximumConnectorCategories = 2
)

// connectorCategories is the only taxonomy source: a test pins the JSON schema enum, and the catalog carries it.
var connectorCategories = []ConnectorCategory{
	{ID: "ai", DisplayName: "AI & Machine Learning", Description: "Model inference, embeddings, speech, and vector search."},
	{ID: "communication", DisplayName: "Communication", Description: "Team chat, email, SMS, messaging apps, and meetings."},
	{ID: "marketing", DisplayName: "Marketing", Description: "Email marketing, marketing automation, advertising, SEO, and websites."},
	{ID: "social-media", DisplayName: "Social Media", Description: "Social networks, publishing, and video platforms."},
	{ID: "crm-sales", DisplayName: "CRM & Sales", Description: "Contacts, leads, deals, and sales pipelines."},
	{ID: "customer-support", DisplayName: "Customer Support", Description: "Help desks, shared inboxes, tickets, and support conversations."},
	{ID: "productivity", DisplayName: "Productivity & Collaboration", Description: "Docs, notes, calendars, spreadsheets, tasks, forms, and whiteboards."},
	{ID: "project-management", DisplayName: "Project Management", Description: "Projects, issues, boards, and work tracking."},
	{ID: "files-documents", DisplayName: "Files, Documents & E-signature", Description: "File storage and sharing, documents, and electronic signatures."},
	{ID: "developer-tools", DisplayName: "Developer Tools & DevOps", Description: "Code hosting, cloud infrastructure, deployment, monitoring, queues, and HTTP APIs."},
	{ID: "databases-storage", DisplayName: "Databases & Storage", Description: "SQL and NoSQL databases, data warehouses, search indexes, and object storage."},
	{ID: "analytics", DisplayName: "Analytics & BI", Description: "Product analytics, web analytics, event tracking, and business intelligence."},
	{ID: "finance-payments", DisplayName: "Finance, Payments & Accounting", Description: "Payments, billing, subscriptions, invoicing, accounting, and banking."},
	{ID: "ecommerce", DisplayName: "E-commerce", Description: "Online stores, orders, and commerce platforms."},
	{ID: "hr-recruiting", DisplayName: "HR & Recruiting", Description: "Employees, payroll, time off, and applicant tracking."},
	{ID: "security-identity", DisplayName: "Security & Identity", Description: "Identity providers, user directories, and access management."},
	{ID: "data-search", DisplayName: "Search & Data APIs", Description: "News, market data, weather, media catalogs, and other public data APIs."},
}

// ConnectorCategories returns the connector category taxonomy in directory display order. The caller owns the
// returned slice.
func ConnectorCategories() []ConnectorCategory {
	return append([]ConnectorCategory(nil), connectorCategories...)
}

// IsKnownConnectorCategory reports whether id names a category in the taxonomy.
func IsKnownConnectorCategory(id string) bool {
	for _, category := range connectorCategories {
		if category.ID == id {
			return true
		}
	}
	return false
}

func validateMetadataCategories(categories []string) []string {
	if len(categories) < MinimumConnectorCategories || len(categories) > MaximumConnectorCategories {
		return []string{"metadata.categories must list 1 or 2 category IDs; the first is the primary category"}
	}
	var problems []string
	seen := make(map[string]bool, len(categories))
	for _, category := range categories {
		if !IsKnownConnectorCategory(category) {
			problems = append(problems, "metadata.categories names unknown category "+strconv.Quote(category))
		} else if seen[category] {
			problems = append(problems, "metadata.categories has duplicate category "+strconv.Quote(category))
		}
		seen[category] = true
	}
	return problems
}
