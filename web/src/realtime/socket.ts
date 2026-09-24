// SPDX-License-Identifier: Apache-2.0

/** Where a reconnecting socket is. `closed` is final: it won't reconnect. */
export type ConnectionStatus = 'connecting' | 'open' | 'reconnecting' | 'closed'

export interface BackoffOptions {
  /** Delay before the first retry. */
  initialMs: number
  /** Longest delay between retries. */
  maxMs: number
  /** Growth per failed attempt. */
  factor: number
  /** ± share of the delay randomized, so many phones don't retry in lockstep. */
  jitter: number
}

export const defaultBackoff: BackoffOptions = {
  initialMs: 500,
  maxMs: 10_000,
  factor: 2,
  jitter: 0.2,
}

/** Delay before retry number `attempt` (0-based): exponential, capped, jittered. */
export function backoffDelay(
  attempt: number,
  o: BackoffOptions = defaultBackoff,
  random: () => number = Math.random,
): number {
  const base = Math.min(o.maxMs, o.initialMs * o.factor ** attempt)
  const jittered = base * (1 - o.jitter + 2 * o.jitter * random())
  return Math.round(Math.min(o.maxMs, jittered))
}

/** A WebSocket constructor; tests pass a fake. */
export type WebSocketFactory = new (url: string) => WebSocket

export interface SocketOptions {
  /** Called on every (re)connect, so it can carry fresh query parameters. */
  url: string | (() => string)
  onMessage: (data: string | ArrayBuffer) => void
  onOpen?: (send: (data: string | ArrayBufferLike | ArrayBufferView) => void) => void
  onStatus?: (status: ConnectionStatus) => void
  /** Return false to stop reconnecting after this close (e.g. replaced). */
  shouldReconnect?: (event: CloseEvent) => boolean
  backoff?: Partial<BackoffOptions>
  WebSocket?: WebSocketFactory
}

export interface ReconnectingSocket {
  /** Sends on the open connection; false (and nothing sent) while disconnected. */
  send(data: string | ArrayBufferLike | ArrayBufferView): boolean
  /** Closes for good. */
  close(): void
  readonly status: ConnectionStatus
}

const OPEN = 1

/**
 * A WebSocket that reconnects with exponential backoff until closed, and
 * right away when the browser comes back online (SES-5). Messages are
 * delivered as strings (text frames) or ArrayBuffers (binary frames).
 */
export function openSocket(o: SocketOptions): ReconnectingSocket {
  const backoff = { ...defaultBackoff, ...o.backoff }
  const Impl: WebSocketFactory = o.WebSocket ?? WebSocket
  let ws: WebSocket | null = null
  let attempt = 0
  let timer: ReturnType<typeof setTimeout> | undefined
  let closed = false
  let status: ConnectionStatus = 'connecting'

  const setStatus = (s: ConnectionStatus) => {
    if (s === status && s !== 'connecting') return
    status = s
    o.onStatus?.(s)
  }

  const send = (data: string | ArrayBufferLike | ArrayBufferView): boolean => {
    if (ws?.readyState !== OPEN) return false
    ws.send(data as string)
    return true
  }

  const connect = () => {
    timer = undefined
    const sock = new Impl(typeof o.url === 'function' ? o.url() : o.url)
    ws = sock
    sock.binaryType = 'arraybuffer'
    sock.onopen = () => {
      if (ws !== sock) return
      attempt = 0
      setStatus('open')
      o.onOpen?.(send)
    }
    sock.onmessage = (ev: MessageEvent) => {
      if (ws === sock) o.onMessage(ev.data as string | ArrayBuffer)
    }
    sock.onclose = (ev: CloseEvent) => {
      if (ws !== sock) return
      ws = null
      if (closed || o.shouldReconnect?.(ev) === false) {
        closed = true
        setStatus('closed')
        return
      }
      setStatus('reconnecting')
      timer = setTimeout(connect, backoffDelay(attempt++, backoff))
    }
  }

  const online = () => {
    if (timer !== undefined && !closed) {
      clearTimeout(timer)
      connect()
    }
  }
  globalThis.addEventListener?.('online', online)

  o.onStatus?.('connecting')
  connect()

  return {
    send,
    close() {
      if (closed && status === 'closed') return
      closed = true
      clearTimeout(timer)
      globalThis.removeEventListener?.('online', online)
      const sock = ws
      ws = null
      sock?.close(1000)
      setStatus('closed')
    },
    get status() {
      return status
    },
  }
}

/** The ws:// or wss:// URL of a server path, on the page's own host. */
export function wsUrl(
  path: string,
  query?: URLSearchParams,
  loc: Pick<Location, 'protocol' | 'host'> = location,
): string {
  const proto = loc.protocol === 'https:' ? 'wss:' : 'ws:'
  const q = query?.toString()
  return `${proto}//${loc.host}${path}${q ? `?${q}` : ''}`
}
