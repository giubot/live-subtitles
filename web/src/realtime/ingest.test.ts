// SPDX-License-Identifier: Apache-2.0
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ApiErrorBody, IngestServerMessage } from '../api/types'
import { FakeWebSocket, FakeWebSocketFactory } from '../test/fakeWebSocket'
import { createIngestSocket, replacedCloseCode, type IngestStatus } from './ingest'

beforeEach(() => {
  FakeWebSocket.reset()
  vi.useFakeTimers()
})
afterEach(() => vi.useRealTimers())

const samples = (n: number, v = 1) => new Int16Array(n).fill(v)

function setup(bufferMs?: number) {
  const statuses: IngestStatus[] = []
  const levels: IngestServerMessage[] = []
  const errors: ApiErrorBody[] = []
  const sock = createIngestSocket({
    sessionId: 'main',
    token: 'tok/+=',
    deviceLabel: 'USB mixer',
    bufferMs,
    onStatus: (s) => statuses.push(s),
    onLevel: (l) => levels.push(l),
    onError: (e) => errors.push(e),
    backoff: { jitter: 0 },
    WebSocket: FakeWebSocketFactory,
  })
  return { sock, statuses, levels, errors }
}

describe('createIngestSocket', () => {
  it('says hello, waits for ready, then streams PCM', () => {
    const { sock, statuses, levels } = setup()
    const ws = FakeWebSocket.last
    expect(ws.url).toBe(`ws://${location.host}/ws/ingest/main?token=tok%2F%2B%3D`)
    ws.accept()
    expect(ws.sentJSON()).toEqual([
      expect.objectContaining({
        type: 'hello',
        format: 's16le',
        sampleRate: 16000,
        channels: 1,
        source: 'browser',
        deviceLabel: 'USB mixer',
      }),
    ])
    sock.send(samples(320)) // before ready: buffered
    expect(ws.sent).toHaveLength(1)
    ws.receive({ type: 'ready' })
    expect(sock.status).toBe('ready')
    expect(ws.sent).toHaveLength(2)
    sock.send(samples(320, 7))
    expect(ws.sent[2]).toEqual(samples(320, 7))
    ws.receive({ type: 'level', levelDbfs: -20, peakDbfs: -6, silent: false, clipping: false })
    expect(levels).toEqual([expect.objectContaining({ levelDbfs: -20 })])
    expect(statuses).toEqual(['ready'])
  })

  it('buffers while reconnecting, dropping the oldest audio beyond bufferMs', () => {
    const { sock, statuses } = setup(100) // 1600 samples
    FakeWebSocket.last.accept()
    FakeWebSocket.last.receive({ type: 'ready' })
    FakeWebSocket.last.drop()
    expect(sock.status).toBe('reconnecting')
    sock.send(samples(800, 1))
    sock.send(samples(800, 2))
    sock.send(samples(800, 3)) // pushes out the first chunk
    vi.advanceTimersByTime(500)
    const ws = FakeWebSocket.last
    ws.accept()
    ws.receive({ type: 'ready' })
    expect(ws.sent.slice(1)).toEqual([samples(800, 2), samples(800, 3)])
    expect(statuses).toEqual(['ready', 'reconnecting', 'ready'])
  })

  it('stops for good when another capture page takes over', () => {
    const { sock, errors } = setup()
    const ws = FakeWebSocket.last
    ws.accept()
    ws.receive({ type: 'ready' })
    ws.receive({ type: 'error', error: { code: 'ingest.replaced', message: 'replaced' } })
    ws.drop(replacedCloseCode)
    expect(sock.status).toBe('replaced')
    expect(errors.map((e) => e.code)).toEqual(['ingest.replaced'])
    vi.advanceTimersByTime(60_000)
    expect(FakeWebSocket.instances).toHaveLength(1)
    sock.send(samples(320))
    expect(ws.sent).toHaveLength(1) // just the hello
  })

  it('closes', () => {
    const { sock } = setup()
    FakeWebSocket.last.accept()
    sock.close()
    expect(sock.status).toBe('closed')
    expect(FakeWebSocket.last.closedWith).toBe(1000)
  })
})
