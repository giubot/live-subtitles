// SPDX-License-Identifier: Apache-2.0
import type { Caption } from '../../api/types'
import { toApiError } from '../../components/apiError'

/** The code the server answers (501) for an operation it doesn't implement yet. */
export const notImplementedCode = 'not_implemented'

/** True when a failed request means the server has no such feature yet (501). */
export function isNotImplemented(err: unknown): boolean {
  const e = toApiError(err)
  return e?.code === notImplementedCode || e?.params?.status === 501
}

/**
 * Sessions whose server answered 501 to a correction, so the editor shows
 * the "not available" state straight away when it is opened again.
 */
const unavailable = new Set<string>()
export const correctionsUnavailable = {
  has: (sessionId: string) => unavailable.has(sessionId),
  add: (sessionId: string) => void unavailable.add(sessionId),
}

/** A caption line in the editor: live finals plus the ones hidden from here. */
export interface EditorLine {
  caption: Caption
  hidden: boolean
}

/**
 * The lines to show, newest first: the track's finals (which never include
 * hidden ones, see realtime/captions.ts) merged with the lines hidden from
 * this panel, so they can be unhidden.
 */
export function editorLines(finals: Caption[], hidden: Record<string, Caption>): EditorLine[] {
  const lines: EditorLine[] = finals
    .filter((c) => !hidden[c.segmentId])
    .map((caption) => ({ caption, hidden: false }))
  for (const caption of Object.values(hidden)) lines.push({ caption, hidden: true })
  return lines.sort((a, b) => b.caption.start - a.caption.start)
}

/** Seconds on the session clock as m:ss, or h:mm:ss past an hour. */
export function clockTime(sec: number): string {
  const s = Math.max(0, Math.floor(sec))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const ss = String(s % 60).padStart(2, '0')
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`
}
