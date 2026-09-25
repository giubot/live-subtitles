// SPDX-License-Identifier: Apache-2.0

package hwcheck

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
)

// cmdTimeout bounds each probe command (sysctl, nvidia-smi, vulkaninfo…).
const cmdTimeout = 5 * time.Second

// GPU is one graphics device the local runtimes can use.
type GPU struct {
	Name    string
	Backend api.HardwareReportGpusBackend
	// MemoryBytes is dedicated video memory; 0 when unknown or unified.
	MemoryBytes int64
}

// Hardware is what the machine offers the local provider.
type Hardware struct {
	OS, Arch    string
	CPUModel    string
	Cores       int
	MemoryBytes int64
	GPUs        []GPU
}

// system is the OS seen by Detect, replaced in tests.
type system struct {
	goos, goarch string
	numCPU       int
	run          func(ctx context.Context, name string, args ...string) ([]byte, error)
	readFile     func(name string) ([]byte, error)
	getenv       func(string) string
}

func hostSystem() system {
	return system{
		goos: runtime.GOOS, goarch: runtime.GOARCH, numCPU: runtime.NumCPU(),
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			ctx, cancel := context.WithTimeout(ctx, cmdTimeout)
			defer cancel()
			return exec.CommandContext(ctx, name, args...).Output()
		},
		readFile: os.ReadFile,
		getenv:   os.Getenv,
	}
}

// detect reads the CPU, memory and GPUs. Every probe is best effort: a
// missing tool leaves its field empty.
func (s system) detect(ctx context.Context) Hardware {
	h := Hardware{OS: s.goos, Arch: s.goarch, Cores: s.numCPU}
	switch s.goos {
	case "darwin":
		h.CPUModel = s.sysctl(ctx, "machdep.cpu.brand_string")
		h.MemoryBytes, _ = strconv.ParseInt(s.sysctl(ctx, "hw.memsize"), 10, 64)
		if s.goarch == "arm64" {
			// Apple Silicon: whisper.cpp and Ollama use Metal on the
			// integrated GPU, which shares the system memory.
			name := "Apple GPU"
			if h.CPUModel != "" {
				name = h.CPUModel + " GPU"
			}
			h.GPUs = append(h.GPUs, GPU{Name: name, Backend: api.Metal})
		}
	case "linux":
		if b, err := s.readFile("/proc/cpuinfo"); err == nil {
			h.CPUModel = cpuinfoModel(b)
		}
		if b, err := s.readFile("/proc/meminfo"); err == nil {
			h.MemoryBytes = meminfoTotal(b)
		}
	case "windows":
		h.CPUModel = strings.TrimSpace(s.getenv("PROCESSOR_IDENTIFIER"))
		if out, err := s.run(ctx, "powershell", "-NoProfile", "-Command",
			"(Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory"); err == nil {
			h.MemoryBytes, _ = strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
		}
	}
	if s.goos != "darwin" {
		cuda := s.nvidiaGPUs(ctx)
		h.GPUs = append(h.GPUs, cuda...)
		h.GPUs = append(h.GPUs, s.vulkanGPUs(ctx, cuda)...)
	}
	return h
}

func (s system) sysctl(ctx context.Context, name string) string {
	out, err := s.run(ctx, "sysctl", "-n", name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// nvidiaGPUs lists CUDA devices from nvidia-smi.
func (s system) nvidiaGPUs(ctx context.Context) []GPU {
	out, err := s.run(ctx, "nvidia-smi", "--query-gpu=name,memory.total", "--format=csv,noheader,nounits")
	if err != nil {
		return nil
	}
	return parseNvidiaSMI(out)
}

// vulkanGPUs lists Vulkan devices from vulkaninfo, leaving out software
// renderers and the GPUs nvidia-smi already reported (CUDA is faster).
func (s system) vulkanGPUs(ctx context.Context, cuda []GPU) []GPU {
	out, err := s.run(ctx, "vulkaninfo", "--summary")
	if err != nil {
		return nil
	}
	var gpus []GPU
	for _, g := range parseVulkanSummary(out) {
		dup := false
		for _, c := range cuda {
			if strings.EqualFold(c.Name, g.Name) {
				dup = true
			}
		}
		if !dup {
			gpus = append(gpus, g)
		}
	}
	return gpus
}

// parseNvidiaSMI reads `nvidia-smi --query-gpu=name,memory.total
// --format=csv,noheader,nounits` (memory in MiB).
func parseNvidiaSMI(out []byte) []GPU {
	var gpus []GPU
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		name, mem, _ := strings.Cut(sc.Text(), ",")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		g := GPU{Name: name, Backend: api.Cuda}
		if mib, err := strconv.ParseInt(strings.TrimSpace(mem), 10, 64); err == nil {
			g.MemoryBytes = mib << 20
		}
		gpus = append(gpus, g)
	}
	return gpus
}

var (
	vulkanDevice = regexp.MustCompile(`^GPU\d+:\s*$`)
	vulkanField  = regexp.MustCompile(`^\s*(deviceName|deviceType)\s*=\s*(.+?)\s*$`)
)

// parseVulkanSummary reads the "GPU<n>:" blocks of `vulkaninfo --summary`,
// skipping CPU devices such as llvmpipe.
func parseVulkanSummary(out []byte) []GPU {
	var gpus []GPU
	var name, typ string
	flush := func() {
		if name != "" && !strings.Contains(typ, "CPU") {
			gpus = append(gpus, GPU{Name: name, Backend: api.Vulkan})
		}
		name, typ = "", ""
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if vulkanDevice.MatchString(line) {
			flush()
			continue
		}
		if m := vulkanField.FindStringSubmatch(line); m != nil {
			if m[1] == "deviceName" {
				name = m[2]
			} else {
				typ = m[2]
			}
		}
	}
	flush()
	return gpus
}

// cpuinfoModel reads the CPU name from /proc/cpuinfo: "model name" on x86,
// "Model" or "Hardware" on ARM boards.
func cpuinfoModel(b []byte) string {
	found := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if _, seen := found[k]; !seen && v != "" {
			found[k] = v
		}
	}
	for _, k := range []string{"model name", "Model", "Hardware", "cpu model"} {
		if v := found[k]; v != "" {
			return v
		}
	}
	return ""
}

// meminfoTotal reads MemTotal (kB) from /proc/meminfo, in bytes.
func meminfoTotal(b []byte) int64 {
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) >= 2 && f[0] == "MemTotal:" {
			kb, err := strconv.ParseInt(f[1], 10, 64)
			if err == nil {
				return kb << 10
			}
		}
	}
	return 0
}
