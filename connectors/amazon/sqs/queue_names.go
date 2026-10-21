// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqs

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

const (
	// fifoQueueSuffix ends the name of every FIFO queue.
	fifoQueueSuffix = ".fifo"
	// maxQueueURLBytes bounds a queue URL far above any URL SQS or a compatible server issues.
	maxQueueURLBytes = 2048
	// maxReceiptHandleBytes bounds the opaque handle SQS returns, which is a few hundred bytes in practice.
	maxReceiptHandleBytes = 4096
	// maxMessageIDBytes bounds the message ID that names an acknowledged message in its Result and receipt.
	maxMessageIDBytes = 128
)

var (
	// queueNamePattern is the SQS rule: up to 80 letters, digits, hyphens, and underscores, plus .fifo.
	queueNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)
	// queueOwnerPattern accepts an AWS account ID and the owner segment compatible servers use.
	queueOwnerPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	// amazonQueueHostPattern captures the Region of an Amazon SQS Regional queue URL.
	amazonQueueHostPattern = regexp.MustCompile(`^sqs\.([a-z0-9-]+)\.amazonaws\.(?:com|com\.cn|eu)$`)
)

// queueAddress is one validated queue URL.
type queueAddress struct {
	url    string
	name   string
	isFIFO bool
	// amazonRegion is the Region an Amazon SQS queue URL names; blank for a compatible server.
	amazonRegion string
}

// parseQueueURL accepts http(s)://host[:port]/<owner>/<queue name>, the form SQS and compatible servers issue.
func parseQueueURL(queueURL string) (queueAddress, error) {
	invalid := errors.New("queueUrl must be a queue URL such as https://sqs.eu-west-1.amazonaws.com/123456789012/orders")
	if len(queueURL) > maxQueueURLBytes {
		return queueAddress{}, invalid
	}
	parsed, err := url.Parse(queueURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return queueAddress{}, invalid
	}
	owner, name, hasName := strings.Cut(strings.TrimPrefix(parsed.Path, "/"), "/")
	if !hasName || !queueOwnerPattern.MatchString(owner) {
		return queueAddress{}, invalid
	}
	baseName, isFIFO := strings.CutSuffix(name, fifoQueueSuffix)
	if !queueNamePattern.MatchString(baseName) || len(name) > 80 {
		return queueAddress{}, errors.New("queueUrl must end with a queue name of 1 to 80 letters, digits, hyphens, and underscores, including any .fifo suffix")
	}
	address := queueAddress{url: queueURL, name: name, isFIFO: isFIFO}
	if match := amazonQueueHostPattern.FindStringSubmatch(parsed.Hostname()); match != nil {
		address.amazonRegion = match[1]
	}
	return address, nil
}

// validateReceiptHandle accepts the printable ASCII handle that receiveMessages returned.
func validateReceiptHandle(receiptHandle string) error {
	if receiptHandle == "" || len(receiptHandle) > maxReceiptHandleBytes || !isPrintableToken(receiptHandle) {
		return errors.New("receiptHandle must be the handle receiveMessages returned for the message")
	}
	return nil
}

// validateMessageID accepts a blank ID or the printable ASCII message ID that receiveMessages returned.
func validateMessageID(messageID string) error {
	if len(messageID) > maxMessageIDBytes || (messageID != "" && !isPrintableToken(messageID)) {
		return errors.New("messageId must be blank or the message ID receiveMessages returned")
	}
	return nil
}

// isPrintableToken accepts printable ASCII without spaces.
func isPrintableToken(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] < 0x21 || value[index] > 0x7E {
			return false
		}
	}
	return true
}
