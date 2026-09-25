// SPDX-License-Identifier: Apache-2.0

package hwcheck

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/iencodev/live-subtitles/internal/api"
)

const vulkanSummary = `==========
VULKANINFO
==========

Devices:
========
GPU0:
	apiVersion         = 1.3.255
	vendorID           = 0x1002
	deviceType         = PHYSICAL_DEVICE_TYPE_INTEGRATED_GPU
	deviceName         = AMD Radeon 780M (RADV PHOENIX)
GPU1:
	apiVersion         = 1.3.255
	deviceType         = PHYSICAL_DEVICE_TYPE_CPU
	deviceName         = llvmpipe (LLVM 17.0.6, 256 bits)
GPU2:
	deviceType         = PHYSICAL_DEVICE_TYPE_DISCRETE_GPU
	deviceName         = NVIDIA GeForce RTX 4070
`

const cpuinfoX86 = `processor	: 0
vendor_id	: GenuineIntel
model name	: Intel(R) Core(TM) i7-1260P
processor	: 1
model name	: Intel(R) Core(TM) i7-1260P
`

// fakeSystem answers commands and files from maps; anything else fails.
func fakeSystem(goos, goarch string, cores int, cmds map[string]string, files map[string]string) system {
	return system{
		goos: goos, goarch: goarch, numCPU: cores,
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			key := strings.TrimSpace(name + " " + strings.Join(args, " "))
			for k, v := range cmds {
				if strings.HasPrefix(key, k) {
					return []byte(v), nil
				}
			}
			return nil, errors.New("exec: not found")
		},
		readFile: func(name string) ([]byte, error) {
			if v, ok := files[name]; ok {
				return []byte(v), nil
			}
			return nil, os.ErrNotExist
		},
		getenv: func(k string) string {
			if k == "PROCESSOR_IDENTIFIER" && goos == "windows" {
				return "Intel64 Family 6 Model 186"
			}
			return ""
		},
	}
}

func TestDetect(t *testing.T) {
	for _, c := range []struct {
		name string
		sys  system
		want Hardware
	}{
		{
			name: "apple silicon",
			sys: fakeSystem("darwin", "arm64", 12, map[string]string{
				"sysctl -n machdep.cpu.brand_string": "Apple M5 Pro\n",
				"sysctl -n hw.memsize":               "51539607552\n",
			}, nil),
			want: Hardware{OS: "darwin", Arch: "arm64", CPUModel: "Apple M5 Pro", Cores: 12, MemoryBytes: 48 * gib,
				GPUs: []GPU{{Name: "Apple M5 Pro GPU", Backend: api.Metal}}},
		},
		{
			name: "intel mac: no metal",
			sys: fakeSystem("darwin", "amd64", 8, map[string]string{
				"sysctl -n machdep.cpu.brand_string": "Intel(R) Core(TM) i9",
				"sysctl -n hw.memsize":               "17179869184",
			}, nil),
			want: Hardware{OS: "darwin", Arch: "amd64", CPUModel: "Intel(R) Core(TM) i9", Cores: 8, MemoryBytes: 16 * gib},
		},
		{
			name: "linux with nvidia and vulkan",
			sys: fakeSystem("linux", "amd64", 16, map[string]string{
				"nvidia-smi":           "NVIDIA GeForce RTX 4070, 12282\n",
				"vulkaninfo --summary": vulkanSummary,
			}, map[string]string{
				"/proc/cpuinfo": cpuinfoX86,
				"/proc/meminfo": "MemTotal:       32768000 kB\nMemFree: 1 kB\n",
			}),
			want: Hardware{OS: "linux", Arch: "amd64", CPUModel: "Intel(R) Core(TM) i7-1260P", Cores: 16, MemoryBytes: 32768000 << 10,
				GPUs: []GPU{
					{Name: "NVIDIA GeForce RTX 4070", Backend: api.Cuda, MemoryBytes: 12282 << 20},
					{Name: "AMD Radeon 780M (RADV PHOENIX)", Backend: api.Vulkan},
				}},
		},
		{
			name: "linux arm board, no tools",
			sys: fakeSystem("linux", "arm64", 4, nil, map[string]string{
				"/proc/cpuinfo": "processor : 0\nBogoMIPS : 108.00\nModel : Raspberry Pi 5 Model B Rev 1.0\n",
				"/proc/meminfo": "MemTotal: 8000000 kB\n",
			}),
			want: Hardware{OS: "linux", Arch: "arm64", CPUModel: "Raspberry Pi 5 Model B Rev 1.0", Cores: 4, MemoryBytes: 8000000 << 10},
		},
		{
			name: "windows",
			sys: fakeSystem("windows", "amd64", 8, map[string]string{
				"powershell": "34359738368\r\n",
			}, nil),
			want: Hardware{OS: "windows", Arch: "amd64", CPUModel: "Intel64 Family 6 Model 186", Cores: 8, MemoryBytes: 32 * gib},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.sys.detect(t.Context()); !reflect.DeepEqual(got, c.want) {
				t.Errorf("detect =\n %+v\nwant\n %+v", got, c.want)
			}
		})
	}
}

func TestParseVulkanSummary(t *testing.T) {
	got := parseVulkanSummary([]byte(vulkanSummary))
	want := []GPU{
		{Name: "AMD Radeon 780M (RADV PHOENIX)", Backend: api.Vulkan},
		{Name: "NVIDIA GeForce RTX 4070", Backend: api.Vulkan},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseVulkanSummary = %+v, want %+v", got, want)
	}
}

func TestParseNvidiaSMI(t *testing.T) {
	got := parseNvidiaSMI([]byte("NVIDIA A10, 23028\nTesla T4, [N/A]\n\n"))
	want := []GPU{
		{Name: "NVIDIA A10", Backend: api.Cuda, MemoryBytes: 23028 << 20},
		{Name: "Tesla T4", Backend: api.Cuda},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseNvidiaSMI = %+v, want %+v", got, want)
	}
}

func TestRecommend(t *testing.T) {
	for _, c := range []struct {
		name string
		hw   Hardware
		want Recommendation
	}{
		{"apple 48 GB", Hardware{Cores: 12, MemoryBytes: 48 * gib, GPUs: []GPU{{Backend: api.Metal}}},
			Recommendation{"large-v3-turbo", "gemma3:4b", true}},
		{"apple 16 GB", Hardware{Cores: 8, MemoryBytes: 16 * gib, GPUs: []GPU{{Backend: api.Metal}}},
			Recommendation{"large-v3-turbo", "gemma3:4b", true}},
		{"apple 8 GB", Hardware{Cores: 8, MemoryBytes: 8 * gib, GPUs: []GPU{{Backend: api.Metal}}},
			Recommendation{"large-v3-turbo", "gemma3:1b", true}},
		{"cuda 12 GB", Hardware{Cores: 8, MemoryBytes: 16 * gib, GPUs: []GPU{{Backend: api.Cuda, MemoryBytes: 12 * gib}}},
			Recommendation{"large-v3-turbo", "gemma3:4b", true}},
		{"cuda 4 GB on a big box", Hardware{Cores: 16, MemoryBytes: 64 * gib, GPUs: []GPU{{Backend: api.Cuda, MemoryBytes: 4 * gib}}},
			Recommendation{"large-v3-turbo", "gemma3:1b", true}},
		{"cuda 2 GB", Hardware{Cores: 4, MemoryBytes: 8 * gib, GPUs: []GPU{{Backend: api.Cuda, MemoryBytes: 2 * gib}}},
			Recommendation{"small", "gemma3:1b", true}},
		{"cuda beats vulkan", Hardware{Cores: 8, MemoryBytes: 32 * gib, GPUs: []GPU{
			{Backend: api.Vulkan}, {Backend: api.Cuda, MemoryBytes: 6 * gib}}},
			Recommendation{"large-v3-turbo", "gemma3:1b", true}},
		{"integrated vulkan, 32 GB", Hardware{Cores: 8, MemoryBytes: 32 * gib, GPUs: []GPU{{Backend: api.Vulkan}}},
			Recommendation{"large-v3-turbo", "gemma3:4b", true}},
		{"cpu 16 cores 32 GB", Hardware{Cores: 16, MemoryBytes: 32 * gib},
			Recommendation{"small", "gemma3:1b", true}},
		{"cpu 4 cores 8 GB", Hardware{Cores: 4, MemoryBytes: 8 * gib},
			Recommendation{"small", "gemma3:1b", false}},
		{"unknown", Hardware{}, Recommendation{"small", "gemma3:1b", false}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := Recommend(c.hw); got != c.want {
				t.Errorf("Recommend = %+v, want %+v", got, c.want)
			}
		})
	}
}
