// SPDX-License-Identifier: Apache-2.0
import { useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { create } from 'zustand'
import type { AdminEvent, SessionStatus } from '../api/types'
import { parse } from './captions'
import {
  openSocket,
  wsUrl,
  type BackoffOptions,
  type ConnectionStatus,
  type WebSocketFactory,
} from './socket'

/** Log events kept for the dashboard. */
export const maxAdminLogs = 50

export interface AdminEventsState {
  /** Live status by session id. */
  statuses: Record<string, SessionStatus>
  /** Recent `log` events, newest last. */
  logs: AdminEvent[]
  connection: ConnectionStatus
}

export const emptyAdminEvents: AdminEventsState = Object.freeze({
  statuses: {},
  logs: [],
  connection: 'connecting',
}) as AdminEventsState

/** Applies one /ws/admin event to the dashboard state. */
export function applyAdminEvent(s: AdminEventsState, ev: AdminEvent): AdminEventsState {
  switch (ev.type) {
    case 'sessionStatus':
      return ev.status ? { ...s, statuses: { ...s.statuses, [ev.status.sessionId]: ev.status } } : s
    case 'sessionDeleted': {
      if (!ev.sessionId || !(ev.sessionId in s.statuses)) return s
      const statuses = { ...s.statuses }
      delete statuses[ev.sessionId]
      return { ...s, statuses }
    }
    case 'streamCaptionStatus': {
      // Stream-caption updates may come on their own, between full statuses.
      const current = ev.sessionId ? s.statuses[ev.sessionId] : undefined
      if (!current || !ev.status?.streamCaptions) return s
      return {
        ...s,
        statuses: {
          ...s.statuses,
          [current.sessionId]: { ...current, streamCaptions: ev.status.streamCaptions },
        },
      }
    }
    case 'log':
      return { ...s, logs: [...s.logs, ev].slice(-maxAdminLogs) }
  }
  return s
}

interface AdminEventsStore extends AdminEventsState {
  apply(ev: AdminEvent): void
  setConnection(connection: ConnectionStatus): void
}

export const useAdminEventsStore = create<AdminEventsStore>()((set) => ({
  ...emptyAdminEvents,
  apply: (ev) => set((st) => applyAdminEvent(st, ev)),
  setConnection: (connection) =>
    // Each connection starts with a full snapshot, so forget statuses that
    // may have gone stale while disconnected.
    set((st) => (connection === 'open' ? { connection, statuses: {} } : { ...st, connection })),
}))

/** Session list and detail queries, refreshed when sessions change. */
const isSessionQuery = (key: readonly unknown[]) =>
  typeof key[1] === 'string' &&
  (key[1].startsWith('/api/sessions') || key[1].startsWith('/api/public/sessions'))

export interface UseAdminEventsOptions {
  enabled?: boolean
  backoff?: Partial<BackoffOptions>
  WebSocket?: WebSocketFactory
}

/**
 * Follows /ws/admin (ADM-1): session statuses (state, levels, latency,
 * viewers…), and log events. Session created/updated/deleted events
 * refresh the session queries. Needs an admin login; mount it once in the
 * admin layout.
 */
export function useAdminEvents(options: UseAdminEventsOptions = {}): AdminEventsState {
  const { enabled = true, backoff, WebSocket } = options
  const queryClient = useQueryClient()
  useEffect(() => {
    if (!enabled) return
    const { apply, setConnection } = useAdminEventsStore.getState()
    const socket = openSocket({
      url: () => wsUrl('/ws/admin'),
      onMessage: (data) => {
        const ev = parse<AdminEvent>(data)
        if (!ev) return
        apply(ev)
        if (
          ev.type === 'sessionCreated' ||
          ev.type === 'sessionUpdated' ||
          ev.type === 'sessionDeleted'
        ) {
          void queryClient.invalidateQueries({ predicate: (q) => isSessionQuery(q.queryKey) })
        }
      },
      onStatus: setConnection,
      backoff,
      WebSocket,
    })
    return () => socket.close()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, queryClient])
  return useAdminEventsStore()
}
