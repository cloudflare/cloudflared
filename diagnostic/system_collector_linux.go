//go:build linux

package diagnostic

import (
	"context"
	"fmt"
	"os/exec"
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

func (collector *SystemCollectorImpl) Collect(ctx context.Context) (*SystemInformation, string, error) {
	memoryInfo, memoryInfoRaw, memoryInfoErr := collectMemoryInformation(ctx)
	fdInfo, fdInfoRaw, fdInfoErr := collectFileDescriptorInformation(ctx)
	disks, disksRaw, diskErr := collectDiskVolumeInformationUnix(ctx)
	osInfo, osInfoRaw, osInfoErr := collectOSInformationUnix(ctx)

	if memoryInfoErr != nil {
		raw := RawSystemInformation(osInfoRaw, memoryInfoRaw, fdInfoRaw, disksRaw)
		return nil, raw, memoryInfoErr
	}

	if fdInfoErr != nil {
		raw := RawSystemInformation(osInfoRaw, memoryInfoRaw, fdInfoRaw, disksRaw)
		return nil, raw, fdInfoErr
	}

	if diskErr != nil {
		raw := RawSystemInformation(osInfoRaw, memoryInfoRaw, fdInfoRaw, disksRaw)
		return nil, raw, diskErr
	}

	if osInfoErr != nil {
		raw := RawSystemInformation(osInfoRaw, memoryInfoRaw, fdInfoRaw, disksRaw)
		return nil, raw, osInfoErr
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
