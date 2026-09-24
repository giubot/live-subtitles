// SPDX-License-Identifier: Apache-2.0

package config

import (
	"log/slog"
	"testing"
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
			want: Config{Addr: "0.0.0.0:8080", DataDir: "./data", LogFormat: "text", LogLevel: slog.LevelInfo},
		},
		{
			name: "env",
			env:  map[string]string{"LIVESUBS_ADDR": ":9090", "LIVESUBS_LOG_FORMAT": "JSON", "LIVESUBS_LOG_LEVEL": "debug"},
			want: Config{Addr: ":9090", DataDir: "./data", LogFormat: "json", LogLevel: slog.LevelDebug},
		},
		{
			name: "flag beats env",
			args: []string{"-addr", ":7070", "-public-base-url", "https://subs.example.com"},
			env:  map[string]string{"LIVESUBS_ADDR": ":9090"},
			want: Config{Addr: ":7070", DataDir: "./data", LogFormat: "text", LogLevel: slog.LevelInfo, PublicBaseURL: "https://subs.example.com"},
		},
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
			if !tt.wantErr && got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
