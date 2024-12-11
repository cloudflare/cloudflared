//go:build darwin

package diagnostic

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
)

type SystemCollectorImpl struct {
	version string
}

func NewSystemCollectorImpl(
	version string,
) *SystemCollectorImpl {
	return &SystemCollectorImpl{
		version,
	}
}

func (collector *SystemCollectorImpl) Collect(ctx context.Context) (*SystemInformation, error) {
	memoryInfo, memoryInfoRaw, memoryInfoErr := collectMemoryInformation(ctx)
	fdInfo, fdInfoRaw, fdInfoErr := collectFileDescriptorInformation(ctx)
	disks, disksRaw, diskErr := collectDiskVolumeInformationUnix(ctx)
	osInfo, osInfoRaw, osInfoErr := collectOSInformationUnix(ctx)

	var memoryMaximum, memoryCurrent, fileDescriptorMaximum, fileDescriptorCurrent uint64
	var osSystem, name, osVersion, osRelease, architecture string

	err := SystemInformationGeneralError{
		OperatingSystemInformationError: nil,
		MemoryInformationError:          nil,
		FileDescriptorsInformationError: nil,
		DiskVolumeInformationError:      nil,
	}

	if memoryInfoErr != nil {
		err.MemoryInformationError = SystemInformationError{
			Err:     memoryInfoErr,
			RawInfo: memoryInfoRaw,
		}
	} else {
		memoryMaximum = memoryInfo.MemoryMaximum
		memoryCurrent = memoryInfo.MemoryCurrent
	}

	if fdInfoErr != nil {
		err.FileDescriptorsInformationError = SystemInformationError{
			Err:     fdInfoErr,
			RawInfo: fdInfoRaw,
		}
	} else {
		fileDescriptorMaximum = fdInfo.FileDescriptorMaximum
		fileDescriptorCurrent = fdInfo.FileDescriptorCurrent
	}

	if diskErr != nil {
		err.DiskVolumeInformationError = SystemInformationError{
			Err:     diskErr,
			RawInfo: disksRaw,
		}
	}

	if osInfoErr != nil {
		err.OperatingSystemInformationError = SystemInformationError{
			Err:     osInfoErr,
			RawInfo: osInfoRaw,
		}
	} else {
		osSystem = osInfo.OsSystem
		name = osInfo.Name
		osVersion = osInfo.OsVersion
		osRelease = osInfo.OsRelease
		architecture = osInfo.Architecture
	}

	cloudflaredVersion := collector.version
	info := NewSystemInformation(
		memoryMaximum,
		memoryCurrent,
		fileDescriptorMaximum,
		fileDescriptorCurrent,
		osSystem,
		name,
		osVersion,
		osRelease,
		architecture,
		cloudflaredVersion,
		runtime.Version(),
		runtime.GOARCH,
		disks,
	)

	return info, err
}

func collectFileDescriptorInformation(ctx context.Context) (
	*FileDescriptorInformation,
	string,
	error,
) {
	const (
		fileDescriptorMaximumKey = "kern.maxfiles"
		fileDescriptorCurrentKey = "kern.num_files"
	)

	command := exec.CommandContext(ctx, "sysctl", fileDescriptorMaximumKey, fileDescriptorCurrentKey)

	stdout, err := command.Output()
	if err != nil {
		return nil, "", fmt.Errorf("error retrieving output from command '%s': %w", command.String(), err)
	}

	output := string(stdout)

	fileDescriptorInfo, err := ParseFileDescriptorInformationFromKV(
		output,
		fileDescriptorMaximumKey,
		fileDescriptorCurrentKey,
	)
	if err != nil {
		return nil, output, err
	}

	// returning raw output in case other collected information
	// resulted in errors
	return fileDescriptorInfo, output, nil
}

func collectMemoryInformation(ctx context.Context) (
	*MemoryInformation,
	string,
	error,
) {
	const (
		memoryMaximumKey   = "hw.memsize"
		memoryAvailableKey = "hw.memsize_usable"
	)

	command := exec.CommandContext(
		ctx,
		"sysctl",
		memoryMaximumKey,
		memoryAvailableKey,
	)

	stdout, err := command.Output()
	if err != nil {
		return nil, "", fmt.Errorf("error retrieving output from command '%s': %w", command.String(), err)
	}

	output := string(stdout)

	mapper := func(field string) (uint64, error) {
		const kiloBytes = 1024
		value, err := strconv.ParseUint(field, 10, 64)
		return value / kiloBytes, err
	}

	memoryInfo, err := ParseMemoryInformationFromKV(output, memoryMaximumKey, memoryAvailableKey, mapper)
	if err != nil {
		return nil, output, err
	}

	// returning raw output in case other collected information
	// resulted in errors
	return memoryInfo, output, nil
}
