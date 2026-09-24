// SPDX-License-Identifier: Apache-2.0
import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Caption } from '../api/types'
import { FakeWebSocket, FakeWebSocketFactory } from '../test/fakeWebSocket'
import {
  addCaption,
  applyCaptionMessage,
  emptySession,
  emptyTrack,
  maxFinalsPerTrack,
  useCaptions,
  useCaptionsStore,
  type SessionCaptions,
} from './captions'

function cap(segmentId: string, text: string, final: boolean, start = 0, lang = 'es'): Caption {
  return {
    sessionId: 'main',
    lang,
    segmentId,
    final,
    text,
    start,
    end: start + 1,
    sourceLang: 'en',
    edited: false,
    hidden: false,
  }
}

describe('addCaption', () => {
  it('replaces interims and finalizes them', () => {
    let t = addCaption(emptyTrack, cap('s1', 'Hola', false))
    t = addCaption(t, cap('s1', 'Hola a', false))
    expect(t.interim?.text).toBe('Hola a')
    expect(t.finals).toEqual([])
    t = addCaption(t, cap('s1', 'Hola a todos.', true))
    expect(t.interim).toBeNull()
    expect(t.finals.map((c) => c.text)).toEqual(['Hola a todos.'])
    // A late interim of a final segment is dropped.
    expect(addCaption(t, cap('s1', 'Hola', false))).toBe(t)
  })

  it('keeps another segment’s interim when a final arrives', () => {
    let t = addCaption(emptyTrack, cap('s2', 'Hoy', false, 2))
    t = addCaption(t, cap('s1', 'Hola.', true, 0))
    expect(t.interim?.segmentId).toBe('s2')
  })

  it('orders finals by start and replaces corrections in place', () => {
    let t = emptyTrack
    for (const [id, start] of [
      ['a', 0],
      ['c', 4],
      ['b', 2],
    ] as const)
      t = addCaption(t, cap(id, id, true, start))
    expect(t.finals.map((c) => c.segmentId)).toEqual(['a', 'b', 'c'])
    t = addCaption(t, { ...cap('b', 'B (edited)', true, 2), edited: true })
    expect(t.finals.map((c) => c.text)).toEqual(['a', 'B (edited)', 'c'])
    t = addCaption(t, { ...cap('b', 'B', true, 2), hidden: true })
    expect(t.finals.map((c) => c.segmentId)).toEqual(['a', 'c'])
  })

  it('keeps at most maxFinalsPerTrack finals', () => {
    let t = emptyTrack
    for (let i = 0; i < maxFinalsPerTrack + 5; i++) t = addCaption(t, cap(`s${i}`, 'x', true, i))
    expect(t.finals).toHaveLength(maxFinalsPerTrack)
    expect(t.finals[0]?.segmentId).toBe('s5')
  })
})

describe('applyCaptionMessage', () => {
  it('merges history on reconnect and drops stale interims', () => {
    let s: SessionCaptions = emptySession
    s = applyCaptionMessage(s, { type: 'caption', caption: cap('s1', 'Uno.', true, 0) })
    s = applyCaptionMessage(s, { type: 'caption', caption: cap('s2', 'Dos', false, 2) })
    s = applyCaptionMessage(s, { type: 'error', error: { code: 'x', message: 'x' } })
    // After a reconnect: s2 was finalized meanwhile, s3 is in progress.
    s = applyCaptionMessage(s, {
      type: 'history',
      captions: [
        cap('s2', 'Dos.', true, 2),
        cap('s3', 'Tres', false, 4),
        cap('s2', 'Two.', true, 2, 'en'),
      ],
    })
    expect(s.tracks.es?.finals.map((c) => c.text)).toEqual(['Uno.', 'Dos.'])
    expect(s.tracks.es?.interim?.text).toBe('Tres')
    expect(s.tracks.en?.finals.map((c) => c.text)).toEqual(['Two.'])
    expect(s.error).toBeUndefined()

    s = applyCaptionMessage(s, { type: 'history', captions: [] })
    expect(s.tracks.es?.interim).toBeNull()
    expect(s.tracks.es?.finals).toHaveLength(2)
  })

  it('tracks state, detected language and viewers', () => {
    let s = applyCaptionMessage(emptySession, {
      type: 'state',
      state: 'live',
      detectedLanguage: 'en',
    })
    s = applyCaptionMessage(s, { type: 'viewers', viewers: 12 })
    s = applyCaptionMessage(s, { type: 'state', state: 'paused' })
    expect(s).toMatchObject({ state: 'paused', detectedLanguage: 'en', viewers: 12 })
  })
})

describe('useCaptions', () => {
  beforeEach(() => {
    FakeWebSocket.reset()
    useCaptionsStore.setState({ sessions: {} })
    vi.useFakeTimers()
  })
  afterEach(() => vi.useRealTimers())

  it('subscribes to the tracks and follows the stream across reconnects', () => {
    const { result, unmount, rerender } = renderHook(
      ({ langs }: { langs: string[] }) =>
        useCaptions('main stage', langs, {
          history: 20,
          WebSocket: FakeWebSocketFactory,
          backoff: { jitter: 0 },
        }),
      { initialProps: { langs: ['es', 'source'] } },
    )
    const ws = FakeWebSocket.last
    expect(ws.url).toBe(
      `ws://${location.host}/ws/captions/main%20stage?lang=es&lang=source&history=20`,
    )
    expect(result.current.connection).toBe('connecting')

    act(() => {
      ws.accept()
      ws.receive({ type: 'history', captions: [cap('s1', 'Hola.', true)] })
      ws.receive({ type: 'caption', caption: cap('s2', 'Hoy', false, 2) })
      ws.receive('not json')
    })
    expect(result.current.connection).toBe('open')
    expect(result.current.tracks.es?.finals.map((c) => c.text)).toEqual(['Hola.'])
    expect(result.current.tracks.es?.interim?.text).toBe('Hoy')

    act(() => ws.drop())
    expect(result.current.connection).toBe('reconnecting')
    expect(result.current.tracks.es?.finals).toHaveLength(1) // kept on screen
    act(() => {
      vi.advanceTimersByTime(500)
      FakeWebSocket.last.accept()
      FakeWebSocket.last.receive({
        type: 'history',
        captions: [cap('s1', 'Hola.', true), cap('s2', 'Hoy vamos.', true, 2)],
      })
    })
    expect(result.current.tracks.es?.finals.map((c) => c.text)).toEqual(['Hola.', 'Hoy vamos.'])
    expect(result.current.tracks.es?.interim).toBeNull()

    // Same tracks in a new array: no reconnect. Different tracks: reconnect.
    rerender({ langs: ['es', 'source'] })
    expect(FakeWebSocket.instances).toHaveLength(2)
    rerender({ langs: ['en'] })
    expect(FakeWebSocket.instances).toHaveLength(3)
    expect(FakeWebSocket.instances[1]?.closedWith).toBe(1000)

    unmount()
    expect(FakeWebSocket.last.closedWith).toBe(1000)
    expect(useCaptionsStore.getState().sessions['main stage']?.connection).toBe('closed')
  })

  it('does nothing without a session', () => {
    const { result } = renderHook(() =>
      useCaptions(undefined, ['es'], { WebSocket: FakeWebSocketFactory }),
    )
    expect(FakeWebSocket.instances).toHaveLength(0)
    expect(result.current).toBe(emptySession)
  })
})
