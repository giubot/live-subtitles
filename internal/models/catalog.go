// SPDX-License-Identifier: Apache-2.0

package models

import "github.com/iencodev/live-subtitles/internal/api"

// HuggingFaceBase is where whisper.cpp's GGML models are downloaded from.
const HuggingFaceBase = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main"

// Model is one catalog entry.
type Model struct {
	ID   string
	Kind api.LocalModelKind
	// Name is what the settings call it: the whisper model name
	// (providers.local.whisperModel) or the Ollama tag (gemmaModel).
	Name string
	// Size in bytes: exact for whisper files, the registry total for Gemma.
	Size int64
	// SHA256 of the whisper file, as Hugging Face publishes it (Gemma
	// layers are verified by Ollama itself).
	SHA256 string
}

// File is the whisper model's file name in the models directory, the name
// whisper-server's --model and scripts/pull-models.sh use.
func (m Model) File() string { return "ggml-" + m.Name + ".bin" }

// Catalog lists the models the setup wizard offers (AI-13): multilingual
// whisper models only (Spanish needs them, never *.en), and Gemma 3 in the
// sizes the hardware check recommends.
var Catalog = []Model{
	{ID: "whisper-large-v3-turbo", Kind: api.Whisper, Name: "large-v3-turbo", Size: 1624555275,
		SHA256: "1fc70f774d38eb169993ac391eea357ef47c88757ef72ee5943879b7e8e2bc69"},
	{ID: "whisper-medium", Kind: api.Whisper, Name: "medium", Size: 1533763059,
		SHA256: "6c14d5adee5f86394037b4e4e8b59f1673b6cee10e3cf0b11bbdbee79c156208"},
	{ID: "whisper-small", Kind: api.Whisper, Name: "small", Size: 487601967,
		SHA256: "1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b"},
	{ID: "gemma3-4b", Kind: api.Gemma, Name: "gemma3:4b", Size: 3338801804},
	{ID: "gemma3-1b", Kind: api.Gemma, Name: "gemma3:1b", Size: 815319791},
}
