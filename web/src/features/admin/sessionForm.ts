// SPDX-License-Identifier: Apache-2.0
import type { Schemas } from '../../api/types'
import type { ChipStatus } from '../../components/StatusChip'

export type Session = Schemas['Session']
export type SessionCreate = Schemas['SessionCreate']
export type SourceLanguage = Schemas['SourceLanguage']
export type ProviderChoice = Schemas['ProviderChoice']
export type StreamCaptionTarget = Schemas['StreamCaptionTarget']

/** How a stream-caption delivery state reads on a chip. */
export const ccChip: Record<Schemas['StreamCaptionStatus']['state'], ChipStatus> = {
  disabled: 'idle',
  idle: 'idle',
  ok: 'ok',
  retrying: 'warn',
  error: 'error',
}

export const streamCaptionTargets: StreamCaptionTarget[] = ['youtube_http', 'obs_websocket']

/** Same rule as the server (and api/openapi.yaml `Slug`). */
export const slugPattern = /^[a-z0-9][a-z0-9-]{0,62}$/

export const providers: ProviderChoice[] = ['default', 'gemini', 'local', 'mock']

export interface SessionFormValues {
  name: string
  slug: string
  room: string
  sourceLanguage: SourceLanguage
  targetLanguages: string[]
  provider: ProviderChoice
  recordingEnabled: boolean
  /** '' for none. */
  glossaryId: string
  ccEnabled: boolean
  ccTarget: StreamCaptionTarget
  /** A target language or `source`. */
  ccTrack: string
  ccMaxChars: number
  /** Write-only: '' keeps whatever URL is stored. */
  ccYoutubeUrl: string
}

export const newSessionValues: SessionFormValues = {
  name: '',
  slug: '',
  room: '',
  sourceLanguage: 'auto',
  targetLanguages: ['es', 'en'],
  provider: 'default',
  recordingEnabled: true,
  glossaryId: '',
  ccEnabled: false,
  ccTarget: 'youtube_http',
  ccTrack: 'en',
  ccMaxChars: 32,
  ccYoutubeUrl: '',
}

export function valuesFrom(s: Session): SessionFormValues {
  return {
    name: s.name ?? '',
    slug: s.id,
    room: s.room ?? '',
    sourceLanguage: s.sourceLanguage ?? 'auto',
    targetLanguages: s.targetLanguages ?? [],
    provider: s.provider ?? 'default',
    recordingEnabled: s.recordingEnabled ?? false,
    glossaryId: s.glossaryId ?? '',
    ccEnabled: s.streamCaptions?.enabled ?? false,
    ccTarget: s.streamCaptions?.target ?? 'youtube_http',
    ccTrack: s.streamCaptions?.track ?? 'en',
    ccMaxChars: s.streamCaptions?.maxCharsPerLine ?? 32,
    ccYoutubeUrl: '',
  }
}

/**
 * A URL slug from a session name: "Sala Konex – Día 2" → "sala-konex-dia-2".
 * Accents are dropped, anything else becomes a hyphen.
 */
export function slugify(name: string): string {
  return name
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+/, '')
    .slice(0, 63)
    .replace(/-+$/, '')
}

/** Field → error code (errors.<code> in common.json), checked before sending. */
export function validate(v: SessionFormValues, creating: boolean): Record<string, string> {
  const bad: Record<string, string> = {}
  const name = v.name.trim()
  if (name.length === 0 || name.length > 120) bad.name = 'request.invalid'
  if (creating && !slugPattern.test(v.slug)) bad.slug = 'session.slug_invalid'
  if (v.room.length > 120) bad.room = 'request.invalid'
  if (v.targetLanguages.length === 0) bad.targetLanguages = 'request.invalid'
  return bad
}

/** The PATCH/POST body: every field, so the form is what's saved. */
export function toBody(v: SessionFormValues) {
  return {
    name: v.name.trim(),
    room: v.room.trim(),
    sourceLanguage: v.sourceLanguage,
    targetLanguages: v.targetLanguages,
    provider: v.provider,
    recordingEnabled: v.recordingEnabled,
    glossaryId: v.glossaryId || null,
    streamCaptions: {
      enabled: v.ccEnabled,
      target: v.ccTarget,
      track: v.ccTrack,
      maxCharsPerLine: v.ccMaxChars,
    },
  }
}

/** The capture page link, with the ingest token it needs. */
export function captureUrl(base: string, token: string): string {
  return `${base}?token=${encodeURIComponent(token)}`
}

const loopback = new Set(['localhost', '127.0.0.1', '[::1]'])

/**
 * Where the capture page should be opened. Browsers only allow the
 * microphone on https or localhost (TLS-2), so when this admin runs on
 * localhost (usually the capture computer itself) the link stays on it;
 * otherwise it's the server's LAN link.
 */
export function captureBase(
  session: Session,
  location: Pick<Location, 'hostname' | 'origin'> = window.location,
) {
  return loopback.has(location.hostname)
    ? `${location.origin}/capture/${encodeURIComponent(session.id)}`
    : session.urls.capture
}
