package diagnostic_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/diagnostic"
)

func TestParseMemoryInformationFromKV(t *testing.T) {
	t.Parallel()

	mapper := func(field string) (uint64, error) {
		value, err := strconv.ParseUint(field, 10, 64)
		return value, err
	}

	linuxMapper := func(field string) (uint64, error) {
		field = strings.TrimRight(field, " kB")
		return strconv.ParseUint(field, 10, 64)
	}

	windowsMemoryOutput := `

FreeVirtualMemory      : 5350472
TotalVirtualMemorySize : 8903424


`
	macosMemoryOutput := `hw.memsize: 38654705664
hw.memsize_usable: 38009012224`
	memoryOutputWithMissingKey := `hw.memsize: 38654705664`

	linuxMemoryOutput := `MemTotal:        8028860 kB
MemFree:          731396 kB
MemAvailable:    4678844 kB
Buffers:          472632 kB
Cached:          3186492 kB
SwapCached:         4196 kB
Active:          3088988 kB
Inactive:        3468560 kB`

	tests := []struct {
		name               string
		output             string
		memoryMaximumKey   string
		memoryAvailableKey string
		expected           *diagnostic.MemoryInformation
		expectedErr        bool
		mapper             func(string) (uint64, error)
	}{
		{
			name:               "parse linux memory values",
			output:             linuxMemoryOutput,
			memoryMaximumKey:   "MemTotal",
			memoryAvailableKey: "MemAvailable",
			expected: &diagnostic.MemoryInformation{
				8028860,
				8028860 - 4678844,
			},
			expectedErr: false,
			mapper:      linuxMapper,
		},
		{
			name:               "parse memory values with missing key",
			output:             memoryOutputWithMissingKey,
			memoryMaximumKey:   "hw.memsize",
			memoryAvailableKey: "hw.memsize_usable",
			expected:           nil,
			expectedErr:        true,
			mapper:             mapper,
		},
		{
			name:               "parse macos memory values",
			output:             macosMemoryOutput,
			memoryMaximumKey:   "hw.memsize",
			memoryAvailableKey: "hw.memsize_usable",
			expected: &diagnostic.MemoryInformation{
				38654705664,
				38654705664 - 38009012224,
			},
			expectedErr: false,
			mapper:      mapper,
		},
		{
			name:               "parse windows memory values",
			output:             windowsMemoryOutput,
			memoryMaximumKey:   "TotalVirtualMemorySize",
			memoryAvailableKey: "FreeVirtualMemory",
			expected: &diagnostic.MemoryInformation{
				8903424,
				8903424 - 5350472,
			},
			expectedErr: false,
			mapper:      mapper,
		},
	}

	for _, tCase := range tests {
		t.Run(tCase.name, func(t *testing.T) {
			t.Parallel()
			memoryInfo, err := diagnostic.ParseMemoryInformationFromKV(
				tCase.output,
				tCase.memoryMaximumKey,
				tCase.memoryAvailableKey,
				tCase.mapper,
			)

			if tCase.expectedErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tCase.expected, memoryInfo)
			}
		})
	}
}

func TestParseUnameOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		output      string
		os          string
		expected    *diagnostic.OsInfo
		expectedErr bool
	}{
		{
			name:   "darwin machine",
			output: "Darwin APC 23.6.0 Darwin Kernel Version 99.6.0: Wed Jul 31 20:48:04 PDT 1997; root:xnu-66666.666.6.666.6~1/RELEASE_ARM64_T6666 arm64",
			os:     "darwin",
			expected: &diagnostic.OsInfo{
				Architecture: "arm64",
				Name:         "APC",
				OsSystem:     "Darwin",
				OsRelease:    "Darwin Kernel Version 99.6.0: Wed Jul 31 20:48:04 PDT 1997; root:xnu-66666.666.6.666.6~1/RELEASE_ARM64_T6666",
				OsVersion:    "23.6.0",
			},
			expectedErr: false,
		},
		{
			name:   "linux machine",
			output: "Linux dab00d565591 6.6.31-linuxkit #1 SMP Thu May 23 08:36:57 UTC 2024 aarch64 GNU/Linux",
			os:     "linux",
			expected: &diagnostic.OsInfo{
				Architecture: "aarch64",
				Name:         "dab00d565591",
				OsSystem:     "Linux",
				OsRelease:    "#1 SMP Thu May 23 08:36:57 UTC 2024",
				OsVersion:    "6.6.31-linuxkit",
			},
			expectedErr: false,
		},
		{
			name:        "not enough fields",
			output:      "Linux ",
			os:          "linux",
			expected:    nil,
			expectedErr: true,
		},
	}

	for _, tCase := range tests {
		t.Run(tCase.name, func(t *testing.T) {
			t.Parallel()
			memoryInfo, err := diagnostic.ParseUnameOutput(
				tCase.output,
				tCase.os,
			)

			if tCase.expectedErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tCase.expected, memoryInfo)
			}
		})
	}
}

func TestParseFileDescriptorInformationFromKV(t *testing.T) {
	const (
		fileDescriptorMaximumKey = "kern.maxfiles"
		fileDescriptorCurrentKey = "kern.num_files"
	)

	t.Parallel()

	memoryOutput := `kern.maxfiles: 276480
kern.num_files: 11787`
	memoryOutputWithMissingKey := `kern.maxfiles: 276480`

	tests := []struct {
		name        string
		output      string
		expected    *diagnostic.FileDescriptorInformation
		expectedErr bool
	}{
		{
			name:        "parse memory values with missing key",
			output:      memoryOutputWithMissingKey,
			expected:    nil,
			expectedErr: true,
		},
		{
			name:   "parse macos memory values",
			output: memoryOutput,
			expected: &diagnostic.FileDescriptorInformation{
				276480,
				11787,
			},
			expectedErr: false,
		},
	}

	for _, tCase := range tests {
		t.Run(tCase.name, func(t *testing.T) {
			t.Parallel()
			fdInfo, err := diagnostic.ParseFileDescriptorInformationFromKV(
				tCase.output,
				fileDescriptorMaximumKey,
				fileDescriptorCurrentKey,
			)

			if tCase.expectedErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tCase.expected, fdInfo)
			}
		})
	}
}

func TestParseSysctlFileDescriptorInformation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		output      string
		expected    *diagnostic.FileDescriptorInformation
		expectedErr bool
	}{
		{
			name:   "expected output",
			output: "111 0 1111111",
			expected: &diagnostic.FileDescriptorInformation{
				FileDescriptorMaximum: 1111111,
				FileDescriptorCurrent: 111,
			},
			expectedErr: false,
		},
		{
			name:        "not enough fields",
			output:      "111 111 ",
			expected:    nil,
			expectedErr: true,
		},
	}

	for _, tCase := range tests {
		t.Run(tCase.name, func(t *testing.T) {
			t.Parallel()
			fdsInfo, err := diagnostic.ParseSysctlFileDescriptorInformation(
				tCase.output,
			)

			if tCase.expectedErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tCase.expected, fdsInfo)
			}
		})
	}
}

func TestParseWinOperatingSystemInfo(t *testing.T) {
	const (
		architecturePrefix = "OSArchitecture"
		osSystemPrefix     = "Caption"
		osVersionPrefix    = "Version"
		osReleasePrefix    = "BuildNumber"
		namePrefix         = "CSName"
	)

	t.Parallel()

	windowsIncompleteOsInfo := `
OSArchitecture         : ARM 64 bits
Caption                : Microsoft Windows 11 Home
Morekeys : 121314
CSName                 : UTILIZA-QO859QP
`
	windowsCompleteOsInfo := `
OSArchitecture         : ARM 64 bits
Caption                : Microsoft Windows 11 Home
Version                : 10.0.22631
BuildNumber            : 22631
Morekeys : 121314
CSName                 : UTILIZA-QO859QP
`

	tests := []struct {
		name        string
		output      string
		expected    *diagnostic.OsInfo
		expectedErr bool
	}{
		{
			name:   "expected output",
			output: windowsCompleteOsInfo,
			expected: &diagnostic.OsInfo{
				Architecture: "ARM 64 bits",
				Name:         "UTILIZA-QO859QP",
				OsSystem:     "Microsoft Windows 11 Home",
				OsRelease:    "22631",
				OsVersion:    "10.0.22631",
			},
			expectedErr: false,
		},
		{
			name:        "missing keys",
			output:      windowsIncompleteOsInfo,
			expected:    nil,
			expectedErr: true,
		},
	}

	for _, tCase := range tests {
		t.Run(tCase.name, func(t *testing.T) {
			t.Parallel()
			osInfo, err := diagnostic.ParseWinOperatingSystemInfo(
				tCase.output,
				architecturePrefix,
				osSystemPrefix,
				osVersionPrefix,
				osReleasePrefix,
				namePrefix,
			)

			if tCase.expectedErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tCase.expected, osInfo)
			}
		})
	}
}

func TestParseDiskVolumeInformationOutput(t *testing.T) {
	t.Parallel()

	invalidUnixDiskVolumeInfo := `Filesystem            Size  Used Avail Use% Mounted on
overlay                59G   19G   38G  33% /
tmpfs                  64M     0   64M   0% /dev
shm                    64M     0   64M   0% /dev/shm
/run/host_mark/Users  461G  266G  195G  58% /tmp/cloudflared
/dev/vda1              59G   19G   38G  33% /etc/hosts
tmpfs                 3.9G     0  3.9G   0% /sys/firmware
`

	unixDiskVolumeInfo := `Filesystem            Size  Used Avail Use% Mounted on
overlay               61202244  18881444  39179476  33% /
tmpfs                    65536         0     65536   0% /dev
shm                      65536         0     65536   0% /dev/shm
/run/host_mark/Users 482797652 278648468 204149184  58% /tmp/cloudflared
/dev/vda1             61202244  18881444  39179476  33% /etc/hosts
tmpfs                  4014428         0   4014428   0% /sys/firmware`
	missingFields := ` DeviceID        Size
--------        ----
C:       size
E:         235563008
Z:       67754782720
`
	invalidTypeField := ` DeviceID        Size   FreeSpace
--------        ----   ---------
C:       size 31318736896
D:
E:         235563008           0
Z:       67754782720 31318732800
`

	windowsDiskVolumeInfo := `

DeviceID        Size   FreeSpace
--------        ----   ---------
C:       67754782720 31318736896
E:         235563008           0
Z:       67754782720 31318732800`

	tests := []struct {
		name        string
		output      string
		expected    []*diagnostic.DiskVolumeInformation
		skipLines   int
		expectedErr bool
	}{
		{
			name:        "invalid unix disk volume information (numbers have units)",
			output:      invalidUnixDiskVolumeInfo,
			expected:    []*diagnostic.DiskVolumeInformation{},
			skipLines:   1,
			expectedErr: true,
		},
		{
			name:      "unix disk volume information",
			output:    unixDiskVolumeInfo,
			skipLines: 1,
			expected: []*diagnostic.DiskVolumeInformation{
				diagnostic.NewDiskVolumeInformation("overlay", 61202244, 18881444),
				diagnostic.NewDiskVolumeInformation("tmpfs", 65536, 0),
				diagnostic.NewDiskVolumeInformation("shm", 65536, 0),
				diagnostic.NewDiskVolumeInformation("/run/host_mark/Users", 482797652, 278648468),
				diagnostic.NewDiskVolumeInformation("/dev/vda1", 61202244, 18881444),
				diagnostic.NewDiskVolumeInformation("tmpfs", 4014428, 0),
			},
			expectedErr: false,
		},
		{
			name:   "windows disk volume information",
			output: windowsDiskVolumeInfo,
			expected: []*diagnostic.DiskVolumeInformation{
				diagnostic.NewDiskVolumeInformation("C:", 67754782720, 31318736896),
				diagnostic.NewDiskVolumeInformation("E:", 235563008, 0),
				diagnostic.NewDiskVolumeInformation("Z:", 67754782720, 31318732800),
			},
			skipLines:   4,
			expectedErr: false,
		},
		{
			name:        "insuficient fields",
			output:      missingFields,
			expected:    nil,
			skipLines:   2,
			expectedErr: true,
		},
		{
			name:        "invalid field",
			output:      invalidTypeField,
			expected:    nil,
			skipLines:   2,
			expectedErr: true,
		},
	}

	for _, tCase := range tests {
		t.Run(tCase.name, func(t *testing.T) {
			t.Parallel()
			disks, err := diagnostic.ParseDiskVolumeInformationOutput(tCase.output, tCase.skipLines, 1)

			if tCase.expectedErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tCase.expected, disks)
			}
		})
	}
}
