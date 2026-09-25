// SPDX-License-Identifier: Apache-2.0

package hwcheck

import (
	"slices"

	"github.com/iencodev/live-subtitles/internal/api"
)

const gib = 1 << 30

// Recommendation is the model pair suggested for the hardware (AI-12).
type Recommendation struct {
	WhisperModel string // a whisper.cpp model name: large-v3-turbo, medium, small
	GemmaModel   string // an Ollama tag: gemma3:4b, gemma3:1b
	// RealtimeLikely is the table's guess that the local provider keeps up
	// with live audio; a benchmark replaces it with a measurement.
	RealtimeLikely bool
}

// rule is one row of the recommendation table; the first row whose
// conditions hold wins.
type rule struct {
	backends []api.HardwareReportGpusBackend // any of these; nil: no GPU needed
	budget   int64                           // minimum model memory (see modelBudget)
	cores    int                             // minimum CPU cores
	rec      Recommendation
}

// recommendations is the table. Sizes, loaded: large-v3-turbo ~1.6 GB,
// small ~0.5 GB; gemma3:4b ~3.3 GB, gemma3:1b ~0.8 GB. On a GPU whisper
// large-v3-turbo runs several times faster than real time, so the GPU's
// memory decides the Gemma size. On a CPU only whisper small keeps up.
var recommendations = []rule{
	{backends: gpuBackends, budget: 8 * gib, rec: Recommendation{"large-v3-turbo", "gemma3:4b", true}},
	{backends: gpuBackends, budget: 4 * gib, rec: Recommendation{"large-v3-turbo", "gemma3:1b", true}},
	{backends: gpuBackends, rec: Recommendation{"small", "gemma3:1b", true}},
	{cores: 8, budget: 8 * gib, rec: Recommendation{"small", "gemma3:1b", true}},
	{rec: Recommendation{"small", "gemma3:1b", false}},
}

var gpuBackends = []api.HardwareReportGpusBackend{api.Metal, api.Cuda, api.Vulkan}

// Recommend picks the models for h from the table.
func Recommend(h Hardware) Recommendation {
	backend, budget := modelBudget(h)
	for _, r := range recommendations {
		if r.backends != nil && !slices.Contains(r.backends, backend) {
			continue
		}
		if budget >= r.budget && h.Cores >= r.cores {
			return r.rec
		}
	}
	return recommendations[len(recommendations)-1].rec
}

// modelBudget is the best backend and the memory the models may use on
// it: a discrete GPU's own memory, or half the system memory for unified
// memory (Apple Silicon), a GPU of unknown memory and the CPU.
func modelBudget(h Hardware) (api.HardwareReportGpusBackend, int64) {
	best, budget := api.None, h.MemoryBytes/2
	rank := map[api.HardwareReportGpusBackend]int{api.None: 0, api.Vulkan: 1, api.Metal: 2, api.Cuda: 2}
	for _, g := range h.GPUs {
		b := h.MemoryBytes / 2
		if g.MemoryBytes > 0 {
			b = g.MemoryBytes
		}
		if rank[g.Backend] > rank[best] || (rank[g.Backend] == rank[best] && b > budget) {
			best, budget = g.Backend, b
		}
	}
	return best, budget
}
