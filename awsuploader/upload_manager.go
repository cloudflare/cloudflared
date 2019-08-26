package awsuploader

// UploadManager is used to manage file uploads on an interval
type UploadManager interface {
	Upload(string) error
	Start()
}
