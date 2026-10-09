// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// MinimumPagedDefaultPages is the fewest pages a paginated query's default mock serves, so an application test
// that reads only the first page, or stops one page early, does not pass.
const MinimumPagedDefaultPages = 3

// FailureKinds lists the Connector SDK failure kinds a mock failure may name, in the SDK's wire spelling.
var FailureKinds = []string{
	"VALIDATION", "AUTHENTICATION", "AUTHORIZATION", "NOT_FOUND", "CONFLICT", "RATE_LIMIT", "QUOTA_EXHAUSTED",
	"AVAILABILITY", "PROVIDER_REJECTION", "TRANSPORT", "RESPONSE_TOO_LARGE", "PROTOCOL", "LOCAL_DEFECT",
}

// Pagination names the JSON fields that carry a listing's cursor: InputField in the operation input and
// NextField in the operation output. A zero, empty, or absent NextField ends the listing.
type Pagination struct {
	InputField string `yaml:"inputField" json:"inputField"`
	NextField  string `yaml:"nextField" json:"nextField"`
}

// OperationMock is one maintained provider outcome that the connector's generated mock package offers to
// application tests. Exactly one shape applies: a branch mock (Branch with Output, Pages, or Failure), a Retry,
// or an Uncertain outcome.
type OperationMock struct {
	Name        string         `yaml:"name" json:"name"`
	Branch      string         `yaml:"branch,omitempty" json:"branch,omitempty"`
	Description string         `yaml:"description" json:"description"`
	Output      *MockValue     `yaml:"output,omitempty" json:"output,omitempty"`
	Pages       []MockValue    `yaml:"pages,omitempty" json:"pages,omitempty"`
	Failure     *MockFailure   `yaml:"failure,omitempty" json:"failure,omitempty"`
	Retry       *MockRetry     `yaml:"retry,omitempty" json:"retry,omitempty"`
	Uncertain   *MockUncertain `yaml:"uncertain,omitempty" json:"uncertain,omitempty"`
	// Default makes the mock answer every call that a test does not script.
	Default bool `yaml:"default,omitempty" json:"default,omitempty"`
}

// MockFailure is the safe failure a mock returns. The mock fills the provider and operation.
type MockFailure struct {
	Kind    string `yaml:"kind" json:"kind"`
	Message string `yaml:"message" json:"message"`
}

// MockRetry asks Dex to retry the Step, after After when it is set.
type MockRetry struct {
	Failure MockFailure `yaml:"failure" json:"failure"`
	After   string      `yaml:"after,omitempty" json:"after,omitempty"`
}

// MockUncertain is a dispatched mutation whose outcome cannot be confirmed.
type MockUncertain struct {
	Output  *MockValue  `yaml:"output,omitempty" json:"output,omitempty"`
	Failure MockFailure `yaml:"failure" json:"failure"`
}

// MockValue is an operation output written in YAML and kept as compact JSON. Quoted scalars and timestamps stay
// JSON strings, so a value decodes into the connector's Go type exactly as written.
type MockValue struct {
	encoded json.RawMessage
}

// UnmarshalYAML converts the YAML value to compact JSON in document order.
func (value *MockValue) UnmarshalYAML(node *yaml.Node) error {
	var encoded bytes.Buffer
	if err := writeYAMLNodeAsJSON(&encoded, node); err != nil {
		return err
	}
	value.encoded = encoded.Bytes()
	return nil
}

// MarshalJSON returns the compact JSON value.
func (value MockValue) MarshalJSON() ([]byte, error) {
	return value.encoded, nil
}

// UnmarshalJSON keeps a JSON value as written.
func (value *MockValue) UnmarshalJSON(encoded []byte) error {
	var compact bytes.Buffer
	if err := json.Compact(&compact, encoded); err != nil {
		return err
	}
	value.encoded = compact.Bytes()
	return nil
}

// JSON returns the compact JSON value.
func (value MockValue) JSON() string {
	return string(value.encoded)
}

// GoName returns the exported Go identifier that generated mock constructors use for the mock.
func (mock OperationMock) GoName() string {
	return strings.ToUpper(mock.Name[:1]) + mock.Name[1:]
}

// IsBranchMock reports whether the mock selects a declared branch rather than a Retry or an uncertain outcome.
func (mock OperationMock) IsBranchMock() bool {
	return mock.Retry == nil && mock.Uncertain == nil
}

// HasMocks reports whether any operation declares a mock. A connector with mocks generates a mock package and
// must cover every operation and branch; see ValidateMockCoverage.
func (manifest Manifest) HasMocks() bool {
	for _, operation := range manifest.Spec.Operations {
		if len(operation.Mocks) > 0 {
			return true
		}
	}
	return false
}

// ValidateMockCoverage checks that a connector with mocks covers its whole surface: every operation declares
// mocks, every declared branch has one, every query has exactly one default, and a paginated query's default
// serves at least MinimumPagedDefaultPages pages. A connector without mocks passes.
func (manifest Manifest) ValidateMockCoverage() error {
	if !manifest.HasMocks() {
		return nil
	}
	var problems []string
	for _, operation := range manifest.Spec.Operations {
		covered := make(map[string]bool, len(operation.Branches))
		defaults := 0
		for _, mock := range operation.Mocks {
			switch {
			case mock.Uncertain != nil:
				covered["uncertain"] = true
			case mock.IsBranchMock():
				covered[mock.Branch] = true
			}
			if mock.Default {
				defaults++
				if operation.Pagination != nil && len(mock.Pages) < MinimumPagedDefaultPages {
					problems = append(problems, fmt.Sprintf("%s: paginated default mock %s must serve at least %d pages", operation.Name, mock.Name, MinimumPagedDefaultPages))
				}
			}
		}
		for _, branch := range operation.Branches {
			if !covered[branch.ID] {
				problems = append(problems, operation.Name+": branch "+branch.ID+" needs a mock")
			}
		}
		if operation.Kind == "query" && defaults != 1 {
			problems = append(problems, operation.Name+": a query needs exactly one default mock")
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("incomplete connector mocks: %s", strings.Join(problems, "; "))
	}
	return nil
}

func validateOperationMocks(operation Operation) []string {
	var problems []string
	prefix := operation.Name + ": "
	if operation.Pagination != nil {
		if !fieldNamePattern.MatchString(operation.Pagination.InputField) || !fieldNamePattern.MatchString(operation.Pagination.NextField) {
			problems = append(problems, prefix+"pagination inputField and nextField must be JSON field names")
		}
		if operation.Kind != "query" {
			problems = append(problems, prefix+"only a query declares pagination")
		}
	}
	branches := make(map[string]OperationBranch, len(operation.Branches))
	reservedGoNames := map[string]bool{"Retry": true, "Default": true}
	for _, branch := range operation.Branches {
		branches[branch.ID] = branch
		reservedGoNames[branch.GoName] = true
	}
	seenNames := map[string]bool{}
	defaults := 0
	for _, mock := range operation.Mocks {
		mockPrefix := prefix + "mock " + mock.Name + ": "
		if !operationPattern.MatchString(mock.Name) || seenNames[mock.Name] {
			problems = append(problems, prefix+"mock names must be unique lower camel case values")
			continue
		}
		seenNames[mock.Name] = true
		if reservedGoNames[mock.GoName()] {
			problems = append(problems, mockPrefix+"name must not repeat a branch goName, Retry, or Default")
		}
		if strings.TrimSpace(mock.Description) == "" {
			problems = append(problems, mockPrefix+"description is required")
		}
		if mock.Default {
			defaults++
		}
		switch {
		case mock.Retry != nil:
			if mock.Branch != "" || mock.Output != nil || mock.Pages != nil || mock.Failure != nil || mock.Uncertain != nil || mock.Default {
				problems = append(problems, mockPrefix+"a retry mock declares only retry")
			}
			problems = append(problems, validateMockFailure(mockPrefix+"retry ", mock.Retry.Failure)...)
			if mock.Retry.After != "" {
				if after, err := time.ParseDuration(mock.Retry.After); err != nil || after < 0 {
					problems = append(problems, mockPrefix+"retry after must be a non-negative duration")
				}
			}
		case mock.Uncertain != nil:
			if operation.Kind != "mutation" || branches["uncertain"].ID == "" {
				problems = append(problems, mockPrefix+"an uncertain mock requires a mutation that declares uncertain")
			}
			if (mock.Branch != "" && mock.Branch != "uncertain") || mock.Output != nil || mock.Pages != nil || mock.Failure != nil || mock.Default {
				problems = append(problems, mockPrefix+"an uncertain mock declares only uncertain")
			}
			problems = append(problems, validateMockFailure(mockPrefix+"uncertain ", mock.Uncertain.Failure)...)
		default:
			problems = append(problems, validateBranchMock(operation, branches, mock, mockPrefix)...)
		}
	}
	if defaults > 1 {
		problems = append(problems, prefix+"at most one mock is the default")
	}
	return problems
}

func validateBranchMock(operation Operation, branches map[string]OperationBranch, mock OperationMock, prefix string) []string {
	var problems []string
	if _, declared := branches[mock.Branch]; !declared {
		problems = append(problems, prefix+"branch must name a declared branch")
	}
	if mock.Branch == "uncertain" {
		problems = append(problems, prefix+"the uncertain branch uses an uncertain mock")
	}
	if mock.Output == nil && mock.Pages == nil && mock.Failure == nil {
		problems = append(problems, prefix+"declares output, pages, or failure")
	}
	if mock.Output != nil && mock.Pages != nil {
		problems = append(problems, prefix+"output and pages are exclusive")
	}
	if mock.Failure != nil {
		problems = append(problems, validateMockFailure(prefix, *mock.Failure)...)
	}
	if mock.Default && (mock.Failure != nil || mock.Branch == "defect") {
		problems = append(problems, prefix+"a default mock answers without a failure on a branch other than defect")
	}
	if mock.Pages != nil {
		if operation.Pagination == nil {
			problems = append(problems, prefix+"pages require operation pagination")
		} else {
			problems = append(problems, validateMockPageChain(prefix, operation.Pagination.NextField, mock.Pages)...)
		}
	}
	return problems
}

// validateMockPageChain requires every page but the last to name a distinct next cursor and the last to end the
// listing, so a generated mock serves the pages in order.
func validateMockPageChain(prefix, nextField string, pages []MockValue) []string {
	if len(pages) == 0 {
		return []string{prefix + "pages must not be empty"}
	}
	seenCursors := map[string]bool{}
	for index, page := range pages {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(page.encoded, &fields); err != nil {
			return []string{fmt.Sprintf("%spage %d must be an object", prefix, index+1)}
		}
		cursor := compactJSON(fields[nextField])
		isLast := index == len(pages)-1
		switch {
		case isLast && !isEmptyCursor(cursor):
			return []string{fmt.Sprintf("%sthe last page must end the listing with an empty %s", prefix, nextField)}
		case !isLast && isEmptyCursor(cursor):
			return []string{fmt.Sprintf("%spage %d must name the next page in %s", prefix, index+1, nextField)}
		case !isLast && seenCursors[cursor]:
			return []string{fmt.Sprintf("%spage %d repeats a %s cursor", prefix, index+1, nextField)}
		}
		seenCursors[cursor] = true
	}
	return nil
}

func validateMockFailure(prefix string, failure MockFailure) []string {
	var problems []string
	isKnownKind := false
	for _, kind := range FailureKinds {
		isKnownKind = isKnownKind || kind == failure.Kind
	}
	if !isKnownKind {
		problems = append(problems, prefix+"failure kind must be a Connector SDK failure kind")
	}
	if strings.TrimSpace(failure.Message) == "" {
		problems = append(problems, prefix+"failure message is required")
	}
	return problems
}

func compactJSON(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return ""
	}
	return compact.String()
}

func isEmptyCursor(cursor string) bool {
	switch cursor {
	case "", "null", "0", `""`:
		return true
	default:
		return false
	}
}

func writeYAMLNodeAsJSON(output *bytes.Buffer, node *yaml.Node) error {
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) != 1 {
			return fmt.Errorf("mock value must contain one YAML value")
		}
		return writeYAMLNodeAsJSON(output, node.Content[0])
	case yaml.AliasNode:
		return fmt.Errorf("line %d: mock values cannot use YAML aliases", node.Line)
	case yaml.MappingNode:
		output.WriteByte('{')
		for index := 0; index < len(node.Content); index += 2 {
			key := node.Content[index]
			if key.Kind != yaml.ScalarNode || key.Tag == "!!merge" {
				return fmt.Errorf("line %d: mock object keys must be strings", key.Line)
			}
			if index > 0 {
				output.WriteByte(',')
			}
			output.WriteString(strconv.Quote(key.Value))
			output.WriteByte(':')
			if err := writeYAMLNodeAsJSON(output, node.Content[index+1]); err != nil {
				return err
			}
		}
		output.WriteByte('}')
		return nil
	case yaml.SequenceNode:
		output.WriteByte('[')
		for index, item := range node.Content {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := writeYAMLNodeAsJSON(output, item); err != nil {
				return err
			}
		}
		output.WriteByte(']')
		return nil
	case yaml.ScalarNode:
		return writeYAMLScalarAsJSON(output, node)
	default:
		return fmt.Errorf("line %d: unsupported mock value", node.Line)
	}
}

func writeYAMLScalarAsJSON(output *bytes.Buffer, node *yaml.Node) error {
	switch node.ShortTag() {
	case "!!str", "!!timestamp", "!!binary":
		encoded, err := json.Marshal(node.Value)
		if err != nil {
			return err
		}
		output.Write(encoded)
		return nil
	case "!!null":
		output.WriteString("null")
		return nil
	}
	var decoded any
	if err := node.Decode(&decoded); err != nil {
		return fmt.Errorf("line %d: %w", node.Line, err)
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return fmt.Errorf("line %d: mock scalar is not JSON: %w", node.Line, err)
	}
	output.Write(encoded)
	return nil
}
