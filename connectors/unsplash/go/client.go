// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package unsplash searches public Unsplash photos with a typed Dex Query Step.
package unsplash

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"encoding/json"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option configures a client dependency at construction.
type Option interface{ applyClientOption(*Client) }

// Client holds immutable HTTP settings and resolves credentials for each call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// SearchPhotosInput selects one page of public photos.
type SearchPhotosInput struct {
	// Query is the nonblank search phrase.
	Query string `json:"query"`
	// Page is the 1-based page; zero uses Unsplash's first page.
	Page int `json:"page,omitempty"`
	// PerPage is the page size; zero uses Unsplash's default of ten.
	PerPage int `json:"perPage,omitempty"`
}

// GetPhotoInput identifies a public photo.
type GetPhotoInput struct {
	// ID is the Unsplash photo ID.
	ID string `json:"id"`
}

// Photo contains hotlink and attribution fields for one Unsplash image.
type Photo struct {
	// ID is the stable Unsplash photo ID.
	ID string `json:"id"`
	// Description is the photographer's description, when present.
	Description *string `json:"description,omitempty"`
	// AltDescription is Unsplash's suggested alternative text, when present.
	AltDescription *string `json:"altDescription,omitempty"`
	// HTMLURL is the photo's Unsplash page for attribution.
	HTMLURL string `json:"htmlUrl"`
	// RegularURL is the Unsplash hosted image URL to hotlink.
	RegularURL string `json:"regularUrl"`
	// PhotographerName is the credited photographer.
	PhotographerName string `json:"photographerName"`
	// PhotographerURL is the photographer's Unsplash profile for attribution.
	PhotographerURL string `json:"photographerURL"`
}

// PhotoPage is one search page and its provider counts.
type PhotoPage struct {
	// Total is the number of matching photos.
	Total int `json:"total"`
	// TotalPages is the number of available pages.
	TotalPages int `json:"totalPages"`
	// Photos holds this page's photos.
	Photos []Photo `json:"photos"`
}

// SearchPhotosOperation searches public photos.
type SearchPhotosOperation struct{ client *Client }

// GetPhotoOperation fetches a public photo by ID.
type GetPhotoOperation struct{ client *Client }

type wirePhoto struct {
	ID             string  `json:"id"`
	Description    *string `json:"description"`
	AltDescription *string `json:"alt_description"`
	URLs           struct {
		Regular string `json:"regular"`
	} `json:"urls"`
	Links struct {
		HTML string `json:"html"`
	} `json:"links"`
	User struct {
		Name  string `json:"name"`
		Links struct {
			HTML string `json:"html"`
		} `json:"links"`
	} `json:"user"`
}

// New validates settings and constructs a reusable Unsplash client.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Unsplash credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Unsplash base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// SearchPhotos binds the search operation to this client.
func (client *Client) SearchPhotos() SearchPhotosOperation { return SearchPhotosOperation{client} }

// GetPhoto binds the photo lookup to this client.
func (client *Client) GetPhoto() GetPhotoOperation { return GetPhotoOperation{client} }

// Definition returns the generated search contract.
func (SearchPhotosOperation) Definition() sdkgo.QueryDefinition { return SearchPhotosDefinition }

// Definition returns the generated lookup contract.
func (GetPhotoOperation) Definition() sdkgo.QueryDefinition { return GetPhotoDefinition }

// Invoke searches one page with a header-only access key.
func (operation SearchPhotosOperation) Invoke(call sdkgo.Call, input SearchPhotosInput) sdkgo.QueryAttempt[PhotoPage] {
	const name = "searchPhotos"
	if strings.TrimSpace(input.Query) == "" || input.Page < 0 || input.PerPage < 0 || input.PerPage > 30 {
		return sdkgo.NewQueryBranch(SearchPhotosBranchDefect, PhotoPage{}, unsplashFailure(name, sdkgo.FailureValidation, "query and page size must be valid"), sdkgo.Receipt{})
	}
	query := url.Values{"query": {input.Query}}
	if input.Page > 0 {
		query.Set("page", strconv.Itoa(input.Page))
	}
	if input.PerPage > 0 {
		query.Set("per_page", strconv.Itoa(input.PerPage))
	}
	body, status, failure, retryAfter := operation.client.get(call, "/search/photos?"+query.Encode(), name)
	if failure != nil {
		if status == 0 {
			return sdkgo.NewQueryBranch(SearchPhotosBranchDefect, PhotoPage{}, failure, sdkgo.Receipt{})
		}
		if status < 0 {
			return sdkgo.NewQueryRetry[PhotoPage](*failure, retryAfter)
		}
		return sdkgo.NewQueryBranch(SearchPhotosBranchInvalidResponse, PhotoPage{}, failure, sdkgo.Receipt{})
	}
	if status != http.StatusOK {
		return sdkgo.NewQueryBranch(SearchPhotosBranchProviderRejected, PhotoPage{}, unsplashFailure(name, unsplashFailureKind(status), "Unsplash rejected search with HTTP "+strconv.Itoa(status)), sdkgo.Receipt{})
	}
	var wire struct {
		Total      int         `json:"total"`
		TotalPages int         `json:"total_pages"`
		Results    []wirePhoto `json:"results"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Total < 0 || wire.TotalPages < 0 || wire.Results == nil {
		return sdkgo.NewQueryBranch(SearchPhotosBranchInvalidResponse, PhotoPage{}, unsplashFailure(name, sdkgo.FailureProtocol, "Unsplash returned an invalid search page"), sdkgo.Receipt{})
	}
	page := PhotoPage{Total: wire.Total, TotalPages: wire.TotalPages, Photos: make([]Photo, 0, len(wire.Results))}
	for _, item := range wire.Results {
		photo, valid := photoFromWire(item)
		if !valid {
			return sdkgo.NewQueryBranch(SearchPhotosBranchInvalidResponse, PhotoPage{}, unsplashFailure(name, sdkgo.FailureProtocol, "Unsplash returned an invalid photo"), sdkgo.Receipt{})
		}
		page.Photos = append(page.Photos, photo)
	}
	return sdkgo.NewQueryBranch(SearchPhotosBranchSearched, page, nil, sdkgo.Receipt{})
}

// Invoke retrieves one photo and selects notFound for HTTP 404.
func (operation GetPhotoOperation) Invoke(call sdkgo.Call, input GetPhotoInput) sdkgo.QueryAttempt[Photo] {
	const name = "getPhoto"
	if strings.TrimSpace(input.ID) == "" || strings.ContainsAny(input.ID, "/?&#") {
		return sdkgo.NewQueryBranch(GetPhotoBranchDefect, Photo{}, unsplashFailure(name, sdkgo.FailureValidation, "photo ID is invalid"), sdkgo.Receipt{})
	}
	body, status, failure, retryAfter := operation.client.get(call, "/photos/"+url.PathEscape(input.ID), name)
	if failure != nil {
		if status == 0 {
			return sdkgo.NewQueryBranch(GetPhotoBranchDefect, Photo{}, failure, sdkgo.Receipt{})
		}
		if status < 0 {
			return sdkgo.NewQueryRetry[Photo](*failure, retryAfter)
		}
		return sdkgo.NewQueryBranch(GetPhotoBranchInvalidResponse, Photo{}, failure, sdkgo.Receipt{})
	}
	if status == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetPhotoBranchNotFound, Photo{}, unsplashFailure(name, sdkgo.FailureNotFound, "Unsplash photo was not found"), sdkgo.Receipt{})
	}
	if status != http.StatusOK {
		return sdkgo.NewQueryBranch(GetPhotoBranchProviderRejected, Photo{}, unsplashFailure(name, unsplashFailureKind(status), "Unsplash rejected photo lookup with HTTP "+strconv.Itoa(status)), sdkgo.Receipt{})
	}
	var wire wirePhoto
	if json.Unmarshal(body, &wire) != nil {
		return sdkgo.NewQueryBranch(GetPhotoBranchInvalidResponse, Photo{}, unsplashFailure(name, sdkgo.FailureProtocol, "Unsplash returned malformed photo JSON"), sdkgo.Receipt{})
	}
	photo, valid := photoFromWire(wire)
	if !valid {
		return sdkgo.NewQueryBranch(GetPhotoBranchInvalidResponse, Photo{}, unsplashFailure(name, sdkgo.FailureProtocol, "Unsplash returned an invalid photo"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(GetPhotoBranchFound, photo, nil, sdkgo.Receipt{})
}

func (client *Client) get(call sdkgo.Call, path, operation string) ([]byte, int, *sdkgo.Failure, time.Duration) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.AccessKey.Reveal()) {
		return nil, 0, unsplashFailure(operation, sdkgo.FailureAuthentication, "Unsplash credentials are unavailable or invalid"), 0
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return nil, 0, unsplashFailure(operation, sdkgo.FailureLocalDefect, "Unsplash request could not be built"), 0
	}
	request.Header.Set("Authorization", "Client-ID "+credentials.AccessKey.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, -1, unsplashFailure(operation, sdkgo.FailureTransport, "Unsplash response is unavailable"), 0
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return nil, -1, unsplashFailure(operation, sdkgo.FailureAvailability, "Unsplash is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	}
	if response.StatusCode != http.StatusOK {
		return nil, response.StatusCode, nil, 0
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return nil, 1, unsplashFailure(operation, sdkgo.FailureResponseTooLarge, "Unsplash response exceeded the limit"), 0
	}
	if err != nil {
		return nil, -1, unsplashFailure(operation, sdkgo.FailureAvailability, "Unsplash response was interrupted"), 0
	}
	return body, response.StatusCode, nil, 0
}

func photoFromWire(wire wirePhoto) (Photo, bool) {
	photo := Photo{ID: wire.ID, Description: wire.Description, AltDescription: wire.AltDescription, HTMLURL: wire.Links.HTML, RegularURL: wire.URLs.Regular, PhotographerName: wire.User.Name, PhotographerURL: wire.User.Links.HTML}
	return photo, photo.ID != "" && validHTTPS(photo.HTMLURL) && validHTTPS(photo.RegularURL) && photo.PhotographerName != "" && validHTTPS(photo.PhotographerURL)
}

func validHTTPS(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func unsplashFailureKind(status int) sdkgo.FailureKind {
	if status == http.StatusUnauthorized {
		return sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		return sdkgo.FailureAuthorization
	}
	return sdkgo.FailureProviderRejection
}

func unsplashFailure(operation string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "unsplash", Operation: operation, Message: message}
}
