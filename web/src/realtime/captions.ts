// SPDX-License-Identifier: Apache-2.0
import { useEffect } from 'react'
import { create } from 'zustand'
import type { ApiErrorBody, Caption, CaptionsServerMessage, SessionState } from '../api/types'
import {
  openSocket,
  wsUrl,
  type BackoffOptions,
  type ConnectionStatus,
  type WebSocketFactory,
} from './socket'

/** The captions of one track: finals in start order, plus the sentence in progress. */
export interface TrackCaptions {
  finals: Caption[]
  interim: Caption | null
}

/** What a viewer knows about one session. */
export interface SessionCaptions {
  /** By track: a target language code or `source`. */
  tracks: Record<string, TrackCaptions>
  state?: SessionState
  detectedLanguage?: string
  viewers?: number
  error?: ApiErrorBody
  connection: ConnectionStatus
}

/** Finals kept per track; older ones are dropped (the export has them all). */
export const maxFinalsPerTrack = 1000

export const emptyTrack: TrackCaptions = Object.freeze({
  finals: [],
  interim: null,
}) as TrackCaptions
export const emptySession: SessionCaptions = Object.freeze({
  tracks: {},
  connection: 'connecting',
}) as SessionCaptions

/**
 * Adds one caption to a track, with the server's rules (internal/bus):
 * a final replaces the entry with its segmentId, or is inserted in start
 * order, and clears the interim of its segment; an interim replaces the
 * current interim unless its segment is already final (a late interim).
 * A hidden final (ADM-4) is removed.
 */
export function addCaption(track: TrackCaptions, c: Caption): TrackCaptions {
  const i = track.finals.findIndex((f) => f.segmentId === c.segmentId)
  if (!c.final) {
    return i >= 0 ? track : { ...track, interim: c }
  }
  // Plain slices rather than toSpliced/with, which older phones lack.
  let finals: Caption[]
  if (c.hidden) {
    finals = i >= 0 ? [...track.finals.slice(0, i), ...track.finals.slice(i + 1)] : track.finals
  } else if (i >= 0) {
    finals = track.finals.slice()
    finals[i] = c
  } else {
    // Usually the newest: append. Otherwise insert after every earlier start.
    let at = track.finals.length
    while (at > 0 && track.finals[at - 1]!.start > c.start) at--
    finals = [...track.finals.slice(0, at), c, ...track.finals.slice(at)]
    if (finals.length > maxFinalsPerTrack) finals = finals.slice(-maxFinalsPerTrack)
  }
  const interim = track.interim?.segmentId === c.segmentId ? null : track.interim
  return { finals, interim }
}

/**
 * Applies one /ws/captions message. `history` (sent on every connect)
 * merges into what's already on screen, so a reconnect doesn't blank the
 * transcript; interims are reset to the history's, since any other is stale.
 */
export function applyCaptionMessage(
  s: SessionCaptions,
  msg: CaptionsServerMessage,
): SessionCaptions {
  switch (msg.type) {
    case 'history': {
      const tracks: Record<string, TrackCaptions> = {}
      for (const [lang, t] of Object.entries(s.tracks)) tracks[lang] = { ...t, interim: null }
      for (const c of msg.captions ?? [])
        tracks[c.lang] = addCaption(tracks[c.lang] ?? emptyTrack, c)
      return { ...s, tracks, error: undefined }
    }
    case 'caption': {
      const c = msg.caption
      if (!c) return s
      return {
        ...s,
        tracks: { ...s.tracks, [c.lang]: addCaption(s.tracks[c.lang] ?? emptyTrack, c) },
      }
    }
    case 'state':
      return {
        ...s,
        state: msg.state ?? s.state,
        detectedLanguage: msg.detectedLanguage ?? s.detectedLanguage,
      }
    case 'viewers':
      return { ...s, viewers: msg.viewers ?? s.viewers }
    case 'error':
      return { ...s, error: msg.error }
  }
  return s
}

interface CaptionsStore {
  sessions: Record<string, SessionCaptions>
  apply(sessionId: string, msg: CaptionsServerMessage): void
  setConnection(sessionId: string, connection: ConnectionStatus): void
}

/** Captions of every session the page follows, fed by `useCaptions`. */
export const useCaptionsStore = create<CaptionsStore>()((set) => ({
  sessions: {},
  apply: (id, msg) =>
    set((st) => ({
      sessions: { ...st.sessions, [id]: applyCaptionMessage(st.sessions[id] ?? emptySession, msg) },
    })),
  setConnection: (id, connection) =>
    set((st) => ({
      sessions: { ...st.sessions, [id]: { ...(st.sessions[id] ?? emptySession), connection } },
    })),
}))

export interface UseCaptionsOptions {
  /** Recent finals per track to get on connect (0–500, server default 50). */
  history?: number
  backoff?: Partial<BackoffOptions>
  WebSocket?: WebSocketFactory
}

/**
 * Follows a session's caption tracks over /ws/captions (OUT-1): reconnects
 * with backoff, merges the history, replaces interims, and keeps everything
 * in `useCaptionsStore`. Use one per session per page; `langs` are tracks
 * (target language codes or `source`).
 */
export function useCaptions(
  sessionId: string | undefined,
  langs: readonly string[],
  options: UseCaptionsOptions = {},
): SessionCaptions {
  const key = langs.join(',')
  const { history, backoff, WebSocket } = options
  useEffect(() => {
    if (!sessionId || !key) return
    const { apply, setConnection } = useCaptionsStore.getState()
    const query = new URLSearchParams()
    for (const lang of key.split(',')) query.append('lang', lang)
    if (history !== undefined) query.set('history', String(history))
    const socket = openSocket({
      url: () => wsUrl(`/ws/captions/${encodeURIComponent(sessionId)}`, query),
      onMessage: (data) => {
        const msg = parse<CaptionsServerMessage>(data)
        if (msg) apply(sessionId, msg)
      },
      onStatus: (status) => setConnection(sessionId, status),
      backoff,
      WebSocket,
    })
    return () => socket.close()
    // backoff and WebSocket are fixed for a page; only the stream's identity reconnects.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId, key, history])
  return useCaptionsStore((st) => (sessionId && st.sessions[sessionId]) || emptySession)
}

/** Parses a JSON text frame; binary frames and bad JSON are ignored. */
export function parse<T>(data: string | ArrayBuffer): T | undefined {
  if (typeof data !== 'string') return undefined
  try {
    return JSON.parse(data) as T
  } catch {
    return undefined
  }
}
