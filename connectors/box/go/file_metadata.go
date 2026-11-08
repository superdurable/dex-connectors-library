// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package box

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

type fileMetadata struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
}

func (metadata fileMetadata) matches(fileID string, digest string, size int64) bool {
	return metadata.ID == fileID && metadata.Type == "file" && metadata.Name != "" && strings.EqualFold(metadata.SHA1, digest) && metadata.Size == size
}

func (client *Client) getFileMetadata(call sdkgo.Call, fileID string) fileResponse {
	accessToken, err := client.resolveAccessToken(call)
	if err != nil {
		return fileResponse{err: err}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+"/2.0/files/"+url.PathEscape(fileID)+"?fields=id,type,name,sha1,size", nil)
	if err != nil {
		return fileResponse{err: errRequest}
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	return client.sendMetadataRequest(request, accessToken)
}

func (client *Client) sendMetadataRequest(request *http.Request, accessToken string) fileResponse {
	response, err := client.httpClient.Do(request)
	if err != nil {
		return fileResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if bytes.Contains(body, []byte(accessToken)) {
		return fileResponse{status: response.StatusCode, retryAfter: retryAfter, err: errCredential}
	}
	return fileResponse{body: body, status: response.StatusCode, retryAfter: retryAfter, err: err}
}

func decodeFileMetadata(body []byte, fileID string) (fileMetadata, bool) {
	var metadata fileMetadata
	if json.Unmarshal(body, &metadata) != nil || metadata.ID != fileID || metadata.Type != "file" || metadata.Name == "" || metadata.Size < 0 || len(metadata.SHA1) != 40 {
		return fileMetadata{}, false
	}
	for _, character := range metadata.SHA1 {
		if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
			return fileMetadata{}, false
		}
	}
	return metadata, true
}
