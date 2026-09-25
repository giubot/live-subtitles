// SPDX-License-Identifier: Apache-2.0
import type { Schemas } from '../../api/types'

export type Settings = Schemas['Settings']
export type SourceLanguage = Schemas['SourceLanguage']
export type Bitrate = Settings['recording']['bitrateKbps']

export const bitrates: Bitrate[] = [32, 48, 64]

/** The Settings form, flat. Numbers stay strings while typing. */
export interface SettingsValues {
  sourceLanguage: SourceLanguage
  targetLanguages: string[]
  liveModel: string
  translationModel: string
  whisperUrl: string
  whisperModel: string
  ollamaUrl: string
  gemmaModel: string
  fallback: boolean
  contextSentences: string
  maxCharsPerLine: string
  maxLines: string
  publicBaseUrl: string
  preferredInterface: string
  recordingEnabled: boolean
  bitrateKbps: Bitrate
  retentionDays: string
  obsUrl: string
  srtEnabled: boolean
  srtPort: string
  srtLatencyMs: string
}

export type Field = keyof SettingsValues

/** Each field's path in the Settings body, to match the server's `fields` errors. */
export const fieldPath: Record<Field, string> = {
  sourceLanguage: 'defaultSourceLanguage',
  targetLanguages: 'defaultTargetLanguages',
  liveModel: 'providers.gemini.liveModel',
  translationModel: 'providers.gemini.translationModel',
  whisperUrl: 'providers.local.whisperUrl',
  whisperModel: 'providers.local.whisperModel',
  ollamaUrl: 'providers.local.ollamaUrl',
  gemmaModel: 'providers.local.gemmaModel',
  fallback: 'providers.fallback',
  contextSentences: 'translation.contextSentences',
  maxCharsPerLine: 'captions.maxCharsPerLine',
  maxLines: 'captions.maxLines',
  publicBaseUrl: 'network.publicBaseUrl',
  preferredInterface: 'network.preferredInterface',
  recordingEnabled: 'recording.enabledByDefault',
  bitrateKbps: 'recording.bitrateKbps',
  retentionDays: 'recording.retentionDays',
  obsUrl: 'obs.websocketUrl',
  srtEnabled: 'srt.enabled',
  srtPort: 'srt.port',
  srtLatencyMs: 'srt.latencyMs',
}

const str = (n: number | undefined, fallback: number) => String(n ?? fallback)

/** Form values from the server's settings, with the spec's defaults for gaps. */
export function valuesFrom(s: Settings): SettingsValues {
  return {
    sourceLanguage: s.defaultSourceLanguage ?? 'auto',
    targetLanguages: s.defaultTargetLanguages,
    liveModel: s.providers.gemini.liveModel,
    translationModel: s.providers.gemini.translationModel,
    whisperUrl: s.providers.local.whisperUrl,
    whisperModel: s.providers.local.whisperModel,
    ollamaUrl: s.providers.local.ollamaUrl,
    gemmaModel: s.providers.local.gemmaModel,
    fallback: s.providers.fallback ?? false,
    contextSentences: str(s.translation?.contextSentences, 3),
    maxCharsPerLine: str(s.captions?.maxCharsPerLine, 42),
    maxLines: str(s.captions?.maxLines, 2),
    publicBaseUrl: s.network.publicBaseUrl ?? '',
    preferredInterface: s.network.preferredInterface ?? '',
    recordingEnabled: s.recording.enabledByDefault,
    bitrateKbps: s.recording.bitrateKbps,
    retentionDays: String(s.recording.retentionDays),
    obsUrl: s.obs?.websocketUrl ?? 'ws://127.0.0.1:4455',
    srtEnabled: s.srt?.enabled ?? true,
    srtPort: str(s.srt?.port, 9000),
    srtLatencyMs: str(s.srt?.latencyMs, 200),
  }
}

/** The PUT body: the loaded settings with the form's values on top. */
export function toSettings(v: SettingsValues, base: Settings): Settings {
  return {
    ...base,
    defaultSourceLanguage: v.sourceLanguage,
    defaultTargetLanguages: v.targetLanguages,
    providers: {
      ...base.providers,
      gemini: { liveModel: v.liveModel.trim(), translationModel: v.translationModel.trim() },
      local: {
        whisperUrl: v.whisperUrl.trim(),
        whisperModel: v.whisperModel.trim(),
        ollamaUrl: v.ollamaUrl.trim(),
        gemmaModel: v.gemmaModel.trim(),
      },
      fallback: v.fallback,
    },
    translation: { contextSentences: Number(v.contextSentences) },
    captions: {
      maxCharsPerLine: Number(v.maxCharsPerLine),
      maxLines: Number(v.maxLines),
    },
    network: {
      ...base.network,
      publicBaseUrl: v.publicBaseUrl.trim() || null,
      preferredInterface: v.preferredInterface || null,
    },
    recording: {
      enabledByDefault: v.recordingEnabled,
      bitrateKbps: v.bitrateKbps,
      retentionDays: Number(v.retentionDays),
    },
    obs: { websocketUrl: v.obsUrl.trim() },
    // passphraseSet is read-only, so it isn't sent back.
    srt: {
      enabled: v.srtEnabled,
      port: Number(v.srtPort),
      latencyMs: Number(v.srtLatencyMs),
    },
  }
}

export type Problem =
  | { kind: 'required' }
  | { kind: 'integer'; min: number; max: number }
  | { kind: 'url' }
  | { kind: 'wsUrl' }
  | { kind: 'languages' }

const ranges: Partial<Record<Field, [number, number]>> = {
  contextSentences: [0, 10],
  maxCharsPerLine: [10, 120],
  maxLines: [1, 4],
  retentionDays: [0, 3650],
  srtPort: [1, 65535],
  srtLatencyMs: [20, 8000],
}

const required: Field[] = ['liveModel', 'translationModel', 'whisperModel', 'gemmaModel']

function isUrl(value: string, schemes: string[]) {
  try {
    return schemes.includes(new URL(value).protocol)
  } catch {
    return false
  }
}

/** Client-side checks, mirroring api/openapi.yaml § Settings. */
export function validate(v: SettingsValues): Partial<Record<Field, Problem>> {
  const out: Partial<Record<Field, Problem>> = {}
  for (const f of required) if (!String(v[f]).trim()) out[f] = { kind: 'required' }
  for (const [f, [min, max]] of Object.entries(ranges) as [Field, [number, number]][]) {
    const raw = String(v[f]).trim()
    const n = Number(raw)
    if (!/^\d+$/.test(raw) || n < min || n > max) out[f] = { kind: 'integer', min, max }
  }
  for (const f of ['whisperUrl', 'ollamaUrl'] as const)
    if (!isUrl(v[f].trim(), ['http:', 'https:'])) out[f] = { kind: 'url' }
  if (v.publicBaseUrl.trim() && !isUrl(v.publicBaseUrl.trim(), ['http:', 'https:']))
    out.publicBaseUrl = { kind: 'url' }
  if (!isUrl(v.obsUrl.trim(), ['ws:', 'wss:'])) out.obsUrl = { kind: 'wsUrl' }
  if (v.targetLanguages.length === 0) out.targetLanguages = { kind: 'languages' }
  return out
}
