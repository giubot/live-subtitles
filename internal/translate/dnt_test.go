// SPDX-License-Identifier: Apache-2.0

package translate

import (
	"context"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iencodev/live-subtitles/internal/api"
	"github.com/iencodev/live-subtitles/internal/domain"
)

func TestFindTerm(t *testing.T) {
	tests := []struct {
		s, term string
		want    [][2]int
	}{
		{"deploy to Kubernetes now", "kubernetes", [][2]int{{10, 20}}},
		{"a rapid API", "API", [][2]int{{8, 11}}},
		{"algo de Go", "go", [][2]int{{8, 10}}},
		{"Kubernetes, kubernetes.", "Kubernetes", [][2]int{{0, 10}, {12, 22}}},
		{"usá Node.js ya", "node.js", [][2]int{{5, 12}}},
		{"en la NUBE", "nube", [][2]int{{6, 10}}},
		{"ÁRBOL árbol", "árbol", [][2]int{{0, 6}, {7, 13}}},
		{"React-based", "React", [][2]int{{0, 5}}},
		{"Reactive", "React", nil},
		{"two APIs and pods", "API", [][2]int{{4, 7}}},
		{"los clústeres", "clúster", [][2]int{{4, 12}}},
		{"pods", "pod", [][2]int{{0, 3}}},
		{"podcast", "pod", nil},
		{"C++ rocks", "C++", [][2]int{{0, 3}}},
		{"anything", "", nil},
	}
	for _, tc := range tests {
		if got := findTerm(tc.s, tc.term); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("findTerm(%q, %q) = %v, want %v", tc.s, tc.term, got, tc.want)
		}
	}
}

func TestEnforceDoNotTranslate(t *testing.T) {
	g := &domain.Glossary{DoNotTranslate: []string{"Kubernetes", "React", "Google Cloud", "API"}}
	tests := []struct {
		name, src, out string
		want           string
		missing        []string
	}{
		{"kept", "Kubernetes and React", "Kubernetes y React", "Kubernetes y React", nil},
		{"re-cased", "Deploy on Google Cloud with Kubernetes", "Desplegá en google cloud con KUBERNETES",
			"Desplegá en Google Cloud con Kubernetes", nil},
		{"translated away", "We use React", "Usamos Reaccionar", "Usamos Reaccionar", []string{"React"}},
		{"only entries in the source count", "Hello", "Hola React", "Hola React", nil},
		{"whole words only", "a rapid test", "una prueba rápida", "una prueba rápida", nil},
		{"no glossary", "React", "Reaccionar", "Reaccionar", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gl := g
			if tc.name == "no glossary" {
				gl = nil
			}
			got, missing := EnforceDoNotTranslate(gl, tc.src, tc.out)
			if got != tc.want || !reflect.DeepEqual(missing, tc.missing) {
				t.Errorf("= %q, %v; want %q, %v", got, missing, tc.want, tc.missing)
			}
		})
	}
}

func TestMaskTerms(t *testing.T) {
	keep := []string{"React", "Kubernetes"}
	masked := maskTerms("React on kubernetes, with React", keep)
	if masked != "⟦1⟧ on ⟦2⟧, with ⟦1⟧" {
		t.Fatalf("masked = %q", masked)
	}
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"⟦1⟧ en ⟦2⟧, con ⟦1⟧", "React en Kubernetes, con React", true},
		{"⟦1⟧ en el clúster", "React en el clúster", false},
		{"⟦1⟧ en ⟦2⟧ ⟦3⟧", "React en Kubernetes ⟦3⟧", false},
	} {
		got, ok := unmaskTerms(tc.in, keep)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("unmask(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// dropTranslator "translates" React into Reaccionar unless it is masked,
// and loses the first placeholder unless keepPlaceholders is set.
type dropTranslator struct {
	keepPlaceholders bool

	mu   sync.Mutex
	reqs []domain.TranslateRequest
}

func (d *dropTranslator) Kind() domain.ProviderKind { return api.ProviderKindMock }

func (d *dropTranslator) Translate(_ context.Context, req domain.TranslateRequest) (domain.TranslateResult, error) {
	d.mu.Lock()
	d.reqs = append(d.reqs, req)
	d.mu.Unlock()
	out := strings.ReplaceAll(req.Text, "React", "Reaccionar")
	if !d.keepPlaceholders {
		out = strings.ReplaceAll(out, "⟦1⟧", "eso")
	}
	return domain.TranslateResult{Text: "es: " + out, Usage: domain.Usage{InputTokens: 1}}, nil
}

func TestFanoutKeepsDoNotTranslate(t *testing.T) {
	g := &domain.Glossary{DoNotTranslate: []string{"React", "Kubernetes"}}
	tests := []struct {
		name             string
		keepPlaceholders bool
		final            bool
		want             string
		calls            int
	}{
		{"final repaired by masking", true, true, "es 1 F es: React en Kubernetes", 2},
		{"repair that loses the mask keeps the first try", false, true, "es 1 F es: Reaccionar en Kubernetes", 2},
		{"interims are re-cased but not retried", true, false, "es 1 I es: Reaccionar en Kubernetes", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := &dropTranslator{keepPlaceholders: tc.keepPlaceholders}
			rec := &recorder{}
			f := New(t.Context(), Options{SessionID: "s", Translator: tr, Targets: []string{"es"}, Glossary: g,
				Publish: rec.publish, Usage: rec.addUsage, Logger: slog.New(slog.DiscardHandler)})
			f.Push(api.Caption{SegmentId: "1", Lang: "source", SourceLang: "en", Final: tc.final, Text: "React en kubernetes"})
			if !tc.final {
				// Close drops pending interims; wait for this one first.
				waitFor(t, func() bool { return len(rec.lines()) == 1 })
			}
			f.Close()
			got := rec.lines()
			if len(got) != 1 || got[0] != tc.want {
				t.Errorf("published %q, want %q", got, tc.want)
			}
			tr.mu.Lock()
			defer tr.mu.Unlock()
			if len(tr.reqs) != tc.calls {
				t.Fatalf("%d translation calls, want %d", len(tr.reqs), tc.calls)
			}
			if tc.calls == 2 && tr.reqs[1].Text != "⟦1⟧ en ⟦2⟧" {
				t.Errorf("repair request %q", tr.reqs[1].Text)
			}
			if rec.usage.InputTokens != int64(tc.calls) {
				t.Errorf("usage %+v, want %d calls counted", rec.usage, tc.calls)
			}
		})
	}
}
