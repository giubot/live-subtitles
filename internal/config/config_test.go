// SPDX-License-Identifier: Apache-2.0

package config

import (
	"log/slog"
	"testing"

	"github.com/iencodev/live-subtitles/internal/metrics"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		want    Config
		wantErr bool
	}{
		{
			name: "defaults",
			want: Config{Addr: "0.0.0.0:8080", DataDir: "./data", LogFormat: "text", LogLevel: slog.LevelInfo, FFmpeg: "ffmpeg"},
		},
		{
			name: "env",
			env:  map[string]string{"LIVESUBS_ADDR": ":9090", "LIVESUBS_LOG_FORMAT": "JSON", "LIVESUBS_LOG_LEVEL": "debug"},
			want: Config{Addr: ":9090", DataDir: "./data", LogFormat: "json", LogLevel: slog.LevelDebug, FFmpeg: "ffmpeg"},
		},
		{
			name: "flag beats env",
			args: []string{"-addr", ":7070", "-public-base-url", "https://subs.example.com"},
			env:  map[string]string{"LIVESUBS_ADDR": ":9090"},
			want: Config{Addr: ":7070", DataDir: "./data", LogFormat: "text", LogLevel: slog.LevelInfo, PublicBaseURL: "https://subs.example.com", FFmpeg: "ffmpeg"},
		},
		{
			name: "admin token and keychain from env",
			env:  map[string]string{"LIVESUBS_ADMIN_TOKEN": "0123456789abcdef", "LIVESUBS_NO_KEYCHAIN": "true"},
			want: Config{Addr: "0.0.0.0:8080", DataDir: "./data", LogFormat: "text", LogLevel: slog.LevelInfo, AdminToken: "0123456789abcdef", NoKeychain: true, FFmpeg: "ffmpeg"},
		},
		{
			name: "no-keychain and ffmpeg flags",
			args: []string{"-no-keychain", "-ffmpeg", "/opt/ffmpeg/bin/ffmpeg"},
			want: Config{Addr: "0.0.0.0:8080", DataDir: "./data", LogFormat: "text", LogLevel: slog.LevelInfo, NoKeychain: true, FFmpeg: "/opt/ffmpeg/bin/ffmpeg"},
		},
		{
			name: "gemini prices from env and flags",
			args: []string{"-gemini-audio-usd-per-min", "0.01"},
			env:  map[string]string{"LIVESUBS_GEMINI_INPUT_USD_PER_MTOK": "0.5", "LIVESUBS_GEMINI_OUTPUT_USD_PER_MTOK": "0", "LIVESUBS_GEMINI_AUDIO_USD_PER_MIN": "1"},
			want: Config{Addr: "0.0.0.0:8080", DataDir: "./data", LogFormat: "text", LogLevel: slog.LevelInfo, FFmpeg: "ffmpeg",
				GeminiPrices: metrics.Prices{InputPerMTok: 0.5, OutputPerMTok: 0, AudioPerMin: 0.01}},
		},
		{name: "bad price", env: map[string]string{"LIVESUBS_GEMINI_AUDIO_USD_PER_MIN": "cheap"}, wantErr: true},
		{name: "negative price", args: []string{"-gemini-input-usd-per-mtok", "-1"}, wantErr: true},
		{name: "short admin token", env: map[string]string{"LIVESUBS_ADMIN_TOKEN": "short"}, wantErr: true},
		{name: "bad keychain bool", env: map[string]string{"LIVESUBS_NO_KEYCHAIN": "maybe"}, wantErr: true},
		{name: "bad level", args: []string{"-log-level", "loud"}, wantErr: true},
		{name: "bad format", env: map[string]string{"LIVESUBS_LOG_FORMAT": "xml"}, wantErr: true},
		{name: "stray argument", args: []string{"serve"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(tt.args, func(k string) string { return tt.env[k] })
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.want.GeminiPrices == (metrics.Prices{}) { // cases that don't set prices expect the defaults
				tt.want.GeminiPrices = metrics.DefaultGeminiPrices
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
