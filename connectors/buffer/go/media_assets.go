// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package buffer

import (
	"errors"
	"net/url"
	"strings"
)

// PostAsset references one publicly hosted image, video, or document.
type PostAsset struct {
	// Type is image, video, or document.
	Type string `json:"type"`
	// URL is a direct public HTTPS file URL without credentials or a query string.
	URL string `json:"url"`
	// ThumbnailURL is optional for images and required for documents.
	ThumbnailURL string `json:"thumbnailUrl,omitempty"`
	// Title is required for documents.
	Title string `json:"title,omitempty"`
	// ThumbnailOffsetMs selects a video thumbnail frame in milliseconds.
	ThumbnailOffsetMs *int `json:"thumbnailOffsetMs,omitempty"`
}

func bufferPostAssetInputs(assets []PostAsset) ([]map[string]any, error) {
	if assets == nil {
		return nil, nil
	}
	result := make([]map[string]any, 0, len(assets))
	for _, asset := range assets {
		if !validPublicMediaURL(asset.URL) {
			return nil, errors.New("media URL must be public HTTPS without credentials or query")
		}
		switch asset.Type {
		case "image":
			if asset.Title != "" || asset.ThumbnailOffsetMs != nil {
				return nil, errors.New("image asset fields are invalid")
			}
			image := map[string]any{"url": asset.URL}
			if asset.ThumbnailURL != "" {
				if !validPublicMediaURL(asset.ThumbnailURL) {
					return nil, errors.New("image thumbnail URL is invalid")
				}
				image["thumbnailUrl"] = asset.ThumbnailURL
			}
			result = append(result, map[string]any{"image": image})
		case "video":
			if asset.Title != "" || asset.ThumbnailURL != "" {
				return nil, errors.New("video asset fields are invalid")
			}
			video := map[string]any{"url": asset.URL}
			if asset.ThumbnailOffsetMs != nil {
				if *asset.ThumbnailOffsetMs < 0 {
					return nil, errors.New("video thumbnail offset is invalid")
				}
				video["metadata"] = map[string]any{"thumbnailOffset": *asset.ThumbnailOffsetMs}
			}
			result = append(result, map[string]any{"video": video})
		case "document":
			if !validPublicMediaURL(asset.ThumbnailURL) || strings.TrimSpace(asset.Title) == "" || asset.ThumbnailOffsetMs != nil {
				return nil, errors.New("document asset fields are invalid")
			}
			result = append(result, map[string]any{"document": map[string]any{"url": asset.URL, "thumbnailUrl": asset.ThumbnailURL, "title": asset.Title}})
		default:
			return nil, errors.New("media asset type is invalid")
		}
	}
	return result, nil
}

func validPublicMediaURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && !strings.ContainsAny(value, "\r\n")
}
