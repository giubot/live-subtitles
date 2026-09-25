// SPDX-License-Identifier: Apache-2.0
import type { Caption, Schemas } from '../../api/types'

export type Recording = Schemas['Recording']
export type DownloadFormat = 'srt' | 'vtt' | 'txt'

/** Recorded audio (AAC in MP4, served with Range support). */
export function audioUrl(recordingId: string): string {
  return `/api/recordings/${encodeURIComponent(recordingId)}/audio`
}

/** A subtitle export of one track of one recording (OUT-8). */
export function subtitlesUrl(
  sessionId: string,
  recordingId: string,
  lang: string,
  format: DownloadFormat,
): string {
  const q = new URLSearchParams({ format, lang, recordingId })
  return `/api/public/sessions/${encodeURIComponent(sessionId)}/subtitles?${q.toString()}`
}

/** Recordings sorted newest first. */
export function newestFirst(list: readonly Recording[]): Recording[] {
  return [...list].sort((a, b) => b.startedAt.localeCompare(a.startedAt))
}

/**
 * The recording to open: the one asked for, else the newest complete one,
 * else the newest. `list` is sorted newest first.
 */
export function pickRecording(list: readonly Recording[], wanted?: string): Recording | undefined {
  return list.find((r) => r.id === wanted) ?? list.find((r) => r.status === 'complete') ?? list[0]
}

/**
 * Index of the cue playing at `time` (seconds): the last one that has
 * started. Between cues the previous one stays highlighted; before the
 * first there's none (-1). Cues are sorted by start.
 */
export function cueAt(cues: readonly Caption[], time: number): number {
  let lo = 0
  let hi = cues.length - 1
  let found = -1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    if ((cues[mid]?.start ?? 0) <= time) {
      found = mid
      lo = mid + 1
    } else {
      hi = mid - 1
    }
  }
  return found
}
