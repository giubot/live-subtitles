// SPDX-License-Identifier: Apache-2.0

// Package whisper is the speech-recognition half of the local provider
// (AI-3): a client of whisper.cpp's `whisper-server` sidecar.
//
// whisper-server transcribes whole files, not streams, so the provider
// cuts the session audio into utterances with an energy VAD and posts each
// one to `POST /inference` as a 16 kHz mono WAV:
//
//   - While an utterance is in progress, it is re-transcribed every
//     VADConfig.InterimEvery of new audio for interim text. Only the latest
//     pending interim request is kept, so a slow server skips interims
//     instead of falling behind.
//   - A pause of VADConfig.Pause, or reaching VADConfig.MaxUtterance (cut at
//     the quietest window of the last seconds), commits the utterance as a
//     final event with the same segment ID. Finals are never dropped.
//   - Audio below the speech threshold (a fixed floor, or a margin above the
//     tracked noise floor) never reaches the server, and utterances with too
//     little voiced audio are dropped. What whisper still invents on noise
//     ("[Música]", "Thanks for watching", "Subtítulos realizados por la
//     comunidad de Amara.org", runs of repeated words) is filtered out.
//
// Language (AI-10): with SourceLanguage `auto` the request asks for
// `language=auto`, and the provider reads `language_probabilities` from the
// verbose_json answer and keeps the likelier of `en` and `es`. When whisper
// itself decided on a third language (so the text is in, say, Portuguese),
// the utterance is transcribed again pinned to the chosen one. Once an
// utterance has 2 s of audio its language is kept for its later interims.
// Limits: detection runs on the utterance audio only, so a very short
// utterance ("OK", "sí") can come out in the wrong language; hysteresis
// across segments is the session's job (P2-05). Servers started with
// --no-language-probabilities only report the detected language name; then
// anything but English or Spanish falls back to the previous segment's
// language. A pinned SourceLanguage skips detection.
//
// English-only models (`*.en`) are refused at Start with
// `provider.model_english_only`, both by the configured model name and by a
// probe: a multilingual model answers a `language=es` request as "spanish",
// an English-only one always as "english".
package whisper
