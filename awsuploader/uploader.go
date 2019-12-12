package awsuploader

// Uploader the functions required to upload to a bucket
type Uploader interface {
	//Upload a file to the bucket
	Upload(string) error
}
