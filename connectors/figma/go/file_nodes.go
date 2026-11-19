// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package figma

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var figmaNodeID = regexp.MustCompile(`^[0-9]+:[0-9]+$`)

// GetFileNodesInput selects up to 100 node IDs from a file or branch key.
type GetFileNodesInput struct {
	FileKey string   `json:"fileKey"`
	NodeIDs []string `json:"nodeIds"`
}

// FileNode contains one selected Figma document node.
type FileNode struct {
	Document json.RawMessage `json:"document"`
}

// FileNodes contains the selected node subtrees and file metadata.
type FileNodes struct {
	Name         string              `json:"name"`
	LastModified string              `json:"lastModified"`
	Nodes        map[string]FileNode `json:"nodes"`
}

// GetFileNodesOperation reads selected file nodes through Figma's file API.
type GetFileNodesOperation struct{ client *Client }

// GetFileNodes binds the query to this client.
func (client *Client) GetFileNodes() GetFileNodesOperation { return GetFileNodesOperation{client} }

// Definition returns the manifest-generated operation contract.
func (GetFileNodesOperation) Definition() sdkgo.QueryDefinition { return GetFileNodesDefinition }

// Invoke reads selected Figma document nodes without changing the file.
func (operation GetFileNodesOperation) Invoke(call sdkgo.Call, input GetFileNodesInput) sdkgo.QueryAttempt[FileNodes] {
	if !safeKey.MatchString(input.FileKey) || len(input.NodeIDs) == 0 || len(input.NodeIDs) > 100 {
		return failedBranch[FileNodes]("getFileNodes", GetFileNodesBranchDefect, sdkgo.FailureValidation, "fileKey or nodeIds is invalid")
	}
	for _, nodeID := range input.NodeIDs {
		if !figmaNodeID.MatchString(nodeID) {
			return failedBranch[FileNodes]("getFileNodes", GetFileNodesBranchDefect, sdkgo.FailureValidation, "nodeIds contains an invalid ID")
		}
	}
	path := "/files/" + input.FileKey + "/nodes?ids=" + url.QueryEscape(strings.Join(input.NodeIDs, ","))
	return invokeGet(operation.client, call, "getFileNodes", path, GetFileNodesBranchFound, GetFileNodesBranchNotFound, decodeFileNodes)
}

func decodeFileNodes(body []byte) (FileNodes, error) {
	var result FileNodes
	if err := json.Unmarshal(body, &result); err != nil {
		return FileNodes{}, err
	}
	if result.Name == "" || result.Nodes == nil {
		return FileNodes{}, fmt.Errorf("incomplete Figma nodes")
	}
	for nodeID, node := range result.Nodes {
		if !figmaNodeID.MatchString(nodeID) || len(node.Document) == 0 || string(node.Document) == "null" {
			return FileNodes{}, fmt.Errorf("incomplete Figma node")
		}
	}
	return result, nil
}
