// SPDX-License-Identifier: Apache-2.0
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { FakeWebSocket, FakeWebSocketFactory } from '../test/fakeWebSocket'
import { backoffDelay, defaultBackoff, openSocket, wsUrl, type ConnectionStatus } from './socket'

beforeEach(() => {
  FakeWebSocket.reset()
  vi.useFakeTimers()
})
afterEach(() => vi.useRealTimers())

describe('backoffDelay', () => {
  it.each([
    [0, 0.5, 500],
    [1, 0.5, 1000],
    [4, 0.5, 8000],
    [5, 0.5, 10_000],
    [20, 0.5, 10_000],
    [0, 0, 400],
    [0, 1, 600],
    [10, 1, 10_000],
  ])('attempt %i with random %f → %i ms', (attempt, random, want) => {
    expect(backoffDelay(attempt, defaultBackoff, () => random)).toBe(want)
  })
})

describe('openSocket', () => {
  it('reconnects with backoff and resets it once connected', () => {
    const statuses: ConnectionStatus[] = []
    const messages: unknown[] = []
    let n = 0
    const sock = openSocket({
      url: () => `ws://x/${++n}`,
      onMessage: (d) => messages.push(d),
      onStatus: (s) => statuses.push(s),
      backoff: { jitter: 0 },
      WebSocket: FakeWebSocketFactory,
    })
    const first = FakeWebSocket.last
    expect(first.url).toBe('ws://x/1')
    expect(first.binaryType).toBe('arraybuffer')
    first.accept()
    first.receive('hello')
    expect(messages).toEqual(['hello'])

    first.drop()
    expect(sock.status).toBe('reconnecting')
    expect(sock.send('x')).toBe(false)
    vi.advanceTimersByTime(499)
    expect(FakeWebSocket.instances).toHaveLength(1)
    vi.advanceTimersByTime(1)
    expect(FakeWebSocket.last.url).toBe('ws://x/2') // the URL is rebuilt
    FakeWebSocket.last.drop()
    vi.advanceTimersByTime(999)
    expect(FakeWebSocket.instances).toHaveLength(2)
    vi.advanceTimersByTime(1)
    FakeWebSocket.last.accept()
    expect(sock.send('ping')).toBe(true)
    expect(FakeWebSocket.last.sent).toEqual(['ping'])

    FakeWebSocket.last.drop()
    vi.advanceTimersByTime(500) // back to the first delay
    expect(FakeWebSocket.instances).toHaveLength(4)
    expect(statuses).toEqual(['connecting', 'open', 'reconnecting', 'open', 'reconnecting'])

    // A late message from a replaced socket is ignored.
    first.receive('stale')
    expect(messages).toEqual(['hello'])

    sock.close()
    expect(FakeWebSocket.last.closedWith).toBe(1000)
    expect(sock.status).toBe('closed')
    vi.advanceTimersByTime(60_000)
    expect(FakeWebSocket.instances).toHaveLength(4)
  })

  it('stops when shouldReconnect says so', () => {
    const sock = openSocket({
      url: 'ws://x',
      onMessage: () => {},
      shouldReconnect: (ev) => ev.code !== 4000,
      WebSocket: FakeWebSocketFactory,
    })
    FakeWebSocket.last.accept()
    FakeWebSocket.last.drop(4000)
    expect(sock.status).toBe('closed')
    vi.advanceTimersByTime(60_000)
    expect(FakeWebSocket.instances).toHaveLength(1)
  })

  it('retries at once when the browser comes back online', () => {
    openSocket({ url: 'ws://x', onMessage: () => {}, WebSocket: FakeWebSocketFactory })
    FakeWebSocket.last.drop()
    window.dispatchEvent(new Event('online'))
    expect(FakeWebSocket.instances).toHaveLength(2)
  })
})

describe('wsUrl', () => {
  it.each([
    ['http:', 'localhost:5173', undefined, 'ws://localhost:5173/ws/admin'],
    ['https:', 'subs.example.com', undefined, 'wss://subs.example.com/ws/admin'],
    [
      'http:',
      '192.168.1.20:8080',
      new URLSearchParams([
        ['lang', 'es'],
        ['lang', 'source'],
      ]),
      'ws://192.168.1.20:8080/ws/admin?lang=es&lang=source',
    ],
  ])('%s//%s', (protocol, host, query, want) => {
    expect(wsUrl('/ws/admin', query, { protocol, host })).toBe(want)
  })
})
