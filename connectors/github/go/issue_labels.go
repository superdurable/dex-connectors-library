// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package github

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const replaceIssueLabelsOperationID = "replaceIssueLabels"

// ReplaceIssueLabelsInput names one existing issue and the complete label set it should carry.
type ReplaceIssueLabelsInput struct {
	// Owner is the GitHub login that owns the repository. Surrounding whitespace is ignored.
	Owner string `json:"owner"`
	// Repository is the repository name without its owner, such as hello-world.
	Repository string `json:"repository"`
	// IssueNumber is the positive issue number within the repository.
	IssueNumber int `json:"issueNumber"`
	// Labels names up to 100 labels of 1 through 50 characters that replace every current label; surrounding
	// whitespace is trimmed and an empty list removes all labels. GitHub creates a named label the repository
	// lacks.
	Labels []string `json:"labels"`
}

// ReplacedIssueLabels is the issue's label set after the replacement. On every other branch only Number is
// set, echoing the requested issue number.
type ReplacedIssueLabels struct {
	// Number is the issue number within its repository.
	Number int `json:"number"`
	// Labels lists the label names GitHub reports on the issue, each bounded to 256 bytes.
	Labels []string `json:"labels"`
	// URL is the issue's GitHub web URL, derived from the configured base URL for GitHub.com or a GitHub
	// Enterprise Server /api/v3 URL, and empty otherwise.
	URL string `json:"url,omitempty"`
}

// ReplaceIssueLabelsOperation implements the replaceIssueLabels Mutation.
type ReplaceIssueLabelsOperation struct{ client *Client }

type replaceIssueLabelsRequestBody struct {
	Labels []string `json:"labels"`
}

// Definition returns the manifest-generated execution and branch contract.
func (ReplaceIssueLabelsOperation) Definition() sdkgo.MutationDefinition {
	return ReplaceIssueLabelsDefinition
}

// IdempotencyKey derives the key recorded in the Receipt from the stable Call ID. GitHub accepts no
// idempotency key; replacing the full label set is safe to send again instead.
func (ReplaceIssueLabelsOperation) IdempotencyKey(callID sdkgo.CallID, _ ReplaceIssueLabelsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one PUT that replaces the issue's labels. Invalid input and a connection without the
// github-oauth-repository method select a branch before GitHub is called. Rate limits, 5xx answers, and
// transport failures return Retry, because sending the same full set again is harmless.
func (operation ReplaceIssueLabelsOperation) Invoke(call sdkgo.Call, input ReplaceIssueLabelsInput) sdkgo.MutationAttempt[ReplacedIssueLabels] {
	requested := ReplacedIssueLabels{Number: max(input.IssueNumber, 0)}
	owner, repository, request, failure := buildReplaceIssueLabelsRequest(input)
	if failure != nil {
		return sdkgo.NewMutationBranch(ReplaceIssueLabelsBranchDefect, requested, failure, sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, replaceIssueLabelsOperationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(ReplaceIssueLabelsBranchDefect, requested, failure, sdkgo.Receipt{})
	}
	if credential.AuthMethodID != RepositoryAuthMethodID {
		return issueLabelsBranch(ReplaceIssueLabelsBranchInsufficientScope, requested, sdkgo.FailureAuthorization,
			"replaceIssueLabels requires a connection authorized with the github-oauth-repository method", sdkgo.Receipt{})
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return issueLabelsBranch(ReplaceIssueLabelsBranchDefect, requested, sdkgo.FailureLocalDefect, "GitHub issue labels request could not be encoded", sdkgo.Receipt{})
	}
	path := issuesPath(owner, repository) + "/" + strconv.Itoa(input.IssueNumber) + "/labels"
	response, err := operation.client.sendRequest(call, &credential, http.MethodPut, path, nil, payload)
	if err != nil {
		if errors.Is(err, errResponseTooLarge) {
			return issueLabelsBranch(ReplaceIssueLabelsBranchInvalidResponse, requested, sdkgo.FailureResponseTooLarge, "GitHub response exceeds the configured size limit", receipt(response))
		}
		return sdkgo.NewMutationRetry[ReplacedIssueLabels](providerFailure(replaceIssueLabelsOperationID, sdkgo.FailureTransport, "GitHub issue labels update did not complete"), 0)
	}
	if terminal := operation.client.classify(replaceIssueLabelsOperationID, response, issueLabelsResponseBranches()); terminal != nil {
		if terminal.isRetryable {
			return sdkgo.NewMutationRetry[ReplacedIssueLabels](terminal.failure, terminal.delay)
		}
		if terminal.branch == ReplaceIssueLabelsBranchProviderRejected {
			return issueLabelsBranch(terminal.branch, requested, statusFailureKind(response.statusCode),
				"GitHub rejected the issue labels with HTTP "+strconv.Itoa(response.statusCode), receipt(response))
		}
		return sdkgo.NewMutationBranch(terminal.branch, requested, &terminal.failure, receipt(response))
	}
	var providerLabels []githubLabel
	if decodeJSON(response.body, &providerLabels) != nil || providerLabels == nil || len(providerLabels) > maximumIssueLabels {
		return issueLabelsBranch(ReplaceIssueLabelsBranchInvalidResponse, requested, sdkgo.FailureProtocol, "GitHub returned an invalid label list", receipt(response))
	}
	names := make([]string, 0, len(providerLabels))
	for _, label := range providerLabels {
		names = append(names, label.Name)
	}
	output := ReplacedIssueLabels{
		Number: input.IssueNumber, Labels: boundedStrings(names, maximumLabels, maximumLabelBytes),
		URL: issueWebURL(operation.client.baseURL, owner, repository, input.IssueNumber),
	}
	return sdkgo.NewMutationBranch(ReplaceIssueLabelsBranchReplaced, output, nil, receipt(response))
}

// buildReplaceIssueLabelsRequest validates the repository, issue number, and label names.
func buildReplaceIssueLabelsRequest(input ReplaceIssueLabelsInput) (string, string, replaceIssueLabelsRequestBody, *sdkgo.Failure) {
	invalid := func(message string) (string, string, replaceIssueLabelsRequestBody, *sdkgo.Failure) {
		failure := providerFailure(replaceIssueLabelsOperationID, sdkgo.FailureValidation, message)
		return "", "", replaceIssueLabelsRequestBody{}, &failure
	}
	owner, repository, failure := validateRepository(replaceIssueLabelsOperationID, input.Owner, input.Repository)
	if failure != nil {
		return "", "", replaceIssueLabelsRequestBody{}, failure
	}
	if input.IssueNumber <= 0 {
		return invalid("GitHub issue number must be positive")
	}
	if len(input.Labels) > maximumIssueLabels {
		return invalid("GitHub issue has more than 100 labels")
	}
	labels := make([]string, 0, len(input.Labels))
	for _, label := range input.Labels {
		label = strings.TrimSpace(label)
		if label == "" || !isSingleLineText(label) || utf8.RuneCountInString(label) > maximumIssueLabelCharacters {
			return invalid("GitHub issue labels must be one line of 1 through 50 characters")
		}
		labels = append(labels, label)
	}
	return owner, repository, replaceIssueLabelsRequestBody{Labels: labels}, nil
}

// issueWebURL maps the API base URL to the web host: api.github.com to github.com, and a GitHub Enterprise
// Server /api/v3 URL to its host. Any other base URL has no known web host.
func issueWebURL(baseURL *url.URL, owner string, repository string, issueNumber int) string {
	webURL := url.URL{Scheme: baseURL.Scheme, Host: baseURL.Host}
	switch {
	case strings.EqualFold(baseURL.Hostname(), "api.github.com") && strings.Trim(baseURL.Path, "/") == "":
		webURL.Host = "github.com"
	case strings.TrimRight(baseURL.Path, "/") == "/api/v3":
	default:
		return ""
	}
	webURL.Path = "/" + owner + "/" + repository + "/issues/" + strconv.Itoa(issueNumber)
	return webURL.String()
}

func issueLabelsBranch(branch sdkgo.BranchID, requested ReplacedIssueLabels, kind sdkgo.FailureKind, message string, branchReceipt sdkgo.Receipt) sdkgo.MutationAttempt[ReplacedIssueLabels] {
	failure := providerFailure(replaceIssueLabelsOperationID, kind, message)
	return sdkgo.NewMutationBranch(branch, requested, &failure, branchReceipt)
}

func issueLabelsResponseBranches() responseBranches {
	return responseBranches{
		insufficientScope: ReplaceIssueLabelsBranchInsufficientScope,
		revoked:           ReplaceIssueLabelsBranchAuthorizationRevoked,
		notFound:          ReplaceIssueLabelsBranchNotFound,
		providerRejected:  ReplaceIssueLabelsBranchProviderRejected,
	}
}
