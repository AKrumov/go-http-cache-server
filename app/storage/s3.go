package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// S3 implements Backend using Amazon S3 (or compatible services like MinIO).
type S3 struct {
	client *s3.Client
	bucket string
	prefix string
}

// S3Options holds configuration for the S3 backend.
type S3Options struct {
	Bucket      string
	Prefix      string
	Region      string
	Endpoint    string                  // optional, for MinIO and other S3-compatible services
	Credentials aws.CredentialsProvider // optional
}

// NewS3 creates a new S3 backend.
func NewS3(ctx context.Context, opts S3Options) (*S3, error) {
	if opts.Bucket == "" {
		return nil, fmt.Errorf("s3 bucket is required")
	}

	loadOpts := []func(*config.LoadOptions) error{
		config.WithRegion(opts.Region),
	}
	if opts.Credentials != nil {
		loadOpts = append(loadOpts, config.WithCredentialsProvider(opts.Credentials))
	}
	cfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	s3Opts := []func(*s3.Options){}
	if opts.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			// Use path-style URLs for MinIO compatibility.
			o.BaseEndpoint = aws.String(opts.Endpoint)
			o.UsePathStyle = true
		})
	}

	client := s3.NewFromConfig(cfg, s3Opts...)
	return &S3{
		client: client,
		bucket: opts.Bucket,
		prefix: opts.Prefix,
	}, nil
}

func (s *S3) key(key string) string {
	if s.prefix == "" {
		return key
	}
	return s.prefix + "/" + key
}

func isNotFound(err error) bool {
	var respErr *smithyhttp.ResponseError
	if errors.As(err, &respErr) {
		return respErr.HTTPStatusCode() == 404
	}
	return false
}

// Head implements Backend.
func (s *S3) Head(ctx context.Context, key string) (size int64, exists bool, err error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(key)),
	})
	if err != nil {
		if isNotFound(err) {
			return 0, false, nil
		}
		return 0, false, err
	}

	if out.ContentLength == nil {
		return 0, true, nil
	}
	return *out.ContentLength, true, nil
}

// Get implements Backend.
func (s *S3) Get(ctx context.Context, key string) (rc io.ReadCloser, size int64, modTime time.Time, exists bool, err error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(key)),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, 0, time.Time{}, false, nil
		}
		return nil, 0, time.Time{}, false, err
	}

	var sz int64
	if out.ContentLength != nil {
		sz = *out.ContentLength
	}
	var mt time.Time
	if out.LastModified != nil {
		mt = *out.LastModified
	}

	return out.Body, sz, mt, true, nil
}

// Put implements Backend.
func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(key)),
		Body:   r,
	}
	if size > 0 {
		input.ContentLength = aws.Int64(size)
	}

	_, err := s.client.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to upload object to S3: %w", err)
	}
	return nil
}
