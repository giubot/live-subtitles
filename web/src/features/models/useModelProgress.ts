// SPDX-License-Identifier: Apache-2.0
import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import type { AdminEvent, Schemas } from '../../api/types'
import { parse } from '../../realtime/captions'
import { openSocket, wsUrl, type ConnectionStatus } from '../../realtime/socket'

type LocalModel = Schemas['LocalModel']

const modelsKey = ['get', '/api/models'] as const

export const busy = (m: LocalModel) => m.status === 'downloading' || m.status === 'verifying'

/**
 * Follows `modelProgress` events on /ws/admin (AI-13) and writes each model
 * into the cached `GET /api/models` list, so progress moves without
 * polling. When a download ends (ready, failed or stopped) the list is
 * refetched. Returns the socket's status: the page polls while it's down.
 */
export function useModelProgress(): ConnectionStatus {
  const queryClient = useQueryClient()
  const [status, setStatus] = useState<ConnectionStatus>('connecting')
  useEffect(() => {
    const socket = openSocket({
      url: () => wsUrl('/ws/admin'),
      onMessage: (data) => {
        const ev = parse<AdminEvent>(data)
        const model = ev?.type === 'modelProgress' ? ev.model : undefined
        if (!model) return
        let finished = false
        queryClient.setQueriesData<LocalModel[]>({ queryKey: modelsKey }, (list) => {
          if (!list) return list
          const prev = list.find((m) => m.id === model.id)
          if (prev && busy(prev) && !busy(model)) finished = true
          return prev ? list.map((m) => (m.id === model.id ? model : m)) : [...list, model]
        })
        if (finished || model.status === 'ready' || model.status === 'error') {
          void queryClient.invalidateQueries({ queryKey: modelsKey })
        }
      },
      onStatus: setStatus,
    })
    return () => socket.close()
  }, [queryClient])
  return status
}
