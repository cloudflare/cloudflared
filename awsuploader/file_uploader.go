package awsuploader

import (
	"context"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

//FileUploader aws compliant bucket upload
type FileUploader struct {
	storage    *s3.S3
	bucketName string
	clientID   string
	secretID   string
}

// NewFileUploader creates a new S3 compliant bucket uploader
func NewFileUploader(bucketName, region, accessKeyID, secretID, token, s3Host string) (*FileUploader, error) {
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(region),
		Credentials: credentials.NewStaticCredentials(accessKeyID, secretID, token),
	})
	if err != nil {
		return nil, err
	}

	var storage *s3.S3
	if s3Host != "" {
		storage = s3.New(sess, &aws.Config{Endpoint: aws.String(s3Host)})
	} else {
		storage = s3.New(sess)
	}

	return &FileUploader{
		storage:    storage,
		bucketName: bucketName,
	}, nil
}

// Upload a file to the bucket
func (u *FileUploader) Upload(filepath string) error {
	info, err := os.Stat(filepath)
	if err != nil {
		return err
	}
	file, err := os.Open(filepath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, serr := u.storage.PutObjectWithContext(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(u.bucketName),
		Key:    aws.String(info.Name()),
		Body:   file,
	})
	return serr
}
