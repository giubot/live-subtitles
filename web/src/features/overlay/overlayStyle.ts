// SPDX-License-Identifier: Apache-2.0

/** Built-in presets from docs/design.md; saved presets come with P3-14. */
export const overlayPresets = ['classic', 'outline', 'lower-third'] as const
export type OverlayPreset = (typeof overlayPresets)[number]

/** A resolved overlay style (the OverlayStyle schema, sizes in px at 1080p). */
export interface OverlayLook {
  fontSizePx: number
  fontWeight: number
  color: string
  outlineColor: string
  outlineWidthPx: number
  background: string
  position: 'top' | 'bottom'
  align: 'left' | 'center' | 'right'
  marginPx: number
  maxLines: number
  /** Hide after this long without new captions; 0 keeps them up. */
  fadeAfterMs: number
  showInterim: boolean
}

const classic: OverlayLook = {
  fontSizePx: 44,
  fontWeight: 600,
  color: 'var(--overlay-fg)',
  outlineColor: 'var(--overlay-outline)',
  outlineWidthPx: 1,
  background: 'var(--overlay-box)',
  position: 'bottom',
  align: 'center',
  marginPx: 60,
  maxLines: 2,
  fadeAfterMs: 6000,
  showInterim: true,
}

export const presetLooks: Record<OverlayPreset, OverlayLook> = {
  classic,
  outline: { ...classic, background: 'transparent', outlineWidthPx: 3 },
  'lower-third': { ...classic, align: 'left', fontSizePx: 38, marginPx: 80 },
}

/** Query parameters, named like OverlayStyle without the units. */
export interface OverlaySearch {
  lang?: string
  preset?: OverlayPreset
  fontSize?: number
  fontWeight?: number
  color?: string
  outlineColor?: string
  outlineWidth?: number
  background?: string
  position?: OverlayLook['position']
  align?: OverlayLook['align']
  margin?: number
  maxLines?: number
  fadeAfter?: number
  interim?: boolean
}

/** A CSS colour from a link, or undefined if the browser wouldn't take it. */
export function safeColor(v: unknown): string | undefined {
  if (typeof v !== 'string' || v.length === 0 || v.length > 64) return undefined
  if (typeof CSS !== 'undefined' && typeof CSS.supports === 'function') {
    return CSS.supports('color', v) ? v : undefined
  }
  return /^[#a-zA-Z0-9(),.%\s/-]+$/.test(v) ? v : undefined
}

function int(v: unknown, min: number, max: number): number | undefined {
  const n = Number(v)
  return v !== '' && v != null && Number.isInteger(n) && n >= min && n <= max ? n : undefined
}

export function parseOverlaySearch(s: Record<string, unknown>): OverlaySearch {
  const out: OverlaySearch = {}
  const set = <K extends keyof OverlaySearch>(k: K, v: OverlaySearch[K] | undefined) => {
    if (v !== undefined) out[k] = v
  }
  if (typeof s.lang === 'string' && s.lang) out.lang = s.lang
  if (overlayPresets.includes(s.preset as OverlayPreset)) out.preset = s.preset as OverlayPreset
  set('fontSize', int(s.fontSize, 12, 200))
  set('fontWeight', int(s.fontWeight, 100, 900))
  set('color', safeColor(s.color))
  set('outlineColor', safeColor(s.outlineColor))
  set('outlineWidth', int(s.outlineWidth, 0, 12))
  set('background', s.background === 'transparent' ? 'transparent' : safeColor(s.background))
  if (s.position === 'top' || s.position === 'bottom') out.position = s.position
  if (s.align === 'left' || s.align === 'center' || s.align === 'right') out.align = s.align
  set('margin', int(s.margin, 0, 400))
  set('maxLines', int(s.maxLines, 1, 4))
  set('fadeAfter', int(s.fadeAfter, 0, 600000))
  const interim = s.interim
  if (interim === 0 || interim === '0' || interim === false || interim === 'false')
    out.interim = false
  if (interim === 1 || interim === '1' || interim === true || interim === 'true') out.interim = true
  return out
}

/** The preset's look with the link's overrides on top. */
export function resolveLook(s: OverlaySearch): OverlayLook {
  const base = presetLooks[s.preset ?? 'classic']
  return {
    fontSizePx: s.fontSize ?? base.fontSizePx,
    fontWeight: s.fontWeight ?? base.fontWeight,
    color: s.color ?? base.color,
    outlineColor: s.outlineColor ?? base.outlineColor,
    outlineWidthPx: s.outlineWidth ?? base.outlineWidthPx,
    background: s.background ?? base.background,
    position: s.position ?? base.position,
    align: s.align ?? base.align,
    marginPx: s.margin ?? base.marginPx,
    maxLines: s.maxLines ?? base.maxLines,
    fadeAfterMs: s.fadeAfter ?? base.fadeAfterMs,
    showInterim: s.interim ?? base.showInterim,
  }
}

/** px at 1080p → a length that scales with the browser source's height. */
export const vh = (px: number) => `${+(px / 10.8).toFixed(4)}vh`

/** An outline drawn with text-shadow in eight directions (OBS's CEF lacks paint-order for HTML text). */
export function outlineShadow(widthPx: number, color: string): string | undefined {
  if (widthPx <= 0) return undefined
  const w = vh(widthPx)
  const dirs = [
    [1, 0],
    [-1, 0],
    [0, 1],
    [0, -1],
    [1, 1],
    [1, -1],
    [-1, 1],
    [-1, -1],
  ]
  return dirs.map(([x, y]) => `calc(${x} * ${w}) calc(${y} * ${w}) 0 ${color}`).join(', ')
}
