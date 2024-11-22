package diagnostic

import "context"

type DiskVolumeInformation struct {
	Name        string `json:"name"`        // represents the filesystem in linux/macos or device name in windows
	SizeMaximum uint64 `json:"sizeMaximum"` // represents the maximum size of the disk in kilobytes
	SizeCurrent uint64 `json:"sizeCurrent"` // represents the current size of the disk in kilobytes
}

func NewDiskVolumeInformation(name string, maximum, current uint64) *DiskVolumeInformation {
	return &DiskVolumeInformation{
		name,
		maximum,
		current,
	}
}

type SystemInformation struct {
	MemoryMaximum         uint64                   `json:"memoryMaximum"`         // represents the maximum memory of the system in kilobytes
	MemoryCurrent         uint64                   `json:"memoryCurrent"`         // represents the system's memory in use in kilobytes
	FileDescriptorMaximum uint64                   `json:"fileDescriptorMaximum"` // represents the maximum number of file descriptors of the system
	FileDescriptorCurrent uint64                   `json:"fileDescriptorCurrent"` // represents the system's file descriptors in use
	OsSystem              string                   `json:"osSystem"`              // represents the operating system name i.e.: linux, windows, darwin
	HostName              string                   `json:"hostName"`              // represents the system host name
	OsVersion             string                   `json:"osVersion"`             // detailed information about the system's release version level
	OsRelease             string                   `json:"osRelease"`             // detailed information about the system's release
	Architecture          string                   `json:"architecture"`          // represents the system's hardware platform i.e: arm64/amd64
	CloudflaredVersion    string                   `json:"cloudflaredVersion"`    // the runtime version of cloudflared
	Disk                  []*DiskVolumeInformation `json:"disk"`
}

func NewSystemInformation(
	memoryMaximum,
	memoryCurrent,
	filesMaximum,
	filesCurrent uint64,
	osystem,
	name,
	osVersion,
	osRelease,
	architecture,
	cloudflaredVersion string,
	disk []*DiskVolumeInformation,
) *SystemInformation {
	return &SystemInformation{
		memoryMaximum,
		memoryCurrent,
		filesMaximum,
		filesCurrent,
		osystem,
		name,
		osVersion,
		osRelease,
		architecture,
		cloudflaredVersion,
		disk,
	}
}

type SystemCollector interface {
	// If the collection is successful it will return `SystemInformation` struct,
	// an empty string, and a nil error.
	// In case there is an error a string with the raw data will be returned
	// however the returned string not contain all the data points.
	//
	// This function expects that the caller sets the context timeout to prevent
	// long-lived collectors.
	Collect(ctx context.Context) (*SystemInformation, string, error)
}
