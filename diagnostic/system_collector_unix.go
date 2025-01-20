//go:build !darwin || !linux || freebsd || openbsd || netbsd 

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
	// FreeBSD uses `sysctl` to retrieve memory information.
	const (
		memTotalKey     = "hw.physmem"
		memAvailableKey = "vm.stats.vm.v_free_count"
	)

	command := exec.CommandContext(ctx, "sysctl", "-n", memTotalKey)
	totalOutput, err := command.Output()
	if err != nil {
		return nil, "", fmt.Errorf("error retrieving output from command '%s': %w", command.String(), err)
	}

	command = exec.CommandContext(ctx, "sysctl", "-n", memAvailableKey)
	availableOutput, err := command.Output()
	if err != nil {
		return nil, "", fmt.Errorf("error retrieving output from command '%s': %w", command.String(), err)
	}

	total, err := strconv.ParseUint(strings.TrimSpace(string(totalOutput)), 10, 64)
	if err != nil {
		return nil, string(totalOutput), fmt.Errorf("error parsing memory total: %w", err)
	}

	available, err := strconv.ParseUint(strings.TrimSpace(string(availableOutput)), 10, 64)
	if err != nil {
		return nil, string(availableOutput), fmt.Errorf("error parsing memory available: %w", err)
	}

	memoryInfo := &MemoryInformation{
		MemoryMaximum: total,
		MemoryCurrent: available * 4096, // FreeBSD reports pages; multiply by page size (4K).
	}

	return memoryInfo, fmt.Sprintf("Total: %s, Available: %s", totalOutput, availableOutput), nil
}

func collectFileDescriptorInformation(ctx context.Context) (*FileDescriptorInformation, string, error) {
	// FreeBSD uses `sysctl` for file descriptor limits.
	const (
		fdMaxKey = "kern.maxfiles"
		fdCurrentKey = "kern.openfiles"
	)

	command := exec.CommandContext(ctx, "sysctl", "-n", fdMaxKey)
	maxOutput, err := command.Output()
	if err != nil {
		return nil, "", fmt.Errorf("error retrieving output from command '%s': %w", command.String(), err)
	}

	command = exec.CommandContext(ctx, "sysctl", "-n", fdCurrentKey)
	currentOutput, err := command.Output()
	if err != nil {
		return nil, "", fmt.Errorf("error retrieving output from command '%s': %w", command.String(), err)
	}

	max, err := strconv.ParseUint(strings.TrimSpace(string(maxOutput)), 10, 64)
	if err != nil {
		return nil, string(maxOutput), fmt.Errorf("error parsing max file descriptors: %w", err)
	}

	current, err := strconv.ParseUint(strings.TrimSpace(string(currentOutput)), 10, 64)
	if err != nil {
		return nil, string(currentOutput), fmt.Errorf("error parsing current file descriptors: %w", err)
	}

	fdInfo := &FileDescriptorInformation{
		FileDescriptorMaximum: max,
		FileDescriptorCurrent: current,
	}

	return fdInfo, fmt.Sprintf("Max: %s, Current: %s", maxOutput, currentOutput), nil
}
