// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package unsplash

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListPhotosInput selects a page of the public editorial feed.
type ListPhotosInput struct {
	// Page is the 1-based page; zero uses the first page.
	Page    int    `json:"page,omitempty"`
	// PerPage is the page size from 1 to 30; zero uses the provider default.
	PerPage int    `json:"perPage,omitempty"`
	// OrderBy sorts the editorial feed by latest, oldest, or popular; blank uses the provider default.
	OrderBy string `json:"orderBy,omitempty"`
}

// GetRandomPhotoInput narrows the public pool for one random photo.
type GetRandomPhotoInput struct {
	// Query is the search text; required for searches and optional for a random photo.
	Query       string `json:"query,omitempty"`
	// Orientation filters the random photo to landscape, portrait, or squarish; blank allows any.
	Orientation string `json:"orientation,omitempty"`
}

// SearchCollectionsInput searches public collections.
type SearchCollectionsInput struct {
	// Query is the search text; required for searches and optional for a random photo.
	Query   string `json:"query"`
	// Page is the 1-based page; zero uses the first page.
	Page    int    `json:"page,omitempty"`
	// PerPage is the page size from 1 to 30; zero uses the provider default.
	PerPage int    `json:"perPage,omitempty"`
}

// ListCollectionsInput selects a page of public collections.
type ListCollectionsInput struct {
	// Page is the 1-based page; zero uses the first page.
	Page    int `json:"page,omitempty"`
	// PerPage is the page size from 1 to 30; zero uses the provider default.
	PerPage int `json:"perPage,omitempty"`
}

// GetCollectionInput identifies one public collection.
type GetCollectionInput struct {
	// ID identifies the public resource.
	ID string `json:"id"`
}

// ListCollectionPhotosInput selects a page of photos in a public collection.
type ListCollectionPhotosInput struct {
	// ID identifies the public resource.
	ID      string `json:"id"`
	// Page is the 1-based page; zero uses the first page.
	Page    int    `json:"page,omitempty"`
	// PerPage is the page size from 1 to 30; zero uses the provider default.
	PerPage int    `json:"perPage,omitempty"`
}

// GetUserInput identifies a public Unsplash profile.
type GetUserInput struct {
	// Username identifies the public Unsplash profile.
	Username string `json:"username"`
}

// ListUserPhotosInput selects a page of a user's public photos.
type ListUserPhotosInput struct {
	// Username identifies the public Unsplash profile.
	Username string `json:"username"`
	// Page is the 1-based page; zero uses the first page.
	Page     int    `json:"page,omitempty"`
	// PerPage is the page size from 1 to 30; zero uses the provider default.
	PerPage  int    `json:"perPage,omitempty"`
}

// SearchUsersInput searches public Unsplash profiles.
type SearchUsersInput struct {
	// Query is the search text; required for searches and optional for a random photo.
	Query   string `json:"query"`
	// Page is the 1-based page; zero uses the first page.
	Page    int    `json:"page,omitempty"`
	// PerPage is the page size from 1 to 30; zero uses the provider default.
	PerPage int    `json:"perPage,omitempty"`
}

// ListTopicsInput selects a page of public topics.
type ListTopicsInput struct {
	// Page is the 1-based page; zero uses the first page.
	Page    int `json:"page,omitempty"`
	// PerPage is the page size from 1 to 30; zero uses the provider default.
	PerPage int `json:"perPage,omitempty"`
}

// GetTopicInput identifies one public topic by ID or slug.
type GetTopicInput struct {
	// ID identifies the public resource.
	ID string `json:"id"`
}

// ListTopicPhotosInput selects a page of a topic's photos.
type ListTopicPhotosInput struct {
	// ID identifies the public resource.
	ID      string `json:"id"`
	// Page is the 1-based page; zero uses the first page.
	Page    int    `json:"page,omitempty"`
	// PerPage is the page size from 1 to 30; zero uses the provider default.
	PerPage int    `json:"perPage,omitempty"`
}

// PhotoList contains one page of public photos.
type PhotoList struct {
	// Photos contains the public photos in this page.
	Photos []Photo `json:"photos"`
}

// Collection describes one public Unsplash photo collection.
type Collection struct {
	// ID identifies the public resource.
	ID          string  `json:"id"`
	// Title is the public collection or topic title.
	Title       string  `json:"title"`
	// Description is the optional public collection description.
	Description *string `json:"description,omitempty"`
	// HTMLURL is the Unsplash page used for attribution.
	HTMLURL     string  `json:"htmlUrl"`
	// TotalPhotos is the provider's count of collection photos.
	TotalPhotos int     `json:"totalPhotos"`
}

// CollectionList contains one page of public collections.
type CollectionList struct {
	// Collections contains the public collections in this page.
	Collections []Collection `json:"collections"`
}

// CollectionPage contains a search page and its provider counts.
type CollectionPage struct {
	// Total is the total number of search matches.
	Total       int          `json:"total"`
	// TotalPages is the total number of search pages.
	TotalPages  int          `json:"totalPages"`
	// Collections contains the public collections in this page.
	Collections []Collection `json:"collections"`
}

// User describes one public Unsplash profile.
type User struct {
	// ID identifies the public resource.
	ID       string `json:"id"`
	// Username identifies the public Unsplash profile.
	Username string `json:"username"`
	// Name is the user's public display name.
	Name     string `json:"name"`
	// HTMLURL is the Unsplash page used for attribution.
	HTMLURL  string `json:"htmlUrl"`
}

// UserPage contains a profile search page and its provider counts.
type UserPage struct {
	// Total is the total number of search matches.
	Total      int    `json:"total"`
	// TotalPages is the total number of search pages.
	TotalPages int    `json:"totalPages"`
	// Users contains the public profiles in this page.
	Users      []User `json:"users"`
}

// Topic describes one public Unsplash topic.
type Topic struct {
	// ID identifies the public resource.
	ID      string `json:"id"`
	// Slug is the topic's readable URL identifier.
	Slug    string `json:"slug"`
	// Title is the public collection or topic title.
	Title   string `json:"title"`
	// HTMLURL is the Unsplash page used for attribution.
	HTMLURL string `json:"htmlUrl"`
}

// TopicList contains one page of public topics.
type TopicList struct {
	// Topics contains the public topics in this page.
	Topics []Topic `json:"topics"`
}

// ListPhotosOperation reads public Unsplash data through this client.
type ListPhotosOperation struct{ client *Client }
// GetRandomPhotoOperation reads public Unsplash data through this client.
type GetRandomPhotoOperation struct{ client *Client }
// SearchCollectionsOperation reads public Unsplash data through this client.
type SearchCollectionsOperation struct{ client *Client }
// ListCollectionsOperation reads public Unsplash data through this client.
type ListCollectionsOperation struct{ client *Client }
// GetCollectionOperation reads public Unsplash data through this client.
type GetCollectionOperation struct{ client *Client }
// ListCollectionPhotosOperation reads public Unsplash data through this client.
type ListCollectionPhotosOperation struct{ client *Client }
// GetUserOperation reads public Unsplash data through this client.
type GetUserOperation struct{ client *Client }
// ListUserPhotosOperation reads public Unsplash data through this client.
type ListUserPhotosOperation struct{ client *Client }
// SearchUsersOperation reads public Unsplash data through this client.
type SearchUsersOperation struct{ client *Client }
// ListTopicsOperation reads public Unsplash data through this client.
type ListTopicsOperation struct{ client *Client }
// GetTopicOperation reads public Unsplash data through this client.
type GetTopicOperation struct{ client *Client }
// ListTopicPhotosOperation reads public Unsplash data through this client.
type ListTopicPhotosOperation struct{ client *Client }

type publicQueryBranches struct{ happy, notFound, providerRejected, invalidResponse, defect sdkgo.BranchID }

func validPage(page, perPage int) bool { return page >= 0 && perPage >= 0 && perPage <= 30 }
func validPathID(id string) bool {
	return id != "" && strings.TrimSpace(id) == id && !strings.ContainsAny(id, "/?&#")
}
func pageQuery(page, perPage int) url.Values {
	q := url.Values{}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	if perPage > 0 {
		q.Set("per_page", strconv.Itoa(perPage))
	}
	return q
}
func queryPath(path string, q url.Values) string {
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

func runPublicQuery[T any](client *Client, call sdkgo.Call, operation, path string, branches publicQueryBranches, decode func([]byte) (T, bool)) sdkgo.QueryAttempt[T] {
	var zero T
	body, status, failure, retryAfter := client.get(call, path, operation)
	if failure != nil {
		switch {
		case status == 0:
			return sdkgo.NewQueryBranch(branches.defect, zero, failure, sdkgo.Receipt{})
		case status < 0:
			return sdkgo.NewQueryRetry[T](*failure, retryAfter)
		default:
			return sdkgo.NewQueryBranch(branches.invalidResponse, zero, failure, sdkgo.Receipt{})
		}
	}
	if status == http.StatusNotFound {
		return sdkgo.NewQueryBranch(branches.notFound, zero, unsplashFailure(operation, sdkgo.FailureNotFound, "Unsplash resource was not found"), sdkgo.Receipt{})
	}
	if status != http.StatusOK {
		return sdkgo.NewQueryBranch(branches.providerRejected, zero, unsplashFailure(operation, unsplashFailureKind(status), "Unsplash rejected the request with HTTP "+strconv.Itoa(status)), sdkgo.Receipt{})
	}
	value, valid := decode(body)
	if !valid {
		return sdkgo.NewQueryBranch(branches.invalidResponse, zero, unsplashFailure(operation, sdkgo.FailureProtocol, "Unsplash returned an invalid response"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(branches.happy, value, nil, sdkgo.Receipt{})
}

func invalidPublicInput[T any](operation string, branch sdkgo.BranchID) sdkgo.QueryAttempt[T] {
	var zero T
	return sdkgo.NewQueryBranch(branch, zero, unsplashFailure(operation, sdkgo.FailureValidation, "Unsplash request input is invalid"), sdkgo.Receipt{})
}

func decodePhoto(body []byte) (Photo, bool) {
	var wire wirePhoto
	if json.Unmarshal(body, &wire) != nil {
		return Photo{}, false
	}
	return photoFromWire(wire)
}
func decodePhotoList(body []byte) (PhotoList, bool) {
	var wire []wirePhoto
	if json.Unmarshal(body, &wire) != nil || wire == nil {
		return PhotoList{}, false
	}
	out := PhotoList{Photos: make([]Photo, 0, len(wire))}
	for _, item := range wire {
		photo, ok := photoFromWire(item)
		if !ok {
			return PhotoList{}, false
		}
		out.Photos = append(out.Photos, photo)
	}
	return out, true
}

type wireCollection struct {
	ID          json.RawMessage `json:"id"`
	Title       string          `json:"title"`
	Description *string         `json:"description"`
	TotalPhotos int             `json:"total_photos"`
	Links       struct {
		HTML string `json:"html"`
	} `json:"links"`
}

func (wire wireCollection) convert() (Collection, bool) {
	id := strings.Trim(string(wire.ID), `"`)
	value := Collection{ID: id, Title: wire.Title, Description: wire.Description, HTMLURL: wire.Links.HTML, TotalPhotos: wire.TotalPhotos}
	return value, id != "" && wire.Title != "" && wire.TotalPhotos >= 0 && validHTTPS(wire.Links.HTML)
}
func decodeCollection(body []byte) (Collection, bool) {
	var wire wireCollection
	if json.Unmarshal(body, &wire) != nil {
		return Collection{}, false
	}
	return wire.convert()
}
func decodeCollectionList(body []byte) (CollectionList, bool) {
	var wire []wireCollection
	if json.Unmarshal(body, &wire) != nil || wire == nil {
		return CollectionList{}, false
	}
	out := CollectionList{Collections: make([]Collection, 0, len(wire))}
	for _, item := range wire {
		value, ok := item.convert()
		if !ok {
			return CollectionList{}, false
		}
		out.Collections = append(out.Collections, value)
	}
	return out, true
}
func decodeCollectionPage(body []byte) (CollectionPage, bool) {
	var wire struct {
		Total      int              `json:"total"`
		TotalPages int              `json:"total_pages"`
		Results    []wireCollection `json:"results"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Results == nil || wire.Total < 0 || wire.TotalPages < 0 {
		return CollectionPage{}, false
	}
	out := CollectionPage{Total: wire.Total, TotalPages: wire.TotalPages, Collections: make([]Collection, 0, len(wire.Results))}
	for _, item := range wire.Results {
		value, ok := item.convert()
		if !ok {
			return CollectionPage{}, false
		}
		out.Collections = append(out.Collections, value)
	}
	return out, true
}

type wireUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Links    struct {
		HTML string `json:"html"`
	} `json:"links"`
}

func (wire wireUser) convert() (User, bool) {
	value := User{ID: wire.ID, Username: wire.Username, Name: wire.Name, HTMLURL: wire.Links.HTML}
	return value, value.ID != "" && value.Username != "" && value.Name != "" && validHTTPS(value.HTMLURL)
}
func decodeUser(body []byte) (User, bool) {
	var wire wireUser
	if json.Unmarshal(body, &wire) != nil {
		return User{}, false
	}
	return wire.convert()
}
func decodeUserPage(body []byte) (UserPage, bool) {
	var wire struct {
		Total      int        `json:"total"`
		TotalPages int        `json:"total_pages"`
		Results    []wireUser `json:"results"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Results == nil || wire.Total < 0 || wire.TotalPages < 0 {
		return UserPage{}, false
	}
	out := UserPage{Total: wire.Total, TotalPages: wire.TotalPages, Users: make([]User, 0, len(wire.Results))}
	for _, item := range wire.Results {
		value, ok := item.convert()
		if !ok {
			return UserPage{}, false
		}
		out.Users = append(out.Users, value)
	}
	return out, true
}

type wireTopic struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
	Links struct {
		HTML string `json:"html"`
	} `json:"links"`
}

func (wire wireTopic) convert() (Topic, bool) {
	value := Topic{ID: wire.ID, Slug: wire.Slug, Title: wire.Title, HTMLURL: wire.Links.HTML}
	return value, value.ID != "" && value.Slug != "" && value.Title != "" && validHTTPS(value.HTMLURL)
}
func decodeTopic(body []byte) (Topic, bool) {
	var wire wireTopic
	if json.Unmarshal(body, &wire) != nil {
		return Topic{}, false
	}
	return wire.convert()
}
func decodeTopicList(body []byte) (TopicList, bool) {
	var wire []wireTopic
	if json.Unmarshal(body, &wire) != nil || wire == nil {
		return TopicList{}, false
	}
	out := TopicList{Topics: make([]Topic, 0, len(wire))}
	for _, item := range wire {
		value, ok := item.convert()
		if !ok {
			return TopicList{}, false
		}
		out.Topics = append(out.Topics, value)
	}
	return out, true
}

// ListPhotos binds the listPhotos Query to this client.
func (client *Client) ListPhotos() ListPhotosOperation { return ListPhotosOperation{client} }

// Definition returns the generated listPhotos contract.
func (ListPhotosOperation) Definition() sdkgo.QueryDefinition { return ListPhotosDefinition }

// Invoke reads public Unsplash data for listPhotos.
func (operation ListPhotosOperation) Invoke(call sdkgo.Call, input ListPhotosInput) sdkgo.QueryAttempt[PhotoList] {
	const name = "listPhotos"
	if !(validPage(input.Page, input.PerPage) && (input.OrderBy == "" || input.OrderBy == "latest" || input.OrderBy == "oldest" || input.OrderBy == "popular")) {
		return invalidPublicInput[PhotoList](name, ListPhotosBranchDefect)
	}
	branches := publicQueryBranches{happy: ListPhotosBranchListed, notFound: ListPhotosBranchNotFound, providerRejected: ListPhotosBranchProviderRejected, invalidResponse: ListPhotosBranchInvalidResponse, defect: ListPhotosBranchDefect}
	return runPublicQuery(operation.client, call, name, func() string {
		q := pageQuery(input.Page, input.PerPage)
		if input.OrderBy != "" {
			q.Set("order_by", input.OrderBy)
		}
		return queryPath("/photos", q)
	}(), branches, decodePhotoList)
}

// GetRandomPhoto binds the getRandomPhoto Query to this client.
func (client *Client) GetRandomPhoto() GetRandomPhotoOperation {
	return GetRandomPhotoOperation{client}
}

// Definition returns the generated getRandomPhoto contract.
func (GetRandomPhotoOperation) Definition() sdkgo.QueryDefinition { return GetRandomPhotoDefinition }

// Invoke reads public Unsplash data for getRandomPhoto.
func (operation GetRandomPhotoOperation) Invoke(call sdkgo.Call, input GetRandomPhotoInput) sdkgo.QueryAttempt[Photo] {
	const name = "getRandomPhoto"
	if !(input.Orientation == "" || input.Orientation == "landscape" || input.Orientation == "portrait" || input.Orientation == "squarish") {
		return invalidPublicInput[Photo](name, GetRandomPhotoBranchDefect)
	}
	branches := publicQueryBranches{happy: GetRandomPhotoBranchFound, notFound: GetRandomPhotoBranchNotFound, providerRejected: GetRandomPhotoBranchProviderRejected, invalidResponse: GetRandomPhotoBranchInvalidResponse, defect: GetRandomPhotoBranchDefect}
	return runPublicQuery(operation.client, call, name, func() string {
		q := url.Values{}
		if input.Query != "" {
			q.Set("query", input.Query)
		}
		if input.Orientation != "" {
			q.Set("orientation", input.Orientation)
		}
		return queryPath("/photos/random", q)
	}(), branches, decodePhoto)
}

// SearchCollections binds the searchCollections Query to this client.
func (client *Client) SearchCollections() SearchCollectionsOperation {
	return SearchCollectionsOperation{client}
}

// Definition returns the generated searchCollections contract.
func (SearchCollectionsOperation) Definition() sdkgo.QueryDefinition {
	return SearchCollectionsDefinition
}

// Invoke reads public Unsplash data for searchCollections.
func (operation SearchCollectionsOperation) Invoke(call sdkgo.Call, input SearchCollectionsInput) sdkgo.QueryAttempt[CollectionPage] {
	const name = "searchCollections"
	if !(strings.TrimSpace(input.Query) != "" && validPage(input.Page, input.PerPage)) {
		return invalidPublicInput[CollectionPage](name, SearchCollectionsBranchDefect)
	}
	branches := publicQueryBranches{happy: SearchCollectionsBranchSearched, notFound: SearchCollectionsBranchNotFound, providerRejected: SearchCollectionsBranchProviderRejected, invalidResponse: SearchCollectionsBranchInvalidResponse, defect: SearchCollectionsBranchDefect}
	return runPublicQuery(operation.client, call, name, func() string {
		q := pageQuery(input.Page, input.PerPage)
		q.Set("query", input.Query)
		return queryPath("/search/collections", q)
	}(), branches, decodeCollectionPage)
}

// ListCollections binds the listCollections Query to this client.
func (client *Client) ListCollections() ListCollectionsOperation {
	return ListCollectionsOperation{client}
}

// Definition returns the generated listCollections contract.
func (ListCollectionsOperation) Definition() sdkgo.QueryDefinition { return ListCollectionsDefinition }

// Invoke reads public Unsplash data for listCollections.
func (operation ListCollectionsOperation) Invoke(call sdkgo.Call, input ListCollectionsInput) sdkgo.QueryAttempt[CollectionList] {
	const name = "listCollections"
	if !(validPage(input.Page, input.PerPage)) {
		return invalidPublicInput[CollectionList](name, ListCollectionsBranchDefect)
	}
	branches := publicQueryBranches{happy: ListCollectionsBranchListed, notFound: ListCollectionsBranchNotFound, providerRejected: ListCollectionsBranchProviderRejected, invalidResponse: ListCollectionsBranchInvalidResponse, defect: ListCollectionsBranchDefect}
	return runPublicQuery(operation.client, call, name, queryPath("/collections", pageQuery(input.Page, input.PerPage)), branches, decodeCollectionList)
}

// GetCollection binds the getCollection Query to this client.
func (client *Client) GetCollection() GetCollectionOperation { return GetCollectionOperation{client} }

// Definition returns the generated getCollection contract.
func (GetCollectionOperation) Definition() sdkgo.QueryDefinition { return GetCollectionDefinition }

// Invoke reads public Unsplash data for getCollection.
func (operation GetCollectionOperation) Invoke(call sdkgo.Call, input GetCollectionInput) sdkgo.QueryAttempt[Collection] {
	const name = "getCollection"
	if !(validPathID(input.ID)) {
		return invalidPublicInput[Collection](name, GetCollectionBranchDefect)
	}
	branches := publicQueryBranches{happy: GetCollectionBranchFound, notFound: GetCollectionBranchNotFound, providerRejected: GetCollectionBranchProviderRejected, invalidResponse: GetCollectionBranchInvalidResponse, defect: GetCollectionBranchDefect}
	return runPublicQuery(operation.client, call, name, "/collections/"+url.PathEscape(input.ID), branches, decodeCollection)
}

// ListCollectionPhotos binds the listCollectionPhotos Query to this client.
func (client *Client) ListCollectionPhotos() ListCollectionPhotosOperation {
	return ListCollectionPhotosOperation{client}
}

// Definition returns the generated listCollectionPhotos contract.
func (ListCollectionPhotosOperation) Definition() sdkgo.QueryDefinition {
	return ListCollectionPhotosDefinition
}

// Invoke reads public Unsplash data for listCollectionPhotos.
func (operation ListCollectionPhotosOperation) Invoke(call sdkgo.Call, input ListCollectionPhotosInput) sdkgo.QueryAttempt[PhotoList] {
	const name = "listCollectionPhotos"
	if !(validPathID(input.ID) && validPage(input.Page, input.PerPage)) {
		return invalidPublicInput[PhotoList](name, ListCollectionPhotosBranchDefect)
	}
	branches := publicQueryBranches{happy: ListCollectionPhotosBranchListed, notFound: ListCollectionPhotosBranchNotFound, providerRejected: ListCollectionPhotosBranchProviderRejected, invalidResponse: ListCollectionPhotosBranchInvalidResponse, defect: ListCollectionPhotosBranchDefect}
	return runPublicQuery(operation.client, call, name, queryPath("/collections/"+url.PathEscape(input.ID)+"/photos", pageQuery(input.Page, input.PerPage)), branches, decodePhotoList)
}

// GetUser binds the getUser Query to this client.
func (client *Client) GetUser() GetUserOperation { return GetUserOperation{client} }

// Definition returns the generated getUser contract.
func (GetUserOperation) Definition() sdkgo.QueryDefinition { return GetUserDefinition }

// Invoke reads public Unsplash data for getUser.
func (operation GetUserOperation) Invoke(call sdkgo.Call, input GetUserInput) sdkgo.QueryAttempt[User] {
	const name = "getUser"
	if !(validPathID(input.Username)) {
		return invalidPublicInput[User](name, GetUserBranchDefect)
	}
	branches := publicQueryBranches{happy: GetUserBranchFound, notFound: GetUserBranchNotFound, providerRejected: GetUserBranchProviderRejected, invalidResponse: GetUserBranchInvalidResponse, defect: GetUserBranchDefect}
	return runPublicQuery(operation.client, call, name, "/users/"+url.PathEscape(input.Username), branches, decodeUser)
}

// ListUserPhotos binds the listUserPhotos Query to this client.
func (client *Client) ListUserPhotos() ListUserPhotosOperation {
	return ListUserPhotosOperation{client}
}

// Definition returns the generated listUserPhotos contract.
func (ListUserPhotosOperation) Definition() sdkgo.QueryDefinition { return ListUserPhotosDefinition }

// Invoke reads public Unsplash data for listUserPhotos.
func (operation ListUserPhotosOperation) Invoke(call sdkgo.Call, input ListUserPhotosInput) sdkgo.QueryAttempt[PhotoList] {
	const name = "listUserPhotos"
	if !(validPathID(input.Username) && validPage(input.Page, input.PerPage)) {
		return invalidPublicInput[PhotoList](name, ListUserPhotosBranchDefect)
	}
	branches := publicQueryBranches{happy: ListUserPhotosBranchListed, notFound: ListUserPhotosBranchNotFound, providerRejected: ListUserPhotosBranchProviderRejected, invalidResponse: ListUserPhotosBranchInvalidResponse, defect: ListUserPhotosBranchDefect}
	return runPublicQuery(operation.client, call, name, queryPath("/users/"+url.PathEscape(input.Username)+"/photos", pageQuery(input.Page, input.PerPage)), branches, decodePhotoList)
}

// SearchUsers binds the searchUsers Query to this client.
func (client *Client) SearchUsers() SearchUsersOperation { return SearchUsersOperation{client} }

// Definition returns the generated searchUsers contract.
func (SearchUsersOperation) Definition() sdkgo.QueryDefinition { return SearchUsersDefinition }

// Invoke reads public Unsplash data for searchUsers.
func (operation SearchUsersOperation) Invoke(call sdkgo.Call, input SearchUsersInput) sdkgo.QueryAttempt[UserPage] {
	const name = "searchUsers"
	if !(strings.TrimSpace(input.Query) != "" && validPage(input.Page, input.PerPage)) {
		return invalidPublicInput[UserPage](name, SearchUsersBranchDefect)
	}
	branches := publicQueryBranches{happy: SearchUsersBranchSearched, notFound: SearchUsersBranchNotFound, providerRejected: SearchUsersBranchProviderRejected, invalidResponse: SearchUsersBranchInvalidResponse, defect: SearchUsersBranchDefect}
	return runPublicQuery(operation.client, call, name, func() string {
		q := pageQuery(input.Page, input.PerPage)
		q.Set("query", input.Query)
		return queryPath("/search/users", q)
	}(), branches, decodeUserPage)
}

// ListTopics binds the listTopics Query to this client.
func (client *Client) ListTopics() ListTopicsOperation { return ListTopicsOperation{client} }

// Definition returns the generated listTopics contract.
func (ListTopicsOperation) Definition() sdkgo.QueryDefinition { return ListTopicsDefinition }

// Invoke reads public Unsplash data for listTopics.
func (operation ListTopicsOperation) Invoke(call sdkgo.Call, input ListTopicsInput) sdkgo.QueryAttempt[TopicList] {
	const name = "listTopics"
	if !(validPage(input.Page, input.PerPage)) {
		return invalidPublicInput[TopicList](name, ListTopicsBranchDefect)
	}
	branches := publicQueryBranches{happy: ListTopicsBranchListed, notFound: ListTopicsBranchNotFound, providerRejected: ListTopicsBranchProviderRejected, invalidResponse: ListTopicsBranchInvalidResponse, defect: ListTopicsBranchDefect}
	return runPublicQuery(operation.client, call, name, queryPath("/topics", pageQuery(input.Page, input.PerPage)), branches, decodeTopicList)
}

// GetTopic binds the getTopic Query to this client.
func (client *Client) GetTopic() GetTopicOperation { return GetTopicOperation{client} }

// Definition returns the generated getTopic contract.
func (GetTopicOperation) Definition() sdkgo.QueryDefinition { return GetTopicDefinition }

// Invoke reads public Unsplash data for getTopic.
func (operation GetTopicOperation) Invoke(call sdkgo.Call, input GetTopicInput) sdkgo.QueryAttempt[Topic] {
	const name = "getTopic"
	if !(validPathID(input.ID)) {
		return invalidPublicInput[Topic](name, GetTopicBranchDefect)
	}
	branches := publicQueryBranches{happy: GetTopicBranchFound, notFound: GetTopicBranchNotFound, providerRejected: GetTopicBranchProviderRejected, invalidResponse: GetTopicBranchInvalidResponse, defect: GetTopicBranchDefect}
	return runPublicQuery(operation.client, call, name, "/topics/"+url.PathEscape(input.ID), branches, decodeTopic)
}

// ListTopicPhotos binds the listTopicPhotos Query to this client.
func (client *Client) ListTopicPhotos() ListTopicPhotosOperation {
	return ListTopicPhotosOperation{client}
}

// Definition returns the generated listTopicPhotos contract.
func (ListTopicPhotosOperation) Definition() sdkgo.QueryDefinition { return ListTopicPhotosDefinition }

// Invoke reads public Unsplash data for listTopicPhotos.
func (operation ListTopicPhotosOperation) Invoke(call sdkgo.Call, input ListTopicPhotosInput) sdkgo.QueryAttempt[PhotoList] {
	const name = "listTopicPhotos"
	if !(validPathID(input.ID) && validPage(input.Page, input.PerPage)) {
		return invalidPublicInput[PhotoList](name, ListTopicPhotosBranchDefect)
	}
	branches := publicQueryBranches{happy: ListTopicPhotosBranchListed, notFound: ListTopicPhotosBranchNotFound, providerRejected: ListTopicPhotosBranchProviderRejected, invalidResponse: ListTopicPhotosBranchInvalidResponse, defect: ListTopicPhotosBranchDefect}
	return runPublicQuery(operation.client, call, name, queryPath("/topics/"+url.PathEscape(input.ID)+"/photos", pageQuery(input.Page, input.PerPage)), branches, decodePhotoList)
}
