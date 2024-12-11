//go:build windows

package diagnostic

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
)

const kiloBytesScale = 1.0 / 1024

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
	disks, disksRaw, diskErr := collectDiskVolumeInformation(ctx)
	osInfo, osInfoRaw, osInfoErr := collectOSInformation(ctx)

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

func collectMemoryInformation(ctx context.Context) (*MemoryInformation, string, error) {
	const (
		memoryTotalPrefix     = "TotalVirtualMemorySize"
		memoryAvailablePrefix = "FreeVirtualMemory"
	)

	command := exec.CommandContext(
		ctx,
		"powershell",
		"-Command",
		"Get-CimInstance -Class Win32_OperatingSystem | Select-Object FreeVirtualMemory, TotalVirtualMemorySize | Format-List",
	)

	stdout, err := command.Output()
	if err != nil {
		return nil, "", fmt.Errorf("error retrieving output from command '%s': %w", command.String(), err)
	}

	output := string(stdout)

	// the result of the command above will return values in bytes hence
	// they need to be converted to kilobytes
	mapper := func(field string) (uint64, error) {
		value, err := strconv.ParseUint(field, 10, 64)
		return uint64(float64(value) * kiloBytesScale), err
	}

	memoryInfo, err := ParseMemoryInformationFromKV(output, memoryTotalPrefix, memoryAvailablePrefix, mapper)
	if err != nil {
		return nil, output, err
	}

	// returning raw output in case other collected information
	// resulted in errors
	return memoryInfo, output, nil
}

func collectDiskVolumeInformation(ctx context.Context) ([]*DiskVolumeInformation, string, error) {

	command := exec.CommandContext(
		ctx,
		"powershell", "-Command", "Get-CimInstance -Class Win32_LogicalDisk | Select-Object DeviceID, Size, FreeSpace")

	stdout, err := command.Output()
	if err != nil {
		return nil, "", fmt.Errorf("error retrieving output from command '%s': %w", command.String(), err)
	}

	output := string(stdout)

	disks, err := ParseDiskVolumeInformationOutput(output, 2, kiloBytesScale)
	if err != nil {
		return nil, output, err
	}

	// returning raw output in case other collected information
	// resulted in errors
	return disks, output, nil
}

func collectOSInformation(ctx context.Context) (*OsInfo, string, error) {
	const (
		architecturePrefix = "OSArchitecture"
		osSystemPrefix     = "Caption"
		osVersionPrefix    = "Version"
		osReleasePrefix    = "BuildNumber"
		namePrefix         = "CSName"
	)

	command := exec.CommandContext(
		ctx,
		"powershell",
		"-Command",
		"Get-CimInstance -Class Win32_OperatingSystem | Select-Object OSArchitecture, Caption, Version, BuildNumber, CSName | Format-List",
	)

	stdout, err := command.Output()
	if err != nil {
		return nil, "", fmt.Errorf("error retrieving output from command '%s': %w", command.String(), err)
	}

	output := string(stdout)

	osInfo, err := ParseWinOperatingSystemInfo(output, architecturePrefix, osSystemPrefix, osVersionPrefix, osReleasePrefix, namePrefix)
	if err != nil {
		return nil, output, err
	}

	// returning raw output in case other collected information
	// resulted in errors
	return osInfo, output, nil
}
