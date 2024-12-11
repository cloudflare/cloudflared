//go:build linux

package diagnostic

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
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
	gerror := SystemInformationGeneralError{}

	if memoryInfoErr != nil {
		gerror.MemoryInformationError = SystemInformationError{
			Err:     memoryInfoErr,
			RawInfo: memoryInfoRaw,
		}
	} else {
		memoryMaximum = memoryInfo.MemoryMaximum
		memoryCurrent = memoryInfo.MemoryCurrent
	}

	if fdInfoErr != nil {
		gerror.FileDescriptorsInformationError = SystemInformationError{
			Err:     fdInfoErr,
			RawInfo: fdInfoRaw,
		}
	} else {
		fileDescriptorMaximum = fdInfo.FileDescriptorMaximum
		fileDescriptorCurrent = fdInfo.FileDescriptorCurrent
	}

	if diskErr != nil {
		gerror.DiskVolumeInformationError = SystemInformationError{
			Err:     diskErr,
			RawInfo: disksRaw,
		}
	}

	if osInfoErr != nil {
		gerror.OperatingSystemInformationError = SystemInformationError{
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

	return info, gerror
}

func collectMemoryInformation(ctx context.Context) (*MemoryInformation, string, error) {
	// This function relies on the output of `cat /proc/meminfo` to retrieve
	// memoryMax and  memoryCurrent.
	// The expected output is in the format of `KEY VALUE UNIT`.
	const (
		memTotalPrefix     = "MemTotal"
		memAvailablePrefix = "MemAvailable"
	)

	command := exec.CommandContext(ctx, "cat", "/proc/meminfo")

	stdout, err := command.Output()
	if err != nil {
		return nil, "", fmt.Errorf("error retrieving output from command '%s': %w", command.String(), err)
	}

	output := string(stdout)

	mapper := func(field string) (uint64, error) {
		field = strings.TrimRight(field, " kB")

		return strconv.ParseUint(field, 10, 64)
	}

	memoryInfo, err := ParseMemoryInformationFromKV(output, memTotalPrefix, memAvailablePrefix, mapper)
	if err != nil {
		return nil, output, err
	}

	// returning raw output in case other collected information
	// resulted in errors
	return memoryInfo, output, nil
}

func collectFileDescriptorInformation(ctx context.Context) (*FileDescriptorInformation, string, error) {
	// Command retrieved from https://docs.kernel.org/admin-guide/sysctl/fs.html#file-max-file-nr.
	// If the sysctl is not available the command with fail.
	command := exec.CommandContext(ctx, "sysctl", "-n", "fs.file-nr")

	stdout, err := command.Output()
	if err != nil {
		return nil, "", fmt.Errorf("error retrieving output from command '%s': %w", command.String(), err)
	}

	output := string(stdout)

	fileDescriptorInfo, err := ParseSysctlFileDescriptorInformation(output)
	if err != nil {
		return nil, output, err
	}

	// returning raw output in case other collected information
	// resulted in errors
	return fileDescriptorInfo, output, nil
}
