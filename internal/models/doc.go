// SPDX-License-Identifier: Apache-2.0

// Package models catalogs and downloads the local provider's models
// (AI-13): whisper.cpp GGML files from Hugging Face, resumed with HTTP
// Range requests and checked against their SHA-256, and Gemma through
// Ollama's pull API. Progress is published for /ws/admin.
package models
