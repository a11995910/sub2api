package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

var (
	ErrInvalidImageTaskRequest         = errors.New("invalid image task request")
	ErrInvalidImageTaskClientRequestID = errors.New("client_request_id must match [A-Za-z0-9_-]{1,64}")
)

var imageTaskClientRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type ParsedImageTaskRequest struct {
	Async           bool
	ClientRequestID string
	UpstreamBody    []byte
	Fingerprint     string
}

func ParseImageTaskRequest(body []byte) (ParsedImageTaskRequest, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return ParsedImageTaskRequest{UpstreamBody: body}, nil
	}

	rawAsync, hasAsync := fields["async"]
	if !hasAsync {
		return ParsedImageTaskRequest{UpstreamBody: body}, nil
	}

	var async bool
	if err := json.Unmarshal(rawAsync, &async); err != nil {
		return ParsedImageTaskRequest{}, fmt.Errorf("%w: async must be a boolean", ErrInvalidImageTaskRequest)
	}

	clientRequestID := ""
	if rawID, ok := fields["client_request_id"]; ok {
		if err := json.Unmarshal(rawID, &clientRequestID); err != nil {
			return ParsedImageTaskRequest{}, ErrInvalidImageTaskClientRequestID
		}
	}
	delete(fields, "async")
	delete(fields, "client_request_id")

	if async && !imageTaskClientRequestIDPattern.MatchString(clientRequestID) {
		return ParsedImageTaskRequest{}, ErrInvalidImageTaskClientRequestID
	}
	if async {
		normalizeImageTaskSize(fields)
	}

	upstreamBody, err := json.Marshal(fields)
	if err != nil {
		return ParsedImageTaskRequest{}, fmt.Errorf("%w: %v", ErrInvalidImageTaskRequest, err)
	}
	parsed := ParsedImageTaskRequest{
		Async:           async,
		ClientRequestID: clientRequestID,
		UpstreamBody:    upstreamBody,
	}
	if async {
		sum := sha256.Sum256(upstreamBody)
		parsed.Fingerprint = hex.EncodeToString(sum[:])
	}
	return parsed, nil
}

func normalizeImageTaskSize(fields map[string]json.RawMessage) {
	rawSize, ok := fields["size"]
	if !ok {
		return
	}
	var size string
	if json.Unmarshal(rawSize, &size) == nil && size == "1:1" {
		fields["size"] = json.RawMessage(`"1024x1024"`)
	}
}
