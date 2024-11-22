//go:build darwin

package diagnostic

import (
	"context"
	"fmt"
	"os/exec"
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

func (collector *SystemCollectorImpl) Collect(ctx context.Context) (*SystemInformation, string, error) {
	memoryInfo, memoryInfoRaw, memoryInfoErr := collectMemoryInformation(ctx)
	fdInfo, fdInfoRaw, fdInfoErr := collectFileDescriptorInformation(ctx)
	disks, disksRaw, diskErr := collectDiskVolumeInformationUnix(ctx)
	osInfo, osInfoRaw, osInfoErr := collectOSInformationUnix(ctx)

	if memoryInfoErr != nil {
		return nil, RawSystemInformation(osInfoRaw, memoryInfoRaw, fdInfoRaw, disksRaw), memoryInfoErr
	}

	if fdInfoErr != nil {
		return nil, RawSystemInformation(osInfoRaw, memoryInfoRaw, fdInfoRaw, disksRaw), fdInfoErr
	}

	if diskErr != nil {
		return nil, RawSystemInformation(osInfoRaw, memoryInfoRaw, fdInfoRaw, disksRaw), diskErr
	}

	if osInfoErr != nil {
		return nil, RawSystemInformation(osInfoRaw, memoryInfoRaw, fdInfoRaw, disksRaw), osInfoErr
	}

	return NewSystemInformation(
		memoryInfo.MemoryMaximum,
		memoryInfo.MemoryCurrent,
		fdInfo.FileDescriptorMaximum,
		fdInfo.FileDescriptorCurrent,
		osInfo.OsSystem,
		osInfo.Name,
		osInfo.OsVersion,
		osInfo.OsRelease,
		osInfo.Architecture,
		collector.version,
		disks,
	), "", nil
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
