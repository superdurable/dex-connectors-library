// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package githubconnector provides bounded, authenticated GitHub profile queries.
package githubconnector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	maximumRepositoryLimit = 500
	providerPageSize       = 100
	userAgent              = "superdurable-dex-github-connector/0.1"
)

var (
	apiVersionPattern  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	githubLoginPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
)

type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	now        func() time.Time
}

func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

type Client struct {
	baseURL                *url.URL
	apiVersion             string
	maxResponseBytes       int64
	defaultRepositoryLimit int
	maxRepositories        int
	httpClient             *http.Client
	credentials            sdkgo.CredentialProvider[Credentials]
	now                    func() time.Time
}

type GetAuthenticatedProfileInput struct{}

type AuthenticatedProfile struct {
	Subject         string     `json:"subject"`
	Login           string     `json:"login"`
	Name            string     `json:"name,omitempty"`
	VerifiedEmail   string     `json:"verifiedEmail"`
	AvatarURL       string     `json:"avatarUrl,omitempty"`
	ProfileURL      string     `json:"profileUrl,omitempty"`
	Company         string     `json:"company,omitempty"`
	BlogURL         string     `json:"blogUrl,omitempty"`
	Location        string     `json:"location,omitempty"`
	Bio             string     `json:"bio,omitempty"`
	TwitterUsername string     `json:"twitterUsername,omitempty"`
	PublicRepos     int64      `json:"publicRepos"`
	Followers       int64      `json:"followers"`
	Following       int64      `json:"following"`
	CreatedAt       *time.Time `json:"createdAt,omitempty"`
	UpdatedAt       *time.Time `json:"updatedAt,omitempty"`
}

type ListPublicRepositoriesInput struct {
	Login string `json:"login"`
	Limit int    `json:"limit,omitempty"`
}

type PublicRepositories struct {
	Repositories []Repository `json:"repositories"`
	Truncated    bool         `json:"truncated"`
}

type Repository struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	FullName        string     `json:"fullName"`
	Description     string     `json:"description,omitempty"`
	ProfileURL      string     `json:"profileUrl,omitempty"`
	HomepageURL     string     `json:"homepageUrl,omitempty"`
	Language        string     `json:"language,omitempty"`
	Topics          []string   `json:"topics,omitempty"`
	Fork            bool       `json:"fork"`
	Archived        bool       `json:"archived"`
	Disabled        bool       `json:"disabled"`
	StargazersCount int64      `json:"stargazersCount"`
	ForksCount      int64      `json:"forksCount"`
	OpenIssuesCount int64      `json:"openIssuesCount"`
	CreatedAt       *time.Time `json:"createdAt,omitempty"`
	UpdatedAt       *time.Time `json:"updatedAt,omitempty"`
	PushedAt        *time.Time `json:"pushedAt,omitempty"`
}

type GetAuthenticatedProfileOperation struct{ client *Client }

type ListPublicRepositoriesOperation struct{ client *Client }

type githubUser struct {
	ID              int64      `json:"id"`
	Login           string     `json:"login"`
	Name            string     `json:"name"`
	AvatarURL       string     `json:"avatar_url"`
	HTMLURL         string     `json:"html_url"`
	Company         string     `json:"company"`
	Blog            string     `json:"blog"`
	Location        string     `json:"location"`
	Bio             string     `json:"bio"`
	TwitterUsername string     `json:"twitter_username"`
	PublicRepos     int64      `json:"public_repos"`
	Followers       int64      `json:"followers"`
	Following       int64      `json:"following"`
	CreatedAt       *time.Time `json:"created_at"`
	UpdatedAt       *time.Time `json:"updated_at"`
}

type githubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

type githubRepository struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	FullName        string     `json:"full_name"`
	Private         bool       `json:"private"`
	Visibility      string     `json:"visibility"`
	HTMLURL         string     `json:"html_url"`
	Description     string     `json:"description"`
	Homepage        string     `json:"homepage"`
	Language        string     `json:"language"`
	Topics          []string   `json:"topics"`
	Fork            bool       `json:"fork"`
	Archived        bool       `json:"archived"`
	Disabled        bool       `json:"disabled"`
	StargazersCount int64      `json:"stargazers_count"`
	ForksCount      int64      `json:"forks_count"`
	OpenIssuesCount int64      `json:"open_issues_count"`
	CreatedAt       *time.Time `json:"created_at"`
	UpdatedAt       *time.Time `json:"updated_at"`
	PushedAt        *time.Time `json:"pushed_at"`
}

type providerResponse struct {
	statusCode int
	header     http.Header
	body       []byte
	requestID  string
}

type terminalResponse struct {
	branch  sdkgo.BranchID
	failure sdkgo.Failure
	retry   bool
	delay   time.Duration
}

func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := url.Parse(config.BaseURL)
	if err != nil || baseURL.Scheme == "" || baseURL.Hostname() == "" {
		return nil, fmt.Errorf("GitHub base URL must be absolute")
	}
	if baseURL.Scheme != "https" && !isLoopback(baseURL.Hostname()) {
		return nil, fmt.Errorf("non-loopback GitHub base URL must use HTTPS")
	}
	if baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, fmt.Errorf("GitHub base URL cannot contain user info, a query, or a fragment")
	}
	if config.MaxRepositories > maximumRepositoryLimit {
		return nil, fmt.Errorf("maximum repositories cannot exceed %d", maximumRepositoryLimit)
	}
	if config.DefaultRepositoryLimit > config.MaxRepositories {
		return nil, fmt.Errorf("default repository limit cannot exceed maximum repositories")
	}
	if credentials == nil {
		return nil, fmt.Errorf("credential provider is required")
	}
	if !apiVersionPattern.MatchString(config.APIVersion) {
		return nil, fmt.Errorf("GitHub API version must use YYYY-MM-DD")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("GitHub connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil {
		return nil, fmt.Errorf("GitHub connector clock is required")
	}
	httpClient := dependencies.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: config.Timeout}
	} else {
		copy := *httpClient
		httpClient = &copy
		if httpClient.Timeout == 0 {
			httpClient.Timeout = config.Timeout
		}
	}
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{
		baseURL: baseURL, apiVersion: config.APIVersion, maxResponseBytes: config.MaxResponseBytes,
		defaultRepositoryLimit: int(config.DefaultRepositoryLimit), maxRepositories: int(config.MaxRepositories),
		httpClient: httpClient, credentials: credentials, now: dependencies.now,
	}, nil
}

func (client *Client) GetAuthenticatedProfile() GetAuthenticatedProfileOperation {
	return GetAuthenticatedProfileOperation{client: client}
}

func (client *Client) ListPublicRepositories() ListPublicRepositoriesOperation {
	return ListPublicRepositoriesOperation{client: client}
}

func (GetAuthenticatedProfileOperation) Definition() sdkgo.QueryDefinition {
	return GetAuthenticatedProfileDefinition
}

func (operation GetAuthenticatedProfileOperation) Invoke(call sdkgo.Call, _ GetAuthenticatedProfileInput) sdkgo.QueryAttempt[AuthenticatedProfile] {
	credential, failure := operation.client.resolveCredential(call, "getAuthenticatedProfile")
	if failure != nil {
		return sdkgo.NewQueryBranch(GetAuthenticatedProfileBranchAuthorizationRevoked, AuthenticatedProfile{}, failure, sdkgo.Receipt{})
	}
	profileResponse, err := operation.client.get(call, credential, "/user", nil)
	if err != nil {
		if errors.Is(err, errResponseTooLarge) {
			failure := providerFailure("getAuthenticatedProfile", sdkgo.FailureResponseTooLarge, "GitHub profile response exceeds the configured size limit")
			return sdkgo.NewQueryBranch(GetAuthenticatedProfileBranchFailed, AuthenticatedProfile{}, &failure, receipt(profileResponse))
		}
		return sdkgo.NewQueryRetry[AuthenticatedProfile](providerFailure("getAuthenticatedProfile", sdkgo.FailureAvailability, "GitHub is unavailable"), 0)
	}
	if terminal := operation.client.classify("getAuthenticatedProfile", profileResponse, profileBranches()); terminal != nil {
		return profileTerminalAttempt(*terminal, receipt(profileResponse))
	}
	if !hasRequiredScopes(profileResponse.header) {
		failure := providerFailure("getAuthenticatedProfile", sdkgo.FailureAuthorization, "GitHub OAuth grant does not match required scopes")
		return sdkgo.NewQueryBranch(GetAuthenticatedProfileBranchInsufficientScope, AuthenticatedProfile{}, &failure, receipt(profileResponse))
	}
	var user githubUser
	if err := decodeJSON(profileResponse.body, &user); err != nil || user.ID <= 0 || strings.TrimSpace(user.Login) == "" {
		failure := providerFailure("getAuthenticatedProfile", sdkgo.FailureProtocol, "GitHub returned an invalid profile response")
		return sdkgo.NewQueryBranch(GetAuthenticatedProfileBranchFailed, AuthenticatedProfile{}, &failure, receipt(profileResponse))
	}
	emailResponse, err := operation.client.get(call, credential, "/user/emails", url.Values{"per_page": {"100"}})
	if err != nil {
		if errors.Is(err, errResponseTooLarge) {
			failure := providerFailure("getAuthenticatedProfile", sdkgo.FailureResponseTooLarge, "GitHub email response exceeds the configured size limit")
			return sdkgo.NewQueryBranch(GetAuthenticatedProfileBranchFailed, AuthenticatedProfile{}, &failure, receipt(emailResponse))
		}
		return sdkgo.NewQueryRetry[AuthenticatedProfile](providerFailure("getAuthenticatedProfile", sdkgo.FailureAvailability, "GitHub is unavailable"), 0)
	}
	combinedReceipt := receipt(emailResponse)
	combinedReceipt.Metadata = map[string]string{
		"profileRequestId": profileResponse.requestID,
		"emailRequestId":   emailResponse.requestID,
	}
	if terminal := operation.client.classify("getAuthenticatedProfile", emailResponse, profileBranches()); terminal != nil {
		return profileTerminalAttempt(*terminal, combinedReceipt)
	}
	if !hasRequiredScopes(emailResponse.header) {
		failure := providerFailure("getAuthenticatedProfile", sdkgo.FailureAuthorization, "GitHub OAuth grant does not match required scopes")
		return sdkgo.NewQueryBranch(GetAuthenticatedProfileBranchInsufficientScope, AuthenticatedProfile{}, &failure, combinedReceipt)
	}
	var emails []githubEmail
	if err := decodeJSON(emailResponse.body, &emails); err != nil {
		failure := providerFailure("getAuthenticatedProfile", sdkgo.FailureProtocol, "GitHub returned an invalid email response")
		return sdkgo.NewQueryBranch(GetAuthenticatedProfileBranchFailed, AuthenticatedProfile{}, &failure, combinedReceipt)
	}
	verifiedEmail := primaryVerifiedEmail(emails)
	if verifiedEmail == "" {
		failure := providerFailure("getAuthenticatedProfile", sdkgo.FailureAuthentication, "GitHub primary verified email is required")
		return sdkgo.NewQueryBranch(GetAuthenticatedProfileBranchVerifiedEmailRequired, AuthenticatedProfile{}, &failure, combinedReceipt)
	}
	profile := normalizeProfile(user, verifiedEmail)
	return sdkgo.NewQueryBranch(GetAuthenticatedProfileBranchProfileLoaded, profile, nil, combinedReceipt)
}

func (ListPublicRepositoriesOperation) Definition() sdkgo.QueryDefinition {
	return ListPublicRepositoriesDefinition
}

func (operation ListPublicRepositoriesOperation) Invoke(call sdkgo.Call, input ListPublicRepositoriesInput) sdkgo.QueryAttempt[PublicRepositories] {
	login := strings.TrimSpace(input.Login)
	if !githubLoginPattern.MatchString(login) {
		failure := providerFailure("listPublicRepositories", sdkgo.FailureValidation, "GitHub login is invalid")
		return sdkgo.NewQueryBranch(ListPublicRepositoriesBranchDefect, PublicRepositories{}, &failure, sdkgo.Receipt{})
	}
	limit := input.Limit
	if limit == 0 {
		limit = operation.client.defaultRepositoryLimit
	}
	if limit < 1 || limit > operation.client.maxRepositories {
		failure := providerFailure("listPublicRepositories", sdkgo.FailureValidation, "repository limit is outside the configured range")
		return sdkgo.NewQueryBranch(ListPublicRepositoriesBranchDefect, PublicRepositories{}, &failure, sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, "listPublicRepositories")
	if failure != nil {
		return sdkgo.NewQueryBranch(ListPublicRepositoriesBranchAuthorizationRevoked, PublicRepositories{}, failure, sdkgo.Receipt{})
	}
	repositories := make(map[int64]Repository, limit)
	page := 1
	truncated := false
	lastReceipt := sdkgo.Receipt{}
	for {
		response, err := operation.client.get(call, credential, "/users/"+url.PathEscape(login)+"/repos", url.Values{
			"type": {"owner"}, "sort": {"pushed"}, "direction": {"desc"},
			"per_page": {strconv.Itoa(providerPageSize)}, "page": {strconv.Itoa(page)},
		})
		if err != nil {
			if errors.Is(err, errResponseTooLarge) {
				failure := providerFailure("listPublicRepositories", sdkgo.FailureResponseTooLarge, "GitHub repository response exceeds the configured size limit")
				return sdkgo.NewQueryBranch(ListPublicRepositoriesBranchFailed, PublicRepositories{}, &failure, receipt(response))
			}
			return sdkgo.NewQueryRetry[PublicRepositories](providerFailure("listPublicRepositories", sdkgo.FailureAvailability, "GitHub is unavailable"), 0)
		}
		lastReceipt = receipt(response)
		lastReceipt.Metadata = map[string]string{"pagesRead": strconv.Itoa(page)}
		if terminal := operation.client.classify("listPublicRepositories", response, repositoryBranches()); terminal != nil {
			return repositoriesTerminalAttempt(*terminal, lastReceipt)
		}
		if !hasRequiredScopes(response.header) {
			failure := providerFailure("listPublicRepositories", sdkgo.FailureAuthorization, "GitHub OAuth grant does not match required scopes")
			return sdkgo.NewQueryBranch(ListPublicRepositoriesBranchInsufficientScope, PublicRepositories{}, &failure, lastReceipt)
		}
		var providerRepositories []githubRepository
		if err := decodeJSON(response.body, &providerRepositories); err != nil {
			failure := providerFailure("listPublicRepositories", sdkgo.FailureProtocol, "GitHub returned an invalid repository response")
			return sdkgo.NewQueryBranch(ListPublicRepositoriesBranchFailed, PublicRepositories{}, &failure, lastReceipt)
		}
		for _, providerRepository := range providerRepositories {
			if providerRepository.ID <= 0 || providerRepository.Private || (providerRepository.Visibility != "" && providerRepository.Visibility != "public") {
				continue
			}
			if _, exists := repositories[providerRepository.ID]; exists {
				continue
			}
			if len(repositories) == limit {
				truncated = true
				break
			}
			repositories[providerRepository.ID] = normalizeRepository(providerRepository)
		}
		hasNext := hasNextPage(response.header)
		if len(repositories) == limit {
			truncated = truncated || hasNext
			break
		}
		if !hasNext || len(providerRepositories) == 0 {
			break
		}
		page++
	}
	values := make([]Repository, 0, len(repositories))
	for _, repository := range repositories {
		values = append(values, repository)
	}
	sort.Slice(values, func(left, right int) bool {
		leftPushed, rightPushed := values[left].PushedAt, values[right].PushedAt
		if leftPushed == nil && rightPushed != nil {
			return false
		}
		if leftPushed != nil && rightPushed == nil {
			return true
		}
		if leftPushed != nil && rightPushed != nil && !leftPushed.Equal(*rightPushed) {
			return leftPushed.After(*rightPushed)
		}
		return values[left].ID < values[right].ID
	})
	output := PublicRepositories{Repositories: values, Truncated: truncated}
	return sdkgo.NewQueryBranch(ListPublicRepositoriesBranchRepositoriesLoaded, output, nil, lastReceipt)
}

func (client *Client) resolveCredential(call sdkgo.Call, operation string) (Credentials, *sdkgo.Failure) {
	credential, err := client.credentials.Resolve(call)
	if err != nil || credential.Validate() != nil {
		failure := providerFailure(operation, sdkgo.FailureAuthentication, "GitHub authorization is unavailable or revoked")
		return Credentials{}, &failure
	}
	return credential, nil
}

func (client *Client) get(call sdkgo.Call, credential Credentials, path string, query url.Values) (providerResponse, error) {
	target := *client.baseURL
	target.Path = strings.TrimRight(target.Path, "/") + path
	target.RawPath = ""
	target.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, target.String(), nil)
	if err != nil {
		return providerResponse{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+credential.AccessToken.Reveal())
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("X-GitHub-Api-Version", client.apiVersion)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, client.maxResponseBytes+1))
	if err != nil {
		return providerResponse{}, err
	}
	if int64(len(body)) > client.maxResponseBytes {
		return providerResponse{statusCode: response.StatusCode, header: response.Header.Clone(), requestID: safeRequestID(response.Header)}, errResponseTooLarge
	}
	return providerResponse{
		statusCode: response.StatusCode, header: response.Header.Clone(), body: body,
		requestID: safeRequestID(response.Header),
	}, nil
}

var errResponseTooLarge = errors.New("GitHub response exceeds the configured size limit")

func (client *Client) classify(operation string, response providerResponse, branches responseBranches) *terminalResponse {
	if response.statusCode >= 200 && response.statusCode < 300 {
		return nil
	}
	if response.statusCode == http.StatusTooManyRequests || isRateLimited(response) {
		return &terminalResponse{
			failure: providerFailure(operation, sdkgo.FailureRateLimit, "GitHub rate limit was reached"),
			retry:   true, delay: retryDelay(response.header, client.now()),
		}
	}
	switch response.statusCode {
	case http.StatusUnauthorized:
		return &terminalResponse{branch: branches.revoked, failure: providerFailure(operation, sdkgo.FailureAuthentication, "GitHub authorization is invalid or revoked")}
	case http.StatusForbidden:
		return &terminalResponse{branch: branches.insufficientScope, failure: providerFailure(operation, sdkgo.FailureAuthorization, "GitHub authorization is forbidden or lacks scope")}
	case http.StatusNotFound:
		return &terminalResponse{branch: branches.notFound, failure: providerFailure(operation, sdkgo.FailureNotFound, "GitHub resource was not found")}
	default:
		if response.statusCode >= 500 {
			return &terminalResponse{failure: providerFailure(operation, sdkgo.FailureAvailability, "GitHub is unavailable"), retry: true}
		}
		return &terminalResponse{branch: branches.failed, failure: providerFailure(operation, sdkgo.FailureProviderRejection, "GitHub rejected the request")}
	}
}

type responseBranches struct {
	insufficientScope sdkgo.BranchID
	revoked           sdkgo.BranchID
	notFound          sdkgo.BranchID
	failed            sdkgo.BranchID
}

func profileBranches() responseBranches {
	return responseBranches{
		insufficientScope: GetAuthenticatedProfileBranchInsufficientScope,
		revoked:           GetAuthenticatedProfileBranchAuthorizationRevoked,
		notFound:          GetAuthenticatedProfileBranchNotFound,
		failed:            GetAuthenticatedProfileBranchFailed,
	}
}

func repositoryBranches() responseBranches {
	return responseBranches{
		insufficientScope: ListPublicRepositoriesBranchInsufficientScope,
		revoked:           ListPublicRepositoriesBranchAuthorizationRevoked,
		notFound:          ListPublicRepositoriesBranchNotFound,
		failed:            ListPublicRepositoriesBranchFailed,
	}
}

func profileTerminalAttempt(terminal terminalResponse, receipt sdkgo.Receipt) sdkgo.QueryAttempt[AuthenticatedProfile] {
	if terminal.retry {
		return sdkgo.NewQueryRetry[AuthenticatedProfile](terminal.failure, terminal.delay)
	}
	return sdkgo.NewQueryBranch(terminal.branch, AuthenticatedProfile{}, &terminal.failure, receipt)
}

func repositoriesTerminalAttempt(terminal terminalResponse, receipt sdkgo.Receipt) sdkgo.QueryAttempt[PublicRepositories] {
	if terminal.retry {
		return sdkgo.NewQueryRetry[PublicRepositories](terminal.failure, terminal.delay)
	}
	return sdkgo.NewQueryBranch(terminal.branch, PublicRepositories{}, &terminal.failure, receipt)
}

func receipt(response providerResponse) sdkgo.Receipt {
	return sdkgo.Receipt{ProviderRequestID: response.requestID}
}

func providerFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "github", Operation: operation, Message: message}
}

func decodeJSON(body []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("response contains trailing JSON")
	}
	return nil
}

func hasRequiredScopes(header http.Header) bool {
	scopes := map[string]bool{}
	for _, scope := range strings.Split(header.Get("X-OAuth-Scopes"), ",") {
		if scope = strings.TrimSpace(scope); scope != "" {
			scopes[scope] = true
		}
	}
	return len(scopes) == 2 && scopes["read:user"] && scopes["user:email"]
}

func primaryVerifiedEmail(emails []githubEmail) string {
	for _, candidate := range emails {
		if !candidate.Primary || !candidate.Verified {
			continue
		}
		email := strings.ToLower(strings.TrimSpace(candidate.Email))
		address, err := mail.ParseAddress(email)
		if err == nil && strings.EqualFold(address.Address, email) && len(email) <= 320 {
			return email
		}
	}
	return ""
}

func normalizeProfile(user githubUser, email string) AuthenticatedProfile {
	return AuthenticatedProfile{
		Subject: strconv.FormatInt(user.ID, 10), Login: boundedString(user.Login, 100), Name: boundedString(user.Name, 256),
		VerifiedEmail: email, AvatarURL: safeURL(user.AvatarURL), ProfileURL: safeURL(user.HTMLURL),
		Company: boundedString(user.Company, 256), BlogURL: safeURL(user.Blog), Location: boundedString(user.Location, 256),
		Bio: boundedString(user.Bio, 1024), TwitterUsername: boundedString(user.TwitterUsername, 100),
		PublicRepos: nonNegative(user.PublicRepos), Followers: nonNegative(user.Followers), Following: nonNegative(user.Following),
		CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt,
	}
}

func normalizeRepository(repository githubRepository) Repository {
	return Repository{
		ID: repository.ID, Name: boundedString(repository.Name, 256), FullName: boundedString(repository.FullName, 512),
		Description: boundedString(repository.Description, 1024), ProfileURL: safeURL(repository.HTMLURL), HomepageURL: safeURL(repository.Homepage),
		Language: boundedString(repository.Language, 128), Topics: boundedStrings(repository.Topics, 100, 64),
		Fork: repository.Fork, Archived: repository.Archived, Disabled: repository.Disabled,
		StargazersCount: nonNegative(repository.StargazersCount), ForksCount: nonNegative(repository.ForksCount),
		OpenIssuesCount: nonNegative(repository.OpenIssuesCount), CreatedAt: repository.CreatedAt,
		UpdatedAt: repository.UpdatedAt, PushedAt: repository.PushedAt,
	}
}

func boundedString(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maximum {
		return value
	}
	value = value[:maximum]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func boundedStrings(values []string, maximumValues, maximumLength int) []string {
	if len(values) > maximumValues {
		values = values[:maximumValues]
	}
	output := make([]string, 0, len(values))
	for _, value := range values {
		if bounded := boundedString(value, maximumLength); bounded != "" {
			output = append(output, bounded)
		}
	}
	return output
}

func safeURL(value string) string {
	value = boundedString(value, 2048)
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" {
		return ""
	}
	return parsed.String()
}

func safeRequestID(header http.Header) string {
	for _, name := range []string{"X-GitHub-Request-Id", "X-Request-Id"} {
		value := strings.TrimSpace(header.Get(name))
		if value != "" && len(value) <= 128 && strings.IndexFunc(value, func(character rune) bool {
			return !(character == '-' || character == '_' || character == '.' || character == ':' || character == '/' || character >= '0' && character <= '9' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z')
		}) == -1 {
			return value
		}
	}
	return ""
}

func isRateLimited(response providerResponse) bool {
	return response.statusCode == http.StatusForbidden && (response.header.Get("Retry-After") != "" || response.header.Get("X-RateLimit-Remaining") == "0")
}

func retryDelay(header http.Header, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(header.Get("Retry-After"), 10, 64); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if reset, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		delay := time.Unix(reset, 0).Sub(now)
		if delay > 0 {
			return delay
		}
	}
	return time.Minute
}

func hasNextPage(header http.Header) bool {
	for _, link := range strings.Split(header.Get("Link"), ",") {
		if strings.Contains(link, `rel="next"`) {
			return true
		}
	}
	return false
}

func nonNegative(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
