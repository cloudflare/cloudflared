package diagnostic

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

type SystemInformationError struct {
	Err     error  `json:"error"`
	RawInfo string `json:"rawInfo"`
}

func (err SystemInformationError) Error() string {
	return err.Err.Error()
}

func (err SystemInformationError) MarshalJSON() ([]byte, error) {
	s := map[string]string{
		"error":   err.Err.Error(),
		"rawInfo": err.RawInfo,
	}

	return json.Marshal(s)
}

type SystemInformationGeneralError struct {
	OperatingSystemInformationError error
	MemoryInformationError          error
	FileDescriptorsInformationError error
	DiskVolumeInformationError      error
}

func (err SystemInformationGeneralError) Error() string {
	builder := &strings.Builder{}
	builder.WriteString("errors found:")

	if err.OperatingSystemInformationError != nil {
		builder.WriteString(err.OperatingSystemInformationError.Error() + ", ")
	}

	if err.MemoryInformationError != nil {
		builder.WriteString(err.MemoryInformationError.Error() + ", ")
	}

	if err.FileDescriptorsInformationError != nil {
		builder.WriteString(err.FileDescriptorsInformationError.Error() + ", ")
	}

	if err.DiskVolumeInformationError != nil {
		builder.WriteString(err.DiskVolumeInformationError.Error() + ", ")
	}

	return builder.String()
}

func (err SystemInformationGeneralError) MarshalJSON() ([]byte, error) {
	data := map[string]SystemInformationError{}

	var sysErr SystemInformationError
	if errors.As(err.OperatingSystemInformationError, &sysErr) {
		data["operatingSystemInformationError"] = sysErr
	}

	if errors.As(err.MemoryInformationError, &sysErr) {
		data["memoryInformationError"] = sysErr
	}

	if errors.As(err.FileDescriptorsInformationError, &sysErr) {
		data["fileDescriptorsInformationError"] = sysErr
	}

	if errors.As(err.DiskVolumeInformationError, &sysErr) {
		data["diskVolumeInformationError"] = sysErr
	}

	return json.Marshal(data)
}

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
	MemoryMaximum         uint64                   `json:"memoryMaximum,omitempty"`         // represents the maximum memory of the system in kilobytes
	MemoryCurrent         uint64                   `json:"memoryCurrent,omitempty"`         // represents the system's memory in use in kilobytes
	FileDescriptorMaximum uint64                   `json:"fileDescriptorMaximum,omitempty"` // represents the maximum number of file descriptors of the system
	FileDescriptorCurrent uint64                   `json:"fileDescriptorCurrent,omitempty"` // represents the system's file descriptors in use
	OsSystem              string                   `json:"osSystem,omitempty"`              // represents the operating system name i.e.: linux, windows, darwin
	HostName              string                   `json:"hostName,omitempty"`              // represents the system host name
	OsVersion             string                   `json:"osVersion,omitempty"`             // detailed information about the system's release version level
	OsRelease             string                   `json:"osRelease,omitempty"`             // detailed information about the system's release
	Architecture          string                   `json:"architecture,omitempty"`          // represents the system's hardware platform i.e: arm64/amd64
	CloudflaredVersion    string                   `json:"cloudflaredVersion,omitempty"`    // the runtime version of cloudflared
	GoVersion             string                   `json:"goVersion,omitempty"`
	GoArch                string                   `json:"goArch,omitempty"`
	Disk                  []*DiskVolumeInformation `json:"disk,omitempty"`
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
	cloudflaredVersion,
	goVersion,
	goArchitecture string,
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
		goVersion,
		goArchitecture,
		disk,
	}
}

type SystemCollector interface {
	// If the collection is successful it will return `SystemInformation` struct,
	// and a nil error.
	//
	// This function expects that the caller sets the context timeout to prevent
	// long-lived collectors.
	Collect(ctx context.Context) (*SystemInformation, error)
}
