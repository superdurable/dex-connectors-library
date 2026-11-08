// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tagmanager

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var numericID = regexp.MustCompile(`^[0-9]{1,30}$`)

// ListAccountsInput selects one page of accounts visible to the connected user.
type ListAccountsInput struct {
	// PageToken is Google's opaque cursor; blank selects the first page.
	PageToken string `json:"pageToken"`
}

// TagManagerAccount is the stable account path and display name.
type TagManagerAccount struct {
	// Path is the API resource identity, such as accounts/123456.
	Path string `json:"path"`
	// Name is the display name shown in Tag Manager.
	Name string `json:"name"`
}

// AccountsPage contains one bounded account page and next cursor.
type AccountsPage struct {
	// Accounts are the projected account identities.
	Accounts []TagManagerAccount `json:"accounts"`
	// NextPageToken is blank on the final page.
	NextPageToken string `json:"nextPageToken"`
}

// ListContainersInput selects containers in one account.
type ListContainersInput struct {
	// AccountID is the numeric account ID from listAccounts.
	AccountID string `json:"accountId"`
	// PageToken is Google's opaque cursor; blank selects the first page.
	PageToken string `json:"pageToken"`
}

// TagManagerContainer is the stable container path and display identity.
type TagManagerContainer struct {
	// Path is the API resource identity.
	Path string `json:"path"`
	// Name is the display name shown in Tag Manager.
	Name string `json:"name"`
	// PublicID is the GTM identifier shown in the Tag Manager UI.
	PublicID string `json:"publicId"`
}

// ContainersPage contains one bounded container page and next cursor.
type ContainersPage struct {
	// Containers are the projected container identities.
	Containers []TagManagerContainer `json:"containers"`
	// NextPageToken is blank on the final page.
	NextPageToken string `json:"nextPageToken"`
}

// ListTagsInput selects tags in one workspace.
type ListTagsInput struct {
	// AccountID is the numeric Tag Manager account ID.
	AccountID string `json:"accountId"`
	// ContainerID is the numeric Tag Manager container ID.
	ContainerID string `json:"containerId"`
	// WorkspaceID is the numeric Tag Manager workspace ID.
	WorkspaceID string `json:"workspaceId"`
	// PageToken is Google's opaque cursor; blank selects the first page.
	PageToken string `json:"pageToken"`
}

// TagManagerTag contains a tag's stable path, display name, and type.
type TagManagerTag struct {
	// Path is the API resource identity.
	Path string `json:"path"`
	// Name is the display name shown in Tag Manager.
	Name string `json:"name"`
	// Type is Google's tag template type identifier.
	Type string `json:"type"`
}

// TagsPage contains one bounded tag page and next cursor.
type TagsPage struct {
	// Tags are the projected tag identities.
	Tags []TagManagerTag `json:"tags"`
	// NextPageToken is blank on the final page.
	NextPageToken string `json:"nextPageToken"`
}

// ListAccountsOperation reads one account page.
type ListAccountsOperation struct{ client *Client }

// ListContainersOperation reads one container page.
type ListContainersOperation struct{ client *Client }

// ListTagsOperation reads one workspace tag page.
type ListTagsOperation struct{ client *Client }

// ListAccounts binds the account Query.
func (client *Client) ListAccounts() ListAccountsOperation { return ListAccountsOperation{client} }

// ListContainers binds the container Query.
func (client *Client) ListContainers() ListContainersOperation {
	return ListContainersOperation{client}
}

// ListTags binds the tag Query.
func (client *Client) ListTags() ListTagsOperation { return ListTagsOperation{client} }

// Definition returns the generated account Query contract.
func (ListAccountsOperation) Definition() sdkgo.QueryDefinition { return ListAccountsDefinition }

// Definition returns the generated container Query contract.
func (ListContainersOperation) Definition() sdkgo.QueryDefinition { return ListContainersDefinition }

// Definition returns the generated tag Query contract.
func (ListTagsOperation) Definition() sdkgo.QueryDefinition { return ListTagsDefinition }

// Invoke lists account identities without retaining private account fields.
func (operation ListAccountsOperation) Invoke(call sdkgo.Call, input ListAccountsInput) sdkgo.QueryAttempt[AccountsPage] {
	if !validPageToken(input.PageToken) {
		return queryBranch("listAccounts", "defect", AccountsPage{}, sdkgo.FailureValidation, "account page token is invalid")
	}
	response := operation.client.request(call, pagePath("/tagmanager/v2/accounts", input.PageToken))
	if decision, failed := classifyRead[AccountsPage](response, "listAccounts"); failed {
		return decision
	}
	var wire struct {
		Account       []TagManagerAccount `json:"account"`
		NextPageToken string              `json:"nextPageToken"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || !validPageResult(len(wire.Account), wire.NextPageToken) {
		return queryBranch("listAccounts", "invalidResponse", AccountsPage{}, sdkgo.FailureProtocol, "Google returned invalid account JSON")
	}
	accounts := make([]TagManagerAccount, 0, len(wire.Account))
	for _, account := range wire.Account {
		if !validResourcePath(account.Path, "accounts", 1) || account.Name == "" {
			return queryBranch("listAccounts", "invalidResponse", AccountsPage{}, sdkgo.FailureProtocol, "Google returned incomplete account metadata")
		}
		accounts = append(accounts, account)
	}
	return sdkgo.NewQueryBranch(ListAccountsBranchListed, AccountsPage{Accounts: accounts, NextPageToken: wire.NextPageToken}, nil, sdkgo.Receipt{})
}

// Invoke lists containers in a numeric Tag Manager account.
func (operation ListContainersOperation) Invoke(call sdkgo.Call, input ListContainersInput) sdkgo.QueryAttempt[ContainersPage] {
	if !validID(input.AccountID) || !validPageToken(input.PageToken) {
		return queryBranch("listContainers", "defect", ContainersPage{}, sdkgo.FailureValidation, "container page input is invalid")
	}
	response := operation.client.request(call, pagePath("/tagmanager/v2/accounts/"+input.AccountID+"/containers", input.PageToken))
	if decision, failed := classifyRead[ContainersPage](response, "listContainers"); failed {
		return decision
	}
	var wire struct {
		Container     []TagManagerContainer `json:"container"`
		NextPageToken string                `json:"nextPageToken"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || !validPageResult(len(wire.Container), wire.NextPageToken) {
		return queryBranch("listContainers", "invalidResponse", ContainersPage{}, sdkgo.FailureProtocol, "Google returned invalid container JSON")
	}
	containers := make([]TagManagerContainer, 0, len(wire.Container))
	for _, container := range wire.Container {
		if !validResourcePath(container.Path, "accounts/"+input.AccountID+"/containers", 1) || container.Name == "" || container.PublicID == "" {
			return queryBranch("listContainers", "invalidResponse", ContainersPage{}, sdkgo.FailureProtocol, "Google returned incomplete container metadata")
		}
		containers = append(containers, container)
	}
	return sdkgo.NewQueryBranch(ListContainersBranchListed, ContainersPage{Containers: containers, NextPageToken: wire.NextPageToken}, nil, sdkgo.Receipt{})
}

// Invoke lists tag identities and types in one workspace.
func (operation ListTagsOperation) Invoke(call sdkgo.Call, input ListTagsInput) sdkgo.QueryAttempt[TagsPage] {
	if !validID(input.AccountID) || !validID(input.ContainerID) || !validID(input.WorkspaceID) || !validPageToken(input.PageToken) {
		return queryBranch("listTags", "defect", TagsPage{}, sdkgo.FailureValidation, "tag page input is invalid")
	}
	parent := "accounts/" + input.AccountID + "/containers/" + input.ContainerID + "/workspaces/" + input.WorkspaceID
	response := operation.client.request(call, pagePath("/tagmanager/v2/"+parent+"/tags", input.PageToken))
	if decision, failed := classifyRead[TagsPage](response, "listTags"); failed {
		return decision
	}
	var wire struct {
		Tag           []TagManagerTag `json:"tag"`
		NextPageToken string          `json:"nextPageToken"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || !validPageResult(len(wire.Tag), wire.NextPageToken) {
		return queryBranch("listTags", "invalidResponse", TagsPage{}, sdkgo.FailureProtocol, "Google returned invalid tag JSON")
	}
	tags := make([]TagManagerTag, 0, len(wire.Tag))
	for _, tag := range wire.Tag {
		if !validResourcePath(tag.Path, parent+"/tags", 1) || tag.Name == "" || tag.Type == "" {
			return queryBranch("listTags", "invalidResponse", TagsPage{}, sdkgo.FailureProtocol, "Google returned incomplete tag metadata")
		}
		tags = append(tags, tag)
	}
	return sdkgo.NewQueryBranch(ListTagsBranchListed, TagsPage{Tags: tags, NextPageToken: wire.NextPageToken}, nil, sdkgo.Receipt{})
}

func classifyRead[T any](response providerResponse, operation string) (sdkgo.QueryAttempt[T], bool) {
	var zero T
	if errors.Is(response.err, errCredential) {
		return queryBranch(operation, "defect", zero, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid"), true
	}
	if errors.Is(response.err, errRequest) {
		return queryRetry[T](operation, sdkgo.FailureTransport, "Google Tag Manager request failed", 0), true
	}
	if response.status == 429 {
		return queryRetry[T](operation, sdkgo.FailureRateLimit, "Google rate limited the request", response.retryAfter), true
	}
	if response.status == 408 || response.status >= 500 {
		return queryRetry[T](operation, sdkgo.FailureAvailability, "Google Tag Manager is temporarily unavailable", response.retryAfter), true
	}
	if response.status != 200 {
		return queryBranch(operation, "providerRejected", zero, sdkgo.FailureProviderRejection, "Google rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return queryBranch(operation, "invalidResponse", zero, sdkgo.FailureResponseTooLarge, "Google response exceeded the configured limit"), true
	}
	if response.err != nil {
		return queryRetry[T](operation, sdkgo.FailureTransport, "Google response was interrupted", 0), true
	}
	return sdkgo.QueryAttempt[T]{}, false
}
func queryBranch[T any](operation, branch string, value T, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-tag-manager", Operation: operation, Message: message}
	return sdkgo.NewQueryBranch(sdkgo.BranchID(branch), value, &failure, sdkgo.Receipt{})
}
func queryRetry[T any](operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-tag-manager", Operation: operation, Message: message}
	return sdkgo.NewQueryRetry[T](failure, delay)
}
func validID(value string) bool { return numericID.MatchString(value) }
func validPageToken(value string) bool {
	return len(value) <= 4096 && !strings.ContainsAny(value, "\r\n")
}
func validPageResult(count int, next string) bool { return count <= 1000 && validPageToken(next) }
func validResourcePath(path, prefix string, segments int) bool {
	if !strings.HasPrefix(path, prefix+"/") {
		return false
	}
	suffix := strings.TrimPrefix(path, prefix+"/")
	return segments == 1 && validID(suffix)
}
func pagePath(path, token string) string {
	if token == "" {
		return path
	}
	return path + "?pageToken=" + url.QueryEscape(token)
}
