// SPDX-License-Identifier: Apache-2.0

package mock

// Line is one scripted sentence in both languages.
type Line struct {
	EN, ES string
}

// DefaultScript is a short conference talk. With source language `auto`,
// the ASR speaks it in blocks of three lines per language, so language
// switches show up in the pipeline.
var DefaultScript = []Line{
	{"Welcome everyone, and thanks for joining this session.", "Bienvenidos a todos, y gracias por sumarse a esta sesión."},
	{"Today we are going to talk about observability in Kubernetes.", "Hoy vamos a hablar de observabilidad en Kubernetes."},
	{"First, let's look at how metrics, logs and traces fit together.", "Primero, veamos cómo se combinan métricas, logs y trazas."},
	{"Most outages start with a small signal that nobody was watching.", "La mayoría de las caídas empiezan con una señal chica que nadie miraba."},
	{"So the goal is to make those signals visible early.", "Entonces el objetivo es hacer visibles esas señales temprano."},
	{"Let's start with a quick demo on a real cluster.", "Empecemos con una demo rápida en un cluster real."},
	{"As you can see, latency went up right after the deploy.", "Como pueden ver, la latencia subió justo después del deploy."},
	{"Any questions before we move on to tracing?", "¿Alguna pregunta antes de pasar a trazas?"},
}
