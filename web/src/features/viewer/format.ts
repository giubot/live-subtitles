// SPDX-License-Identifier: Apache-2.0
import type { ChipStatus } from '../../components/StatusChip'
import type { SessionState } from '../../api/types'

/** Session clock seconds as HH:MM:SS, the viewer's mono timecode. */
export function timecode(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(Math.floor(s / 3600))}:${pad(Math.floor(s / 60) % 60)}:${pad(s % 60)}`
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
