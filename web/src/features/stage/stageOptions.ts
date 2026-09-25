// SPDX-License-Identifier: Apache-2.0
import type { Schemas } from '../../api/types'

export type StageStyle = Schemas['StageStyle']
export type StagePreset = NonNullable<StageStyle['preset']>

export const stagePresets: StagePreset[] = ['white-on-black', 'yellow-on-black', 'black-on-white']

/** Colours of each preset (tokens.css `--stage-*`), never the UI theme (UI-7). */
export const presetColours: Record<StagePreset, { bg: string; fg: string; secondary: string }> = {
  'white-on-black': {
    bg: 'var(--stage-bg-dark)',
    fg: 'var(--stage-fg-white)',
    secondary: 'var(--stage-fg-secondary)',
  },
  'yellow-on-black': {
    bg: 'var(--stage-bg-dark)',
    fg: 'var(--stage-fg-yellow)',
    secondary: 'var(--stage-fg-secondary)',
  },
  // The grey secondary is too light on white; the second language is
  // black there too, set apart by size and weight.
  'black-on-white': {
    bg: 'var(--stage-bg-light)',
    fg: 'var(--stage-fg-black)',
    secondary: 'var(--stage-fg-black)',
  },
}

/** `/stage/$id` query overrides, e.g. `?preset=yellow-on-black&dual=1&lines=2`. */
export interface StageSearch {
  preset?: StagePreset
  dual?: boolean
  lines?: number
  qr?: boolean
  lang?: string
  lang2?: string
}

const flag = (v: unknown) =>
  v === true || v === 1 || v === '1' || v === 'true'
    ? true
    : v === false || v === 0 || v === '0' || v === 'false'
      ? false
      : undefined

export function parseStageSearch(s: Record<string, unknown>): StageSearch {
  const out: StageSearch = {}
  if (stagePresets.includes(s.preset as StagePreset)) out.preset = s.preset as StagePreset
  const dual = flag(s.dual)
  if (dual !== undefined) out.dual = dual
  const qr = flag(s.qr)
  if (qr !== undefined) out.qr = qr
  const lines = Number(s.lines)
  if (Number.isInteger(lines) && lines >= 1 && lines <= 5) out.lines = lines
  if (typeof s.lang === 'string' && s.lang) out.lang = s.lang
  if (typeof s.lang2 === 'string' && s.lang2) out.lang2 = s.lang2
  return out
}

export interface StageOptions {
  preset: StagePreset
  lines: number
  qr: boolean
  /** Main track. */
  lang?: string
  /** Second track under the main one, when dual. */
  lang2?: string
}

/**
 * What the stage shows: the link's overrides, else the session's
 * stageStyle, else the defaults (white on black, 3 lines, QR on, single
 * language). The main track is the first target language; the second is
 * the next one, or the original.
 */
export function stageOptions(
  search: StageSearch,
  style: Partial<StageStyle> | undefined,
  languages: readonly string[],
): StageOptions {
  const ok = (l?: string): l is string => !!l && (l === 'source' || languages.includes(l))
  const lang = ok(search.lang) ? search.lang : (languages[0] ?? undefined)
  const dual = search.dual ?? style?.dualLanguage ?? false
  let lang2: string | undefined
  if (dual && lang) {
    lang2 =
      ok(search.lang2) && search.lang2 !== lang
        ? search.lang2
        : (languages.find((l) => l !== lang) ?? 'source')
  }
  return {
    preset: search.preset ?? style?.preset ?? 'white-on-black',
    lines: search.lines ?? style?.lines ?? 3,
    qr: search.qr ?? style?.showQr ?? true,
    lang,
    lang2,
  }
}
