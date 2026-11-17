// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tagmanager

import (
	"encoding/json"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ResourcePathInput selects a resource path returned by a Tag Manager list operation.
type ResourcePathInput struct {
	// Path is an API relative path, such as accounts/123456.
	Path string `json:"path"`
}

// ListChildResourcesInput selects one page of resources below a container or workspace.
type ListChildResourcesInput struct {
	// ParentPath is the API relative path returned by a parent list operation.
	ParentPath string `json:"parentPath"`
	// PageToken is Google's opaque next-page cursor; blank selects the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// TagManagerResource projects only a resource's identity and display name.
type TagManagerResource struct {
	// Path is Google's stable API relative resource path.
	Path string `json:"path"`
	// Name is the display name in Tag Manager.
	Name string `json:"name"`
}

// TagManagerResourcesPage is a bounded page of projected resource identities.
type TagManagerResourcesPage struct {
	// Resources are resource paths and names, without configuration or secrets.
	Resources []TagManagerResource `json:"resources"`
	// NextPageToken is blank on the final page.
	NextPageToken string `json:"nextPageToken"`
}

// GetAccountOperation reads one account.
type GetAccountOperation struct{ client *Client }

// GetContainerOperation reads one container.
type GetContainerOperation struct{ client *Client }

// ListWorkspacesOperation reads one workspace page.
type ListWorkspacesOperation struct{ client *Client }

// GetWorkspaceOperation reads one workspace.
type GetWorkspaceOperation struct{ client *Client }

// GetAccount binds the account lookup Query.
func (client *Client) GetAccount() GetAccountOperation { return GetAccountOperation{client} }

// GetContainer binds the container lookup Query.
func (client *Client) GetContainer() GetContainerOperation { return GetContainerOperation{client} }

// ListWorkspaces binds the workspace list Query.
func (client *Client) ListWorkspaces() ListWorkspacesOperation {
	return ListWorkspacesOperation{client}
}

// GetWorkspace binds the workspace lookup Query.
func (client *Client) GetWorkspace() GetWorkspaceOperation { return GetWorkspaceOperation{client} }

// Definition returns the generated account lookup contract.
func (GetAccountOperation) Definition() sdkgo.QueryDefinition { return GetAccountDefinition }

// Definition returns the generated container lookup contract.
func (GetContainerOperation) Definition() sdkgo.QueryDefinition { return GetContainerDefinition }

// Definition returns the generated workspace list contract.
func (ListWorkspacesOperation) Definition() sdkgo.QueryDefinition { return ListWorkspacesDefinition }

// Definition returns the generated workspace lookup contract.
func (GetWorkspaceOperation) Definition() sdkgo.QueryDefinition { return GetWorkspaceDefinition }

// Invoke reads an account without returning private account settings.
func (operation GetAccountOperation) Invoke(call sdkgo.Call, input ResourcePathInput) sdkgo.QueryAttempt[TagManagerResource] {
	return operation.client.getResource(call, input.Path, "accounts", "getAccount")
}

// Invoke reads a container without returning private container settings.
func (operation GetContainerOperation) Invoke(call sdkgo.Call, input ResourcePathInput) sdkgo.QueryAttempt[TagManagerResource] {
	return operation.client.getResource(call, input.Path, "accounts/*/containers", "getContainer")
}

// Invoke lists workspace identities in one container.
func (operation ListWorkspacesOperation) Invoke(call sdkgo.Call, input ListChildResourcesInput) sdkgo.QueryAttempt[TagManagerResourcesPage] {
	return operation.client.listResources(call, input, "accounts/*/containers", "workspaces", "workspace", "listWorkspaces")
}

// Invoke reads one workspace without returning its configuration.
func (operation GetWorkspaceOperation) Invoke(call sdkgo.Call, input ResourcePathInput) sdkgo.QueryAttempt[TagManagerResource] {
	return operation.client.getResource(call, input.Path, "accounts/*/containers/*/workspaces", "getWorkspace")
}

// GetTagOperation reads one tags resources.
type GetTagOperation struct{ client *Client }

// GetTag binds the tags lookup Query.
func (client *Client) GetTag() GetTagOperation { return GetTagOperation{client} }

// Definition returns the generated tags Query contract.
func (GetTagOperation) Definition() sdkgo.QueryDefinition { return GetTagDefinition }

// Invoke reads a projected tags identity.
func (operation GetTagOperation) Invoke(call sdkgo.Call, input ResourcePathInput) sdkgo.QueryAttempt[TagManagerResource] {
	return operation.client.getResource(call, input.Path, "accounts/*/containers/*/workspaces/*/tags", "getTag")
}

// ListTriggersOperation reads a page of triggers resources.
type ListTriggersOperation struct{ client *Client }

// ListTriggers binds the triggers list Query.
func (client *Client) ListTriggers() ListTriggersOperation { return ListTriggersOperation{client} }

// Definition returns the generated triggers Query contract.
func (ListTriggersOperation) Definition() sdkgo.QueryDefinition { return ListTriggersDefinition }

// Invoke lists projected triggers identities.
func (operation ListTriggersOperation) Invoke(call sdkgo.Call, input ListChildResourcesInput) sdkgo.QueryAttempt[TagManagerResourcesPage] {
	return operation.client.listResources(call, input, "accounts/*/containers/*/workspaces", "triggers", "trigger", "listTriggers")
}

// GetTriggerOperation reads one triggers resources.
type GetTriggerOperation struct{ client *Client }

// GetTrigger binds the triggers lookup Query.
func (client *Client) GetTrigger() GetTriggerOperation { return GetTriggerOperation{client} }

// Definition returns the generated triggers Query contract.
func (GetTriggerOperation) Definition() sdkgo.QueryDefinition { return GetTriggerDefinition }

// Invoke reads a projected triggers identity.
func (operation GetTriggerOperation) Invoke(call sdkgo.Call, input ResourcePathInput) sdkgo.QueryAttempt[TagManagerResource] {
	return operation.client.getResource(call, input.Path, "accounts/*/containers/*/workspaces/*/triggers", "getTrigger")
}

// ListVariablesOperation reads a page of variables resources.
type ListVariablesOperation struct{ client *Client }

// ListVariables binds the variables list Query.
func (client *Client) ListVariables() ListVariablesOperation { return ListVariablesOperation{client} }

// Definition returns the generated variables Query contract.
func (ListVariablesOperation) Definition() sdkgo.QueryDefinition { return ListVariablesDefinition }

// Invoke lists projected variables identities.
func (operation ListVariablesOperation) Invoke(call sdkgo.Call, input ListChildResourcesInput) sdkgo.QueryAttempt[TagManagerResourcesPage] {
	return operation.client.listResources(call, input, "accounts/*/containers/*/workspaces", "variables", "variable", "listVariables")
}

// GetVariableOperation reads one variables resources.
type GetVariableOperation struct{ client *Client }

// GetVariable binds the variables lookup Query.
func (client *Client) GetVariable() GetVariableOperation { return GetVariableOperation{client} }

// Definition returns the generated variables Query contract.
func (GetVariableOperation) Definition() sdkgo.QueryDefinition { return GetVariableDefinition }

// Invoke reads a projected variables identity.
func (operation GetVariableOperation) Invoke(call sdkgo.Call, input ResourcePathInput) sdkgo.QueryAttempt[TagManagerResource] {
	return operation.client.getResource(call, input.Path, "accounts/*/containers/*/workspaces/*/variables", "getVariable")
}

// ListFoldersOperation reads a page of folders resources.
type ListFoldersOperation struct{ client *Client }

// ListFolders binds the folders list Query.
func (client *Client) ListFolders() ListFoldersOperation { return ListFoldersOperation{client} }

// Definition returns the generated folders Query contract.
func (ListFoldersOperation) Definition() sdkgo.QueryDefinition { return ListFoldersDefinition }

// Invoke lists projected folders identities.
func (operation ListFoldersOperation) Invoke(call sdkgo.Call, input ListChildResourcesInput) sdkgo.QueryAttempt[TagManagerResourcesPage] {
	return operation.client.listResources(call, input, "accounts/*/containers/*/workspaces", "folders", "folder", "listFolders")
}

// GetFolderOperation reads one folders resources.
type GetFolderOperation struct{ client *Client }

// GetFolder binds the folders lookup Query.
func (client *Client) GetFolder() GetFolderOperation { return GetFolderOperation{client} }

// Definition returns the generated folders Query contract.
func (GetFolderOperation) Definition() sdkgo.QueryDefinition { return GetFolderDefinition }

// Invoke reads a projected folders identity.
func (operation GetFolderOperation) Invoke(call sdkgo.Call, input ResourcePathInput) sdkgo.QueryAttempt[TagManagerResource] {
	return operation.client.getResource(call, input.Path, "accounts/*/containers/*/workspaces/*/folders", "getFolder")
}

// ListTemplatesOperation reads a page of templates resources.
type ListTemplatesOperation struct{ client *Client }

// ListTemplates binds the templates list Query.
func (client *Client) ListTemplates() ListTemplatesOperation { return ListTemplatesOperation{client} }

// Definition returns the generated templates Query contract.
func (ListTemplatesOperation) Definition() sdkgo.QueryDefinition { return ListTemplatesDefinition }

// Invoke lists projected templates identities.
func (operation ListTemplatesOperation) Invoke(call sdkgo.Call, input ListChildResourcesInput) sdkgo.QueryAttempt[TagManagerResourcesPage] {
	return operation.client.listResources(call, input, "accounts/*/containers/*/workspaces", "templates", "template", "listTemplates")
}

// GetTemplateOperation reads one templates resources.
type GetTemplateOperation struct{ client *Client }

// GetTemplate binds the templates lookup Query.
func (client *Client) GetTemplate() GetTemplateOperation { return GetTemplateOperation{client} }

// Definition returns the generated templates Query contract.
func (GetTemplateOperation) Definition() sdkgo.QueryDefinition { return GetTemplateDefinition }

// Invoke reads a projected templates identity.
func (operation GetTemplateOperation) Invoke(call sdkgo.Call, input ResourcePathInput) sdkgo.QueryAttempt[TagManagerResource] {
	return operation.client.getResource(call, input.Path, "accounts/*/containers/*/workspaces/*/templates", "getTemplate")
}

// ListEnvironmentsOperation reads a page of environments resources.
type ListEnvironmentsOperation struct{ client *Client }

// ListEnvironments binds the environments list Query.
func (client *Client) ListEnvironments() ListEnvironmentsOperation {
	return ListEnvironmentsOperation{client}
}

// Definition returns the generated environments Query contract.
func (ListEnvironmentsOperation) Definition() sdkgo.QueryDefinition {
	return ListEnvironmentsDefinition
}

// Invoke lists projected environments identities.
func (operation ListEnvironmentsOperation) Invoke(call sdkgo.Call, input ListChildResourcesInput) sdkgo.QueryAttempt[TagManagerResourcesPage] {
	return operation.client.listResources(call, input, "accounts/*/containers", "environments", "environment", "listEnvironments")
}

// GetEnvironmentOperation reads one environments resources.
type GetEnvironmentOperation struct{ client *Client }

// GetEnvironment binds the environments lookup Query.
func (client *Client) GetEnvironment() GetEnvironmentOperation {
	return GetEnvironmentOperation{client}
}

// Definition returns the generated environments Query contract.
func (GetEnvironmentOperation) Definition() sdkgo.QueryDefinition { return GetEnvironmentDefinition }

// Invoke reads a projected environments identity.
func (operation GetEnvironmentOperation) Invoke(call sdkgo.Call, input ResourcePathInput) sdkgo.QueryAttempt[TagManagerResource] {
	return operation.client.getResource(call, input.Path, "accounts/*/containers/*/environments", "getEnvironment")
}

// GetVersionOperation reads one versions resources.
type GetVersionOperation struct{ client *Client }

// GetVersion binds the versions lookup Query.
func (client *Client) GetVersion() GetVersionOperation { return GetVersionOperation{client} }

// Definition returns the generated versions Query contract.
func (GetVersionOperation) Definition() sdkgo.QueryDefinition { return GetVersionDefinition }

// Invoke reads a projected versions identity.
func (operation GetVersionOperation) Invoke(call sdkgo.Call, input ResourcePathInput) sdkgo.QueryAttempt[TagManagerResource] {
	return operation.client.getResource(call, input.Path, "accounts/*/containers/*/versions", "getVersion")
}

func (client *Client) getResource(call sdkgo.Call, path, pathPattern, operation string) sdkgo.QueryAttempt[TagManagerResource] {
	if !matchesResourcePath(path, pathPattern) {
		return queryBranch(operation, "defect", TagManagerResource{}, sdkgo.FailureValidation, "Tag Manager resource path is invalid")
	}
	response := client.request(call, "/tagmanager/v2/"+path)
	if decision, failed := classifyRead[TagManagerResource](response, operation); failed {
		return decision
	}
	var resource TagManagerResource
	if json.Unmarshal(response.body, &resource) != nil || resource.Path != path {
		return queryBranch(operation, "invalidResponse", TagManagerResource{}, sdkgo.FailureProtocol, "Google returned invalid resource JSON")
	}
	return sdkgo.NewQueryBranch(sdkgo.BranchID("found"), resource, nil, sdkgo.Receipt{})
}

func (client *Client) listResources(call sdkgo.Call, input ListChildResourcesInput, parentPattern, childName, wireField, operation string) sdkgo.QueryAttempt[TagManagerResourcesPage] {
	if !matchesResourcePath(input.ParentPath, parentPattern) || !validPageToken(input.PageToken) {
		return queryBranch(operation, "defect", TagManagerResourcesPage{}, sdkgo.FailureValidation, "Tag Manager parent path or page token is invalid")
	}
	response := client.request(call, pagePath("/tagmanager/v2/"+input.ParentPath+"/"+childName, input.PageToken))
	if decision, failed := classifyRead[TagManagerResourcesPage](response, operation); failed {
		return decision
	}
	var wire map[string]json.RawMessage
	if json.Unmarshal(response.body, &wire) != nil {
		return queryBranch(operation, "invalidResponse", TagManagerResourcesPage{}, sdkgo.FailureProtocol, "Google returned invalid resource page JSON")
	}
	var resources []TagManagerResource
	var nextPageToken string
	if raw := wire[wireField]; len(raw) != 0 && json.Unmarshal(raw, &resources) != nil {
		return queryBranch(operation, "invalidResponse", TagManagerResourcesPage{}, sdkgo.FailureProtocol, "Google returned invalid resource page JSON")
	}
	if raw := wire["nextPageToken"]; len(raw) != 0 && json.Unmarshal(raw, &nextPageToken) != nil {
		return queryBranch(operation, "invalidResponse", TagManagerResourcesPage{}, sdkgo.FailureProtocol, "Google returned invalid page cursor")
	}
	if !validPageResult(len(resources), nextPageToken) {
		return queryBranch(operation, "invalidResponse", TagManagerResourcesPage{}, sdkgo.FailureProtocol, "Google returned an invalid resource page")
	}
	for _, resource := range resources {
		if !matchesResourcePath(resource.Path, parentPattern+"/*/"+childName) || !strings.HasPrefix(resource.Path, input.ParentPath+"/") {
			return queryBranch(operation, "invalidResponse", TagManagerResourcesPage{}, sdkgo.FailureProtocol, "Google returned incomplete resource metadata")
		}
	}
	return sdkgo.NewQueryBranch(sdkgo.BranchID("listed"), TagManagerResourcesPage{Resources: resources, NextPageToken: nextPageToken}, nil, sdkgo.Receipt{})
}

func matchesResourcePath(path, pattern string) bool {
	segments := strings.Split(path, "/")
	parts := strings.Split(pattern, "/")
	if len(segments) != len(parts)+1 {
		return false
	}
	for index, part := range parts {
		if part != "*" && segments[index] != part {
			return false
		}
		if part == "*" && !validID(segments[index]) {
			return false
		}
	}
	return validID(segments[len(segments)-1])
}
