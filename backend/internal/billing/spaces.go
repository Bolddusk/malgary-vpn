package billing

import (
	"bytes"
	"context"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Spaces stores payment proofs in a private DigitalOcean Spaces bucket (S3-compatible).
type Spaces struct {
	c      *minio.Client
	bucket string
}

// NewSpaces connects to endpoint (e.g. ams3.digitaloceanspaces.com).
func NewSpaces(endpoint, bucket, accessKey, secretKey string) (*Spaces, error) {
	c, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: true})
	if err != nil {
		return nil, err
	}
	return &Spaces{c: c, bucket: bucket}, nil
}

// Put uploads a private object.
func (s *Spaces) Put(ctx context.Context, key string, body []byte, contentType string) error {
	_, err := s.c.PutObject(ctx, s.bucket, key, bytes.NewReader(body), int64(len(body)),
		minio.PutObjectOptions{ContentType: contentType, ServerSideEncryption: nil})
	return err
}
