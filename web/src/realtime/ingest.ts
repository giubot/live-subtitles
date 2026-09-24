// SPDX-License-Identifier: Apache-2.0
import type {
  ApiErrorBody,
  AudioSourceKind,
  IngestHello,
  IngestServerMessage,
  SessionState,
} from '../api/types'
import { parse } from './captions'
import { openSocket, wsUrl, type BackoffOptions, type WebSocketFactory } from './socket'

/** Audio format /ws/ingest takes: s16le, 16 kHz, mono. */
export const ingestSampleRate = 16000

/** Close code of a connection replaced by a newer one (internal/audio/ingest). */
export const replacedCloseCode = 4000

/**
 * - `connecting`: opening the socket or waiting for `ready`.
 * - `ready`: audio flows.
 * - `reconnecting`: the connection dropped; audio is buffered meanwhile.
 * - `replaced`: another capture page took over; this one stops for good.
 * - `closed`: `close()` was called.
 */
export type IngestStatus = 'connecting' | 'ready' | 'reconnecting' | 'replaced' | 'closed'

export interface IngestSocketOptions {
  sessionId: string
  /** The session's ingest token (from the capture URL). */
  token: string
  source?: AudioSourceKind
  deviceLabel?: string
  onStatus?: (status: IngestStatus) => void
  /** Level, silence and clipping, a few times a second (AUD-6). */
  onLevel?: (level: IngestServerMessage) => void
  onState?: (state: SessionState) => void
  onError?: (error: ApiErrorBody) => void
  /**
   * Audio kept while disconnected and sent on reconnect, in ms (default
   * 5000). Older audio is dropped first.
   */
  bufferMs?: number
  backoff?: Partial<BackoffOptions>
  WebSocket?: WebSocketFactory
}

export interface IngestSocket {
  /** Queues 16 kHz mono samples; they're sent at once when ready, else buffered. */
  send(samples: Int16Array): void
  close(): void
  readonly status: IngestStatus
}

/**
 * The capture page's audio connection (/ws/ingest, AUD-1): sends the
 * IngestHello on every connect, waits for `ready`, then streams binary
 * PCM. It reconnects with backoff and flushes what it buffered meanwhile
 * (SES-5), except after being replaced by another capture page.
 *
 * Samples go out in the platform's byte order, which is little-endian on
 * every device a browser runs on.
 */
export function createIngestSocket(o: IngestSocketOptions): IngestSocket {
  const maxBuffered = Math.round(((o.bufferMs ?? 5000) / 1000) * ingestSampleRate)
  let buffer: Int16Array[] = []
  let buffered = 0
  let status: IngestStatus = 'connecting'
  let replaced = false
  const setStatus = (s: IngestStatus) => {
    if (s === status) return
    status = s
    o.onStatus?.(s)
  }

  const query = new URLSearchParams({ token: o.token })
  const socket = openSocket({
    url: () => wsUrl(`/ws/ingest/${encodeURIComponent(o.sessionId)}`, query),
    onOpen: (send) => {
      const hello: IngestHello = {
        type: 'hello',
        format: 's16le',
        sampleRate: ingestSampleRate,
        channels: 1,
        source: o.source ?? 'browser',
        deviceLabel: o.deviceLabel,
        clientTime: Date.now(),
      }
      send(JSON.stringify(hello))
    },
    onMessage: (data) => {
      const msg = parse<IngestServerMessage>(data)
      if (!msg) return
      switch (msg.type) {
        case 'ready':
          setStatus('ready')
          flush()
          break
        case 'level':
          o.onLevel?.(msg)
          break
        case 'state':
          if (msg.state) o.onState?.(msg.state)
          break
        case 'error':
          if (msg.error?.code === 'ingest.replaced') replaced = true
          if (msg.error) o.onError?.(msg.error)
          break
      }
    },
    onStatus: (s) => {
      if (s === 'reconnecting') setStatus('reconnecting')
      else if (s === 'closed') setStatus(replaced ? 'replaced' : 'closed')
      else if (s === 'connecting') setStatus('connecting')
      // 'open' isn't ready yet: the server answers the hello first.
    },
    shouldReconnect: (ev) => {
      if (ev.code === replacedCloseCode) replaced = true
      return !replaced
    },
    backoff: o.backoff,
    WebSocket: o.WebSocket,
  })

  function flush() {
    const chunks = buffer
    buffer = []
    buffered = 0
    for (const c of chunks) {
      if (!socket.send(c)) {
        keep(c)
      }
    }
  }

  function keep(samples: Int16Array) {
    buffer.push(samples)
    buffered += samples.length
    while (buffered > maxBuffered && buffer.length > 0) {
      buffered -= buffer.shift()!.length
    }
  }

  return {
    send(samples) {
      if (status === 'replaced' || status === 'closed' || samples.length === 0) return
      if (status === 'ready' && buffer.length === 0 && socket.send(samples)) return
      keep(samples.slice())
      if (status === 'ready') flush()
    },
    close() {
      socket.close()
      buffer = []
      buffered = 0
    },
    get status() {
      return status
    },
  }
}
