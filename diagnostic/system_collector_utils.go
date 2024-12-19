package diagnostic

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

func findColonSeparatedPairs[V any](output string, keys []string, mapper func(string) (V, error)) map[string]V {
	const (
		memoryField             = 1
		memoryInformationFields = 2
	)

	lines := strings.Split(output, "\n")
	pairs := make(map[string]V, 0)

	// sort keys and lines to allow incremental search
	sort.Strings(lines)
	sort.Strings(keys)

	// keeps track of the last key found
	lastIndex := 0

	for _, line := range lines {
		if lastIndex == len(keys) {
			// already found all keys no need to continue iterating
			// over the other values
			break
		}

		for index, key := range keys[lastIndex:] {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, key) {
				fields := strings.Split(line, ":")
				if len(fields) < memoryInformationFields {
					lastIndex = index + 1

					break
				}

				field, err := mapper(strings.TrimSpace(fields[memoryField]))
				if err != nil {
					lastIndex = lastIndex + index + 1

					break
				}

				pairs[key] = field
				lastIndex = lastIndex + index + 1

				break
			}
		}
	}

	return pairs
}

func ParseDiskVolumeInformationOutput(output string, skipLines int, scale float64) ([]*DiskVolumeInformation, error) {
	const (
		diskFieldsMinimum = 3
		nameField         = 0
		sizeMaximumField  = 1
		sizeCurrentField  = 2
	)

	disksRaw := strings.Split(output, "\n")
	disks := make([]*DiskVolumeInformation, 0)

	if skipLines > len(disksRaw) || skipLines < 0 {
		skipLines = 0
	}

	for _, disk := range disksRaw[skipLines:] {
		if disk == "" {
			// skip empty line
			continue
		}

		fields := strings.Fields(disk)
		if len(fields) < diskFieldsMinimum {
			return nil, fmt.Errorf("expected disk volume to have %d fields got %d: %w",
				diskFieldsMinimum, len(fields), ErrInsuficientFields,
			)
		}

		name := fields[nameField]

		sizeMaximum, err := strconv.ParseUint(fields[sizeMaximumField], 10, 64)
		if err != nil {
			continue
		}

		sizeCurrent, err := strconv.ParseUint(fields[sizeCurrentField], 10, 64)
		if err != nil {
			continue
		}

		diskInfo := NewDiskVolumeInformation(
			name, uint64(float64(sizeMaximum)*scale), uint64(float64(sizeCurrent)*scale),
		)
		disks = append(disks, diskInfo)
	}

	if len(disks) == 0 {
		return nil, ErrNoVolumeFound
	}

	return disks, nil
}

type OsInfo struct {
	OsSystem     string
	Name         string
	OsVersion    string
	OsRelease    string
	Architecture string
}

func ParseUnameOutput(output string, system string) (*OsInfo, error) {
	const (
		osystemField               = 0
		nameField                  = 1
		osVersionField             = 2
		osReleaseStartField        = 3
		osInformationFieldsMinimum = 6
		darwin                     = "darwin"
	)

	architectureOffset := 2
	if system == darwin {
		architectureOffset = 1
	}

	fields := strings.Fields(output)
	if len(fields) < osInformationFieldsMinimum {
		return nil, fmt.Errorf("expected system information to have %d fields got %d: %w",
			osInformationFieldsMinimum, len(fields), ErrInsuficientFields,
		)
	}

	architectureField := len(fields) - architectureOffset
	osystem := fields[osystemField]
	name := fields[nameField]
	osVersion := fields[osVersionField]
	osRelease := strings.Join(fields[osReleaseStartField:architectureField], " ")
	architecture := fields[architectureField]

	return &OsInfo{
		osystem,
		name,
		osVersion,
		osRelease,
		architecture,
	}, nil
}

func ParseWinOperatingSystemInfo(
	output string,
	architectureKey string,
	osSystemKey string,
	osVersionKey string,
	osReleaseKey string,
	nameKey string,
) (*OsInfo, error) {
	identity := func(s string) (string, error) { return s, nil }

	keys := []string{architectureKey, osSystemKey, osVersionKey, osReleaseKey, nameKey}
	pairs := findColonSeparatedPairs(
		output,
		keys,
		identity,
	)

	architecture, exists := pairs[architectureKey]
	if !exists {
		return nil, fmt.Errorf("parsing os information: %w, key=%s", ErrKeyNotFound, architectureKey)
	}

	osSystem, exists := pairs[osSystemKey]
	if !exists {
		return nil, fmt.Errorf("parsing os information: %w, key=%s", ErrKeyNotFound, osSystemKey)
	}

	osVersion, exists := pairs[osVersionKey]
	if !exists {
		return nil, fmt.Errorf("parsing os information: %w, key=%s", ErrKeyNotFound, osVersionKey)
	}

	osRelease, exists := pairs[osReleaseKey]
	if !exists {
		return nil, fmt.Errorf("parsing os information: %w, key=%s", ErrKeyNotFound, osReleaseKey)
	}

	name, exists := pairs[nameKey]
	if !exists {
		return nil, fmt.Errorf("parsing os information: %w, key=%s", ErrKeyNotFound, nameKey)
	}

	return &OsInfo{osSystem, name, osVersion, osRelease, architecture}, nil
}

type FileDescriptorInformation struct {
	FileDescriptorMaximum uint64
	FileDescriptorCurrent uint64
}

func ParseSysctlFileDescriptorInformation(output string) (*FileDescriptorInformation, error) {
	const (
		openFilesField             = 0
		maxFilesField              = 2
		fileDescriptorLimitsFields = 3
	)

	fields := strings.Fields(output)

	if len(fields) != fileDescriptorLimitsFields {
		return nil,
			fmt.Errorf(
				"expected file descriptor information to have %d fields got %d: %w",
				fileDescriptorLimitsFields,
				len(fields),
				ErrInsuficientFields,
			)
	}

	fileDescriptorCurrent, err := strconv.ParseUint(fields[openFilesField], 10, 64)
	if err != nil {
		return nil, fmt.Errorf(
			"error parsing files current field '%s': %w",
			fields[openFilesField],
			err,
		)
	}

	fileDescriptorMaximum, err := strconv.ParseUint(fields[maxFilesField], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("error parsing files max field '%s': %w", fields[maxFilesField], err)
	}

	return &FileDescriptorInformation{fileDescriptorMaximum, fileDescriptorCurrent}, nil
}

func ParseFileDescriptorInformationFromKV(
	output string,
	fileDescriptorMaximumKey string,
	fileDescriptorCurrentKey string,
) (*FileDescriptorInformation, error) {
	mapper := func(field string) (uint64, error) {
		return strconv.ParseUint(field, 10, 64)
	}

	pairs := findColonSeparatedPairs(output, []string{fileDescriptorMaximumKey, fileDescriptorCurrentKey}, mapper)

	fileDescriptorMaximum, exists := pairs[fileDescriptorMaximumKey]
	if !exists {
		return nil, fmt.Errorf(
			"parsing file descriptor information: %w, key=%s",
			ErrKeyNotFound,
			fileDescriptorMaximumKey,
		)
	}

	fileDescriptorCurrent, exists := pairs[fileDescriptorCurrentKey]
	if !exists {
		return nil, fmt.Errorf(
			"parsing file descriptor information: %w, key=%s",
			ErrKeyNotFound,
			fileDescriptorCurrentKey,
		)
	}

	return &FileDescriptorInformation{fileDescriptorMaximum, fileDescriptorCurrent}, nil
}

type MemoryInformation struct {
	MemoryMaximum uint64 // size in KB
	MemoryCurrent uint64 // size in KB
}

func ParseMemoryInformationFromKV(
	output string,
	memoryMaximumKey string,
	memoryAvailableKey string,
	mapper func(field string) (uint64, error),
) (*MemoryInformation, error) {
	pairs := findColonSeparatedPairs(output, []string{memoryMaximumKey, memoryAvailableKey}, mapper)

	memoryMaximum, exists := pairs[memoryMaximumKey]
	if !exists {
		return nil, fmt.Errorf("parsing memory information: %w, key=%s", ErrKeyNotFound, memoryMaximumKey)
	}

	memoryAvailable, exists := pairs[memoryAvailableKey]
	if !exists {
		return nil, fmt.Errorf("parsing memory information: %w, key=%s", ErrKeyNotFound, memoryAvailableKey)
	}

	memoryCurrent := memoryMaximum - memoryAvailable

	return &MemoryInformation{memoryMaximum, memoryCurrent}, nil
}

func RawSystemInformation(osInfoRaw string, memoryInfoRaw string, fdInfoRaw string, disksRaw string) string {
	var builder strings.Builder

	formatInfo := func(info string, builder *strings.Builder) {
		if info == "" {
			builder.WriteString("No information\n")
		} else {
			builder.WriteString(info)
			builder.WriteString("\n")
		}
	}

	builder.WriteString("---BEGIN Operating system information\n")
	formatInfo(osInfoRaw, &builder)
	builder.WriteString("---END Operating system information\n")
	builder.WriteString("---BEGIN Memory information\n")
	formatInfo(memoryInfoRaw, &builder)
	builder.WriteString("---END Memory information\n")
	builder.WriteString("---BEGIN File descriptors information\n")
	formatInfo(fdInfoRaw, &builder)
	builder.WriteString("---END File descriptors information\n")
	builder.WriteString("---BEGIN Disks information\n")
	formatInfo(disksRaw, &builder)
	builder.WriteString("---END Disks information\n")

	rawInformation := builder.String()

	return rawInformation
}

func collectDiskVolumeInformationUnix(ctx context.Context) ([]*DiskVolumeInformation, string, error) {
	command := exec.CommandContext(ctx, "df", "-k")

	stdout, err := command.Output()
	if err != nil {
		return nil, "", fmt.Errorf("error retrieving output from command '%s': %w", command.String(), err)
	}

	output := string(stdout)

	disks, err := ParseDiskVolumeInformationOutput(output, 1, 1)
	if err != nil {
		return nil, output, err
	}

	// returning raw output in case other collected information
	// resulted in errors
	return disks, output, nil
}

func collectOSInformationUnix(ctx context.Context) (*OsInfo, string, error) {
	command := exec.CommandContext(ctx, "uname", "-a")

	stdout, err := command.Output()
	if err != nil {
		return nil, "", fmt.Errorf("error retrieving output from command '%s': %w", command.String(), err)
	}

	output := string(stdout)

	osInfo, err := ParseUnameOutput(output, runtime.GOOS)
	if err != nil {
		return nil, output, err
	}

	// returning raw output in case other collected information
	// resulted in errors
	return osInfo, output, nil
}
