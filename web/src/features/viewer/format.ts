// SPDX-License-Identifier: Apache-2.0
import type { ChipStatus } from '../../components/StatusChip'
import type { SessionState } from '../../api/types'

/** Session clock seconds as HH:MM:SS, the viewer's mono timecode. */
export function timecode(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(Math.floor(s / 3600))}:${pad(Math.floor(s / 60) % 60)}:${pad(s % 60)}`
}

/** A gap marker's length in whole seconds, at least 1. */
export function gapSeconds(ms: number): number {
  return Math.max(1, Math.round(ms / 1000))
}

/** How a session state reads on a status chip. */
export const stateChip: Record<SessionState, ChipStatus> = {
  idle: 'idle',
  starting: 'starting',
  live: 'live',
  paused: 'warn',
  stopping: 'starting',
  error: 'error',
}
