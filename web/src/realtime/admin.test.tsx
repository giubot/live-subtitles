// SPDX-License-Identifier: Apache-2.0
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { AdminEvent, SessionStatus } from '../api/types'
import { FakeWebSocket, FakeWebSocketFactory } from '../test/fakeWebSocket'
import {
  applyAdminEvent,
  emptyAdminEvents,
  maxAdminLogs,
  useAdminEvents,
  useAdminEventsStore,
} from './admin'

const at = '2026-09-25T13:30:00Z'
const status = (sessionId: string, state: SessionStatus['state']): AdminEvent => ({
  type: 'sessionStatus',
  at,
  status: { sessionId, state, viewers: 0 },
})

describe('applyAdminEvent', () => {
  it('keeps the latest status per session and forgets deleted ones', () => {
    let s = applyAdminEvent(emptyAdminEvents, status('main', 'idle'))
    s = applyAdminEvent(s, status('side', 'live'))
    s = applyAdminEvent(s, status('main', 'live'))
    expect(Object.fromEntries(Object.entries(s.statuses).map(([k, v]) => [k, v.state]))).toEqual({
      main: 'live',
      side: 'live',
    })
    s = applyAdminEvent(s, { type: 'sessionDeleted', at, sessionId: 'side' })
    expect(Object.keys(s.statuses)).toEqual(['main'])
    expect(applyAdminEvent(s, { type: 'sessionDeleted', at, sessionId: 'nope' })).toBe(s)
  })

  it('keeps the last log events', () => {
    let s = emptyAdminEvents
    for (let i = 0; i < maxAdminLogs + 3; i++) {
      s = applyAdminEvent(s, { type: 'log', at, log: { level: 'warn', code: `c${i}` } })
    }
    expect(s.logs).toHaveLength(maxAdminLogs)
    expect(s.logs[0]?.log?.code).toBe('c3')
  })
})

describe('useAdminEvents', () => {
  beforeEach(() => {
    FakeWebSocket.reset()
    useAdminEventsStore.setState({ ...emptyAdminEvents })
  })

  it('follows /ws/admin and refreshes session queries on changes', () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    )
    const { result, unmount } = renderHook(
      () => useAdminEvents({ WebSocket: FakeWebSocketFactory }),
      { wrapper },
    )
    const ws = FakeWebSocket.last
    expect(ws.url).toBe(`ws://${location.host}/ws/admin`)

    act(() => {
      useAdminEventsStore.getState().apply(status('stale', 'live'))
      ws.accept() // a new connection starts from its snapshot
      ws.receive(status('main', 'live'))
    })
    expect(Object.keys(result.current.statuses)).toEqual(['main'])
    expect(result.current.connection).toBe('open')
    expect(invalidate).not.toHaveBeenCalled()

    act(() => ws.receive({ type: 'sessionCreated', at, sessionId: 'new' }))
    expect(invalidate).toHaveBeenCalledTimes(1)
    const { predicate } = invalidate.mock.calls[0]![0] as unknown as {
      predicate: (q: { queryKey: unknown[] }) => boolean
    }
    expect(predicate({ queryKey: ['get', '/api/sessions'] })).toBe(true)
    expect(predicate({ queryKey: ['get', '/api/sessions/{sessionId}', {}] })).toBe(true)
    expect(predicate({ queryKey: ['get', '/api/settings'] })).toBe(false)

    unmount()
    expect(ws.closedWith).toBe(1000)
  })

  it('stays closed when disabled', () => {
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>
    )
    renderHook(() => useAdminEvents({ enabled: false, WebSocket: FakeWebSocketFactory }), {
      wrapper,
    })
    expect(FakeWebSocket.instances).toHaveLength(0)
  })
})
