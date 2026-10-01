// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package reportarchive demonstrates every Amazon S3 operation in one Flow started from Dex Web Start Flow:
// archive a generated report once under a deterministic key, read its metadata and text back, and list the
// archive folder.
package reportarchive

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "AmazonS3ReportArchive"
	// ConnectionName is the static Dex Web connection for Amazon S3.
	ConnectionName = "amazon-s3-reports"
	// ReportPrefix is the archive folder every report key starts with.
	ReportPrefix = "reports/"
	// ReportContentType is the content type of every archived report.
	ReportContentType = "text/markdown; charset=utf-8"
	// GeneratorMetadataValue is the generator metadata the example writes on every report.
	GeneratorMetadataValue = "dex-s3-report-archive"

	recordRequestStepType   = "RecordReportArchiveRequest"
	archiveReportStepType   = "ArchiveReport"
	recordArchiveStepType   = "RecordArchiveOutcome"
	readMetadataStepType    = "ReadBackReportMetadata"
	recordMetadataStepType  = "RecordReportMetadata"
	readTextStepType        = "ReadBackReportText"
	verifyTextStepType      = "VerifyReportText"
	listArchiveStepType     = "ListArchivedReports"
	completeArchiveStepType = "CompleteReportArchive"
	archiveListingPageSize  = 10
	maxTitleRunes           = 200
	maxHighlights           = 20
	maxHighlightRunes       = 500
)

var (
	requestAttribute = dex.DefineAttribute[ArchiveRequest]("amazon-s3-report-archive-request")
	outcomeAttribute = dex.DefineAttribute[Outcome]("amazon-s3-report-archive-outcome")
	reportIDPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
)

// Input is the typed request entered in Dex Web Start Flow.
type Input struct {
	// ReportID names the report and its key, as 1 to 63 lowercase letters, digits, and hyphens, such as
	// 2026-09-ops-weekly; the report is archived at reports/<reportId>.md.
	ReportID string `json:"reportId"`
	// Title is the report heading.
	Title string `json:"title"`
	// Highlights are the report's bullet points.
	Highlights []string `json:"highlights,omitempty"`
	// Bucket is the archive bucket; blank uses the connection's defaultBucket.
	Bucket string `json:"bucket,omitempty"`
	// ShouldReplaceExisting replaces a report already archived under the key; false archives it only once.
	ShouldReplaceExisting bool `json:"shouldReplaceExisting,omitempty"`
}

// ArchiveRequest is the validated request and the generated report the Flow archives.
type ArchiveRequest struct {
	// ReportID is the validated report ID.
	ReportID string `json:"reportId"`
	// Bucket is the requested bucket; blank uses the connection's defaultBucket.
	Bucket string `json:"bucket,omitempty"`
	// Key is the deterministic report key, reports/<reportId>.md.
	Key string `json:"key"`
	// Text is the generated Markdown report.
	Text string `json:"text"`
	// IsCreateOnly archives the report only when no object exists at the key.
	IsCreateOnly bool `json:"isCreateOnly"`
}

// ObjectLocation is the application input of the read-back Steps.
type ObjectLocation struct {
	// Bucket is the object's bucket.
	Bucket string `json:"bucket"`
	// Key is the object key.
	Key string `json:"key"`
}

// Status is the business outcome of one archive request.
type Status string

const (
	// StatusArchived means this Flow stored the report.
	StatusArchived Status = "archived"
	// StatusAlreadyArchived means a report was already archived under the key, so nothing was written.
	StatusAlreadyArchived Status = "alreadyArchived"
)

// Outcome is the durable result of the Flow and its completion output.
type Outcome struct {
	// Status is the business outcome.
	Status Status `json:"status"`
	// Stored is the object at the key after the write; for alreadyArchived it is the existing report.
	Stored *s3.StoredObject `json:"stored,omitempty"`
	// IsFromEarlierAttempt reports that a repeated attempt found the report its earlier attempt stored.
	IsFromEarlierAttempt bool `json:"isFromEarlierAttempt,omitempty"`
	// Report is the report's metadata, read back with headObject.
	Report *s3.ObjectMetadata `json:"report,omitempty"`
	// IsTextVerified reports that the archived text read back equals the report this Flow generated.
	IsTextVerified bool `json:"isTextVerified"`
	// ArchivedReportKeys lists the first page of keys in the archive folder.
	ArchivedReportKeys []string `json:"archivedReportKeys,omitempty"`
	// HasMoreArchivedReports reports that the archive folder has more keys than the first page.
	HasMoreArchivedReports bool `json:"hasMoreArchivedReports,omitempty"`
}

// Flow archives one generated report in Amazon S3 and reads it back.
type Flow struct {
	dex.FlowDefaults
	connection s3.Connection
}

// NewFlow binds the Amazon S3 Connection.
func NewFlow(connection s3.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the request, Amazon S3, and outcome Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordArchiveRequest{}),
		dex.DefineStep(s3.NewPutObjectStep(s3.PutObjectStepConfig[ArchiveRequest]{
			StepType: archiveReportStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "amazon-s3", GroupLabel: "Amazon S3",
				Explanation: "Store the report under its deterministic key; a repeated attempt finds its own earlier write.",
			},
			Connection: flow.connection, MapToOperationInput: MapToPutObjectInput,
			Stored:        sdkgo.GoTo(sdkgo.StepRef[s3.PutObjectResult](recordArchiveStepType)),
			AlreadyExists: sdkgo.GoTo(sdkgo.StepRef[s3.PutObjectResult](recordArchiveStepType)),
		})),
		dex.DefineStep(recordArchiveOutcome{}),
		dex.DefineStep(s3.NewHeadObjectStep(s3.HeadObjectStepConfig[ObjectLocation]{
			StepType: readMetadataStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "amazon-s3", GroupLabel: "Amazon S3",
				Explanation: "Read the archived report's size, ETag, content type, and metadata back from S3.",
			},
			Connection: flow.connection, MapToOperationInput: MapToHeadObjectInput,
			Found: sdkgo.GoTo(recordReportMetadata{}),
		})),
		dex.DefineStep(recordReportMetadata{}),
		dex.DefineStep(s3.NewGetObjectTextStep(s3.GetObjectTextStepConfig[ObjectLocation]{
			StepType: readTextStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "amazon-s3", GroupLabel: "Amazon S3",
				Explanation: "Read the archived report back as bounded UTF-8 text.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetObjectTextInput,
			Read: sdkgo.GoTo(verifyReportText{}),
		})),
		dex.DefineStep(verifyReportText{}),
		dex.DefineStep(s3.NewListObjectsStep(s3.ListObjectsStepConfig[ObjectLocation]{
			StepType: listArchiveStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "amazon-s3", GroupLabel: "Amazon S3",
				Explanation: "List the first page of report keys in the archive folder.",
			},
			Connection: flow.connection, MapToOperationInput: MapToListObjectsInput,
			Found: sdkgo.GoTo(completeReportArchive{}),
		})),
		dex.DefineStep(completeReportArchive{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{requestAttribute, outcomeAttribute}}
}

// GetDexSummary returns the request and outcome.
//
// dex:field attribute-key:amazon-s3-report-archive-request value-type:json editable:false description:"Report ID, key, and write mode"
// dex:field attribute-key:amazon-s3-report-archive-outcome value-type:json editable:false description:"Archive outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := reportArchiveInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"amazon-s3-report-archive-request": request,
		"amazon-s3-report-archive-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the request and outcome.
//
// dex:field attribute-key:amazon-s3-report-archive-request value-type:json editable:false description:"Report ID, key, generated text, and write mode"
// dex:field attribute-key:amazon-s3-report-archive-outcome value-type:json editable:false description:"Stored object, read-back metadata, text check, and archive listing"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := reportArchiveInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"amazon-s3-report-archive-request": request,
		"amazon-s3-report-archive-outcome": outcome,
	}}, nil
}

// NewArchiveRequest validates input and generates the report and its deterministic key.
func NewArchiveRequest(input Input) (ArchiveRequest, error) {
	if !reportIDPattern.MatchString(input.ReportID) {
		return ArchiveRequest{}, errors.New("reportId must be 1 to 63 lowercase letters, digits, and hyphens, starting with a letter or digit")
	}
	title := strings.TrimSpace(input.Title)
	if err := validateReportLine(title, maxTitleRunes); err != nil || title == "" {
		return ArchiveRequest{}, errors.New("title must be 1 to 200 characters on one line")
	}
	if len(input.Highlights) > maxHighlights {
		return ArchiveRequest{}, errors.New("highlights can hold at most 20 lines")
	}
	var report strings.Builder
	report.WriteString("# " + title + "\n\nReport ID: " + input.ReportID + "\n")
	if len(input.Highlights) > 0 {
		report.WriteString("\n")
	}
	for _, highlight := range input.Highlights {
		highlight = strings.TrimSpace(highlight)
		if err := validateReportLine(highlight, maxHighlightRunes); err != nil || highlight == "" {
			return ArchiveRequest{}, errors.New("each highlight must be 1 to 500 characters on one line")
		}
		report.WriteString("- " + highlight + "\n")
	}
	return ArchiveRequest{
		ReportID: input.ReportID, Bucket: input.Bucket, Key: ReportPrefix + input.ReportID + ".md",
		Text: report.String(), IsCreateOnly: !input.ShouldReplaceExisting,
	}, nil
}

// MapToPutObjectInput stores the generated report with its ID and generator as metadata.
func MapToPutObjectInput(request ArchiveRequest) s3.PutObjectInput {
	return s3.PutObjectInput{
		Bucket: request.Bucket, Key: request.Key, ContentType: ReportContentType, TextContent: request.Text,
		Metadata:     map[string]string{"report-id": request.ReportID, "generator": GeneratorMetadataValue},
		IsCreateOnly: request.IsCreateOnly,
	}
}

// MapToHeadObjectInput reads back the archived report's metadata.
func MapToHeadObjectInput(location ObjectLocation) s3.HeadObjectInput {
	return s3.HeadObjectInput{Bucket: location.Bucket, Key: location.Key}
}

// MapToGetObjectTextInput reads back the archived report's text.
func MapToGetObjectTextInput(location ObjectLocation) s3.GetObjectTextInput {
	return s3.GetObjectTextInput{Bucket: location.Bucket, Key: location.Key}
}

// MapToListObjectsInput lists the first page of the archive folder in the report's bucket.
func MapToListObjectsInput(location ObjectLocation) s3.ListObjectsInput {
	return s3.ListObjectsInput{Bucket: location.Bucket, Prefix: ReportPrefix, Delimiter: "/", MaxKeys: archiveListingPageSize}
}

func validateReportLine(line string, maxRunes int) error {
	if !utf8.ValidString(line) || utf8.RuneCountInString(line) > maxRunes || strings.ContainsFunc(line, unicode.IsControl) {
		return errors.New("line is invalid")
	}
	return nil
}

func reportArchiveInspection(ctx dex.Context) (ArchiveRequest, Outcome, error) {
	request, err := optionalAttribute(ctx, requestAttribute)
	if err != nil {
		return ArchiveRequest{}, Outcome{}, err
	}
	outcome, err := optionalAttribute(ctx, outcomeAttribute)
	if err != nil {
		return ArchiveRequest{}, Outcome{}, err
	}
	return request, outcome, nil
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// dex:group group-id:report-archive group-label:"Report archive"
// dex:explanation text:"Validate the request, generate the report, and derive its deterministic key."
type recordArchiveRequest struct {
	dex.StepDefaults
}

func (recordArchiveRequest) GetStepType() string { return recordRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordArchiveRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordArchiveRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	request, err := NewArchiveRequest(input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := requestAttribute.Set(ctx, request); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ArchiveRequest](archiveReportStepType), request), nil
}

// dex:group group-id:report-archive group-label:"Report archive"
// dex:explanation text:"Record whether this Flow stored the report or found one already archived."
type recordArchiveOutcome struct {
	dex.StepDefaultsNoWaitFor[s3.PutObjectResult]
}

func (recordArchiveOutcome) GetStepType() string { return recordArchiveStepType }

func (recordArchiveOutcome) Execute(ctx dex.Context, result s3.PutObjectResult) (*dex.StepDecision, error) {
	stored := result.Value
	outcome := Outcome{Status: StatusArchived, Stored: &stored, IsFromEarlierAttempt: stored.IsFromEarlierAttempt}
	if result.Branch == s3.PutObjectBranchAlreadyExists {
		outcome.Status = StatusAlreadyArchived
	}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ObjectLocation](readMetadataStepType), ObjectLocation{Bucket: stored.Bucket, Key: stored.Key}), nil
}

// dex:group group-id:report-archive group-label:"Report archive"
// dex:explanation text:"Record the read-back metadata and continue to the text read."
type recordReportMetadata struct {
	dex.StepDefaultsNoWaitFor[s3.HeadObjectResult]
}

func (recordReportMetadata) GetStepType() string { return recordMetadataStepType }

func (recordReportMetadata) Execute(ctx dex.Context, result s3.HeadObjectResult) (*dex.StepDecision, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	metadata := result.Value
	outcome.Report = &metadata
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ObjectLocation](readTextStepType), ObjectLocation{Bucket: metadata.Bucket, Key: metadata.Key}), nil
}

// dex:group group-id:report-archive group-label:"Report archive"
// dex:explanation text:"Compare the archived text with the generated report and continue to the listing."
type verifyReportText struct {
	dex.StepDefaultsNoWaitFor[s3.GetObjectTextResult]
}

func (verifyReportText) GetStepType() string { return verifyTextStepType }

func (verifyReportText) Execute(ctx dex.Context, result s3.GetObjectTextResult) (*dex.StepDecision, error) {
	request, err := requestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.IsTextVerified = result.Value.Text == request.Text
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	location := ObjectLocation{Bucket: result.Value.Bucket, Key: result.Value.Key}
	return dex.GoTo(sdkgo.StepRef[ObjectLocation](listArchiveStepType), location), nil
}

// dex:group group-id:report-archive group-label:"Report archive"
// dex:explanation text:"Record the archive folder's first page of keys and complete the Flow."
type completeReportArchive struct {
	dex.StepDefaultsNoWaitFor[s3.ListObjectsResult]
}

func (completeReportArchive) GetStepType() string { return completeArchiveStepType }

func (completeReportArchive) Execute(ctx dex.Context, result s3.ListObjectsResult) (*dex.StepDecision, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.ArchivedReportKeys = nil
	for _, object := range result.Value.Objects {
		outcome.ArchivedReportKeys = append(outcome.ArchivedReportKeys, object.Key)
	}
	outcome.HasMoreArchivedReports = result.Value.IsTruncated
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
