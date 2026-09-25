// SPDX-License-Identifier: Apache-2.0
import type { ChipStatus } from '../../components/StatusChip'
import type { Recording } from '../replay/replay'

/** How a recording status reads on a status chip. */
export const recordingChip: Record<Recording['status'], ChipStatus> = {
  recording: 'live',
  complete: 'ok',
  failed: 'error',
}

const units = ['byte', 'kilobyte', 'megabyte', 'gigabyte', 'terabyte'] as const

/** A byte count in the largest unit that keeps it ≥ 1 ("1.4 GB"), localised. */
export function formatBytes(bytes: number, locale?: string): string {
  let value = Math.max(0, bytes)
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return new Intl.NumberFormat(locale, {
    style: 'unit',
    unit: units[unit],
    unitDisplay: 'short',
    maximumFractionDigits: unit === 0 ? 0 : 1,
  }).format(value)
}

/** Seconds of audio: `durationSec`, else the span from start to end, if ended. */
export function durationOf(r: Recording): number | undefined {
  if (r.durationSec != null) return r.durationSec
  if (!r.endedAt) return undefined
  return (Date.parse(r.endedAt) - Date.parse(r.startedAt)) / 1000
}
