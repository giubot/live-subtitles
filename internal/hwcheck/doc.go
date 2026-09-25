// SPDX-License-Identifier: Apache-2.0

// Package hwcheck reports hardware capabilities and benchmarks the local
// pipeline (AI-12): CPU, memory and GPUs (Metal on Apple Silicon, CUDA via
// nvidia-smi, Vulkan via vulkaninfo), whether whisper-server, Ollama and
// ffmpeg answer, a model recommendation table, and a real-time-factor
// benchmark on a bundled clip. It is pure Go: probes read /proc or run
// sysctl and the vendor tools, and a missing tool only leaves its part of
// the report empty.
package hwcheck
