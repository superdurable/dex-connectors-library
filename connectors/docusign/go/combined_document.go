// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	downloadCombinedDocumentOperationID = "downloadCombinedDocument"
	jsonMediaType                       = "application/json"
	pdfMediaType                        = "application/pdf"
	// documentRequestTimeout leaves time within the 120-second Execute timeout to finish the Result.
	documentRequestTimeout = 110 * time.Second
	// documentHeartbeatInterval keeps a long download alive within the 30-second heartbeat timeout.
	documentHeartbeatInterval = 5 * time.Second
	// maximumStoredLocationLength bounds the location a CombinedDocumentStore returns.
	maximumStoredLocationLength = 1024
)

// pdfSignature starts every PDF file.
var pdfSignature = []byte("%PDF-")

var (
	errCombinedDocumentTooLarge   = errors.New("the combined PDF exceeds the configured maxDocumentBytes")
	errCombinedDocumentNotPDF     = errors.New("DocuSign returned something other than a PDF for the combined document")
	errCombinedDocumentUnread     = errors.New("the combined PDF download was interrupted")
	errCombinedDocumentStore      = errors.New("the CombinedDocumentStore did not store the combined PDF")
	errCombinedDocumentStoreShort = errors.New("the CombinedDocumentStore returned before reading the whole combined PDF")
)

// CombinedDocumentStore keeps the combined PDFs that downloadCombinedDocument downloads, outside every
// Dex payload. Applications pass one to WithCombinedDocumentStore. The connector may call it
// concurrently for different envelopes and computes the Result's digest from the same bytes.
type CombinedDocumentStore interface {
	// StoreCombinedDocument reads content to its end and stores it as document. It returns a location
	// that is safe to persist in a Dex Result, such as an object key or a file name, never a credential
	// or a presigned URL, at most 1024 bytes.
	//
	// It must be idempotent and atomic: Dex can repeat a download, also concurrently after a slow local
	// attempt, so storing the same document again must replace or keep the earlier copy, and a Read
	// error from content, such as an interrupted or oversized download, must leave no partial copy. Any
	// error selects invalidResponse, and Dex does not retry it.
	StoreCombinedDocument(ctx context.Context, document CombinedDocumentReference, content io.Reader) (string, error)
}

// CombinedDocumentReference identifies the combined PDF a CombinedDocumentStore receives.
type CombinedDocumentReference struct {
	// AccountID is the DocuSign API account that holds the envelope.
	AccountID string
	// EnvelopeID is the envelope's GUID.
	EnvelopeID string
	// IncludesCertificate is true when the PDF ends with the certificate of completion.
	IncludesCertificate bool
}

// DownloadCombinedDocumentInput names the envelope whose documents to download.
type DownloadCombinedDocumentInput struct {
	// EnvelopeID is the envelope's GUID, normally of a completed envelope.
	EnvelopeID string `json:"envelopeId"`
	// IncludeCertificate appends DocuSign's certificate of completion to the combined PDF.
	IncludeCertificate bool `json:"includeCertificate,omitempty"`
}

// CombinedDocument describes one downloaded combined PDF; it never holds the PDF's bytes.
type CombinedDocument struct {
	// EnvelopeID is the envelope's GUID.
	EnvelopeID string `json:"envelopeId"`
	// ContentType is application/pdf.
	ContentType string `json:"contentType"`
	// ByteCount is the PDF's size in bytes.
	ByteCount int64 `json:"byteCount"`
	// SHA256 is the lowercase hexadecimal SHA-256 digest of the PDF's bytes.
	SHA256 string `json:"sha256"`
	// IncludesCertificate is true when the PDF ends with the certificate of completion.
	IncludesCertificate bool `json:"includesCertificate,omitempty"`
	// StoredLocation is where the CombinedDocumentStore put the PDF; blank without a store.
	StoredLocation string `json:"storedLocation,omitempty"`
}

// DownloadCombinedDocumentOperation implements the downloadCombinedDocument Query.
type DownloadCombinedDocumentOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (DownloadCombinedDocumentOperation) Definition() sdkgo.QueryDefinition {
	return DownloadCombinedDocumentDefinition
}

// Invoke streams the combined PDF through its digest and into the client's CombinedDocumentStore. A
// 429, 5xx, or interrupted download returns Retry; nothing is kept from an interrupted attempt.
func (operation DownloadCombinedDocumentOperation) Invoke(
	call sdkgo.Call, input DownloadCombinedDocumentInput,
) sdkgo.QueryAttempt[CombinedDocument] {
	client := operation.client
	if err := validateEnvelopeID(input.EnvelopeID); err != nil {
		return sdkgo.NewQueryBranch(DownloadCombinedDocumentBranchDefect, CombinedDocument{}, docusignFailurePointer(downloadCombinedDocumentOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	envelopeID := lowercaseGUID(input.EnvelopeID)
	session, sessionFailure := client.openSession(call, downloadCombinedDocumentOperationID)
	if sessionFailure != nil {
		return querySessionFailure[CombinedDocument](sessionFailure, DownloadCombinedDocumentBranchProviderRejected, DownloadCombinedDocumentBranchInvalidResponse, DownloadCombinedDocumentBranchDefect)
	}
	heartbeat := startDocumentHeartbeat(call)
	defer heartbeat.stop()
	requestContext, cancel := context.WithTimeout(call.Context, documentRequestTimeout)
	defer cancel()
	response, rejected, err := client.openDocumentStream(call, requestContext, &session,
		"/envelopes/"+url.PathEscape(envelopeID)+"/documents/combined",
		url.Values{"certificate": {strconv.FormatBool(input.IncludeCertificate)}})
	receipt := client.receipt(envelopeID, "")
	if err != nil {
		return sdkgo.NewQueryRetry[CombinedDocument](docusignFailure(downloadCombinedDocumentOperationID, sdkgo.FailureTransport, "DocuSign could not be reached to download the combined PDF"), 0)
	}
	if response == nil {
		outcome := client.classifyDocuSignFailure(downloadCombinedDocumentOperationID, rejected)
		receipt = client.receipt(envelopeID, outcome.errorCode)
		switch {
		case outcome.isRetry:
			return sdkgo.NewQueryRetry[CombinedDocument](outcome.failure, outcome.retryAfter)
		case outcome.isNotFound:
			return sdkgo.NewQueryBranch(DownloadCombinedDocumentBranchNotFound, CombinedDocument{}, &outcome.failure, receipt)
		default:
			return sdkgo.NewQueryBranch(DownloadCombinedDocumentBranchProviderRejected, CombinedDocument{}, &outcome.failure, receipt)
		}
	}
	defer response.Body.Close()
	reference := CombinedDocumentReference{AccountID: session.account.accountID, EnvelopeID: envelopeID, IncludesCertificate: input.IncludeCertificate}
	document, err := client.digestCombinedDocument(requestContext, response, reference)
	switch {
	case errors.Is(err, errCombinedDocumentTooLarge):
		return sdkgo.NewQueryBranch(DownloadCombinedDocumentBranchTooLarge, CombinedDocument{}, docusignFailurePointer(downloadCombinedDocumentOperationID, sdkgo.FailureResponseTooLarge, err.Error()), receipt)
	case errors.Is(err, errCombinedDocumentUnread):
		return sdkgo.NewQueryRetry[CombinedDocument](docusignFailure(downloadCombinedDocumentOperationID, sdkgo.FailureTransport, err.Error()), 0)
	case errors.Is(err, errCombinedDocumentNotPDF):
		return sdkgo.NewQueryBranch(DownloadCombinedDocumentBranchInvalidResponse, CombinedDocument{}, docusignFailurePointer(downloadCombinedDocumentOperationID, sdkgo.FailureProtocol, err.Error()), receipt)
	case err != nil:
		return sdkgo.NewQueryBranch(DownloadCombinedDocumentBranchInvalidResponse, CombinedDocument{}, docusignFailurePointer(downloadCombinedDocumentOperationID, sdkgo.FailureLocalDefect, err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(DownloadCombinedDocumentBranchDownloaded, document, nil, receipt)
}

// digestCombinedDocument checks the media type and PDF signature, then streams the body within
// maxDocumentBytes through SHA-256 and the store.
func (client *Client) digestCombinedDocument(
	requestContext context.Context, response *http.Response, reference CombinedDocumentReference,
) (CombinedDocument, error) {
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != pdfMediaType {
		return CombinedDocument{}, errCombinedDocumentNotPDF
	}
	if response.ContentLength > client.maxDocumentBytes {
		return CombinedDocument{}, errCombinedDocumentTooLarge
	}
	body := bufio.NewReader(response.Body)
	signature, err := body.Peek(len(pdfSignature))
	if err != nil && !errors.Is(err, io.EOF) {
		return CombinedDocument{}, errCombinedDocumentUnread
	}
	if !bytes.Equal(signature, pdfSignature) {
		return CombinedDocument{}, errCombinedDocumentNotPDF
	}
	content := &boundedDigestReader{source: body, remaining: client.maxDocumentBytes, digest: sha256.New()}
	location := ""
	if client.documentStore != nil {
		location, err = client.documentStore.StoreCombinedDocument(requestContext, reference, content)
		if content.err != nil {
			return CombinedDocument{}, content.err
		}
		if err != nil || !isBoundedText(location, 0, maximumStoredLocationLength) {
			return CombinedDocument{}, errCombinedDocumentStore
		}
		if !content.isComplete && !content.isAtEnd() {
			return CombinedDocument{}, errCombinedDocumentStoreShort
		}
	} else if _, err := io.Copy(io.Discard, content); err != nil {
		return CombinedDocument{}, content.err
	}
	return CombinedDocument{
		EnvelopeID: reference.EnvelopeID, ContentType: pdfMediaType, ByteCount: content.byteCount,
		SHA256: hex.EncodeToString(content.digest.Sum(nil)), IncludesCertificate: reference.IncludesCertificate,
		StoredLocation: location,
	}, nil
}

// boundedDigestReader hashes what it passes on and fails once more than remaining bytes arrive.
type boundedDigestReader struct {
	source     io.Reader
	remaining  int64
	digest     hash.Hash
	byteCount  int64
	isComplete bool
	err        error
}

// Read passes bytes on until EOF; an oversized or interrupted body returns a sticky error.
func (reader *boundedDigestReader) Read(buffer []byte) (int, error) {
	if reader.err != nil {
		return 0, reader.err
	}
	if reader.isComplete {
		return 0, io.EOF
	}
	count, err := reader.source.Read(buffer)
	if int64(count) > reader.remaining {
		reader.err = errCombinedDocumentTooLarge
		return 0, reader.err
	}
	reader.remaining -= int64(count)
	reader.byteCount += int64(count)
	_, _ = reader.digest.Write(buffer[:count]) // A hash never fails to write.
	switch {
	case errors.Is(err, io.EOF):
		reader.isComplete = true
	case err != nil:
		reader.err = errCombinedDocumentUnread
		return count, reader.err
	}
	return count, err
}

// isAtEnd reports whether a store that stopped without seeing EOF had read every byte.
func (reader *boundedDigestReader) isAtEnd() bool {
	var probe [1]byte
	count, err := reader.Read(probe[:])
	return count == 0 && errors.Is(err, io.EOF)
}

// documentHeartbeat records a nil Dex heartbeat every five seconds while a download is in flight.
type documentHeartbeat struct {
	stopSignal chan struct{}
	stopped    chan struct{}
}

func startDocumentHeartbeat(call sdkgo.Call) *documentHeartbeat {
	heartbeat := &documentHeartbeat{stopSignal: make(chan struct{}), stopped: make(chan struct{})}
	go heartbeat.recordUntilStopped(call)
	return heartbeat
}

func (heartbeat *documentHeartbeat) recordUntilStopped(call sdkgo.Call) {
	defer close(heartbeat.stopped)
	ticker := time.NewTicker(documentHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-heartbeat.stopSignal:
			return
		case <-ticker.C:
			_ = call.Context.RecordHeartbeat(nil) // A missed liveness heartbeat is repeated five seconds later.
		}
	}
}

// stop returns after the last heartbeat, because Dex rejects heartbeats after Execute returns.
func (heartbeat *documentHeartbeat) stop() {
	close(heartbeat.stopSignal)
	<-heartbeat.stopped
}
