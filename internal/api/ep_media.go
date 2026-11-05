package api

import (
	"context"
	"io"
)

// Upload: POST /v1/uploads?purpose=
func (c *Client) Upload(ctx context.Context, purpose, contentType string, body io.Reader, size int64) (*Upload, error) {
	// TODO(core): Task 11
	return nil, nil
}

func (c *Client) DeleteUpload(ctx context.Context, uploadID string) error {
	// TODO(core): Task 11
	return nil
}

// FetchMedia: GET /v1/media/{id}; 304 => NotModified
func (c *Client) FetchMedia(ctx context.Context, mediaURL, ifNoneMatch string) (*MediaBlob, error) {
	// TODO(core): Task 11
	return nil, nil
}
