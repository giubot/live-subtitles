// SPDX-License-Identifier: Apache-2.0
import { create } from 'zustand'
import { api } from '../../api/client'
import type { Schemas } from '../../api/types'

/**
 * Where a session's live audio comes from when Start is pressed: the
 * capture page (`browser`) or an encoder pushing SRT (`srt`, AUD-5). The
 * API takes it per start (SessionStartRequest.source) and doesn't store
 * it on the session, so this browser remembers the choice per session.
 */
export type LiveSource = Extract<Schemas['AudioSourceKind'], 'browser' | 'srt'>

export const liveSources: LiveSource[] = ['browser', 'srt']

const storageKey = 'ls.admin.sessionSource'

function load(): Record<string, LiveSource> {
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(storageKey) ?? '{}')
    if (raw == null || typeof raw !== 'object') return {}
    return Object.fromEntries(
      Object.entries(raw).filter((e): e is [string, LiveSource] => e[1] === 'srt'),
    )
  } catch {
    return {}
  }
}

function save(sources: Record<string, LiveSource>) {
  try {
    localStorage.setItem(storageKey, JSON.stringify(sources))
  } catch {
    // Not stored; the choice lasts for this visit.
  }
}

interface SessionSources {
  /** Only non-default choices are kept; a missing id means `browser`. */
  sources: Record<string, LiveSource>
  setSource(sessionId: string, source: LiveSource): void
}

export const useSessionSources = create<SessionSources>()((set) => ({
  sources: load(),
  setSource: (id, source) =>
    set((s) => {
      const sources = { ...s.sources }
      if (source === 'browser') delete sources[id]
      else sources[id] = source
      save(sources)
      return { sources }
    }),
}))

/** The session's chosen live source (`browser` unless SRT was picked). */
export function useSessionSource(sessionId: string | undefined): LiveSource {
  return useSessionSources((s) => (sessionId ? s.sources[sessionId] : undefined) ?? 'browser')
}

/** Why SRT can't be offered, or undefined when it can. */
export type SrtBlocker = 'noLibsrt' | 'disabled' | 'unknown'

/**
 * Whether SRT ingest can be used: a saved session has SessionUrls.srtIngest
 * only when ffmpeg has libsrt (SystemInfo.features.srtIngest) and SRT is
 * on in the settings; a new one is judged from those two directly.
 */
export function useSrtAvailability(session?: { urls: Schemas['SessionUrls'] }): {
  available: boolean
  blocker?: SrtBlocker
  loading: boolean
} {
  const info = api.useQuery('get', '/api/system/info')
  const settings = api.useQuery('get', '/api/settings')
  const loading = info.isLoading || settings.isLoading
  const blocker: SrtBlocker | undefined =
    info.data?.features.srtIngest === false
      ? 'noLibsrt'
      : settings.data?.srt?.enabled === false
        ? 'disabled'
        : undefined
  if (session) {
    if (session.urls.srtIngest) return { available: true, loading: false }
    return { available: false, blocker: blocker ?? 'unknown', loading }
  }
  if (blocker) return { available: false, blocker, loading }
  if (info.data?.features.srtIngest) return { available: true, loading }
  return { available: false, blocker: 'unknown', loading }
}
