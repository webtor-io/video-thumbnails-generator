package services

import (
	"bytes"
	"io"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
)

// S3Storage manipulates with previews
type S3Storage struct {
	bucket string
	cl     *cs.S3Client
	inited bool
}

const (
	awsBucket = "aws-bucket"
)

var (
	// ErrNoPreview raises when no previews found
	ErrNoPreview = errors.New("No preview")
)

// RegisterS3StorageFlags registers S3Storage flags
func RegisterS3StorageFlags(c *cli.App) {
	c.Flags = append(c.Flags, cli.StringFlag{
		Name:   awsBucket,
		Usage:  "AWS Bucket",
		Value:  "",
		EnvVar: "AWS_BUCKET",
	})
}

// NewS3Storage initializes S3Storage
func NewS3Storage(c *cli.Context, cl *cs.S3Client) *S3Storage {
	return &S3Storage{
		bucket: c.String(awsBucket),
		cl:     cl,
	}
}

func (s *S3Storage) initBucket() error {
	if s.inited {
		return nil
	}
	s.inited = true
	cl := s.cl.Get()
	_, err := cl.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(s.bucket),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeBucketAlreadyExists:
				log.WithError(err).Warnf("Preview bucket %v already exists", s.bucket)
			case s3.ErrCodeBucketAlreadyOwnedByYou:
				log.WithError(err).Warnf("Preview bucket %v already owned", s.bucket)
			default:
				return errors.Wrapf(err, "Failed to create preview bucket %v", s.bucket)
			}
		} else {
			return errors.Wrapf(err, "Failed to create preview bucket %v", s.bucket)
		}
	}
	return nil
}

// PutPreview puts preview in S3 storage
func (s *S3Storage) PutPreview(key string, data []byte) error {
	err := s.initBucket()
	if err != nil {
		return errors.Wrap(err, "Failed to init bucket")
	}
	log.Infof("Storing preview key=%v bucket=%v", key, s.bucket)
	_, err = s.cl.Get().PutObject(&s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return errors.Wrapf(err, "Failed to store preview key=%v bucket=%v", key, s.bucket)
	}
	return nil
}

// GetPreview gets preview from S3 storage
func (s *S3Storage) GetPreview(key string) (io.ReadCloser, error) {
	log.Infof("Fetching preview key=%v bucket=%v", key, s.bucket)
	r, err := s.cl.Get().GetObject(&s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok && (awsErr.Code() == s3.ErrCodeNoSuchKey || awsErr.Code() == s3.ErrCodeNoSuchBucket) {
			log.Infof("No preview key=%v bucket=%v", key, s.bucket)
			return nil, ErrNoPreview
		}
		return nil, errors.Wrapf(err, "Failed to fetch preview key=%v bucket=%v", key, s.bucket)
	}
	return r.Body, nil
}
