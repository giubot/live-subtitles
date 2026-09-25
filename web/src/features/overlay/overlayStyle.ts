// SPDX-License-Identifier: Apache-2.0
import type { Schemas } from '../../api/types'

/** The OverlayStyle schema, as saved presets store it. */
export type OverlayStyle = Schemas['OverlayStyle']

/** Built-in presets from docs/design.md; saved presets (P3-14) are fetched by id. */
export const overlayPresets = ['classic', 'outline', 'lower-third'] as const
export type OverlayPreset = (typeof overlayPresets)[number]

export function isBuiltinPreset(id: unknown): id is OverlayPreset {
  return overlayPresets.includes(id as OverlayPreset)
}

/** What a saved preset's id may look like in a link. */
const presetIdPattern = /^[A-Za-z0-9_-]{1,64}$/

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

/** Accepted ranges, shared by link parsing, saved presets and the editor. */
export const lookLimits = {
  fontSizePx: [12, 200],
  fontWeight: [100, 900],
  outlineWidthPx: [0, 12],
  marginPx: [0, 400],
  maxLines: [1, 4],
  fadeAfterMs: [0, 600000],
} as const satisfies Record<string, readonly [number, number]>

/** Query parameters, named like OverlayStyle without the units. */
export interface OverlaySearch {
  lang?: string
  /** A built-in preset name or a saved preset's id. */
  preset?: string
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

/** A background: a CSS colour or `transparent`. */
export function safeBackground(v: unknown): string | undefined {
  return v === 'transparent' ? 'transparent' : safeColor(v)
}

function int(v: unknown, [min, max]: readonly [number, number]): number | undefined {
  const n = Number(v)
  return v !== '' && v != null && Number.isInteger(n) && n >= min && n <= max ? n : undefined
}

export function parseOverlaySearch(s: Record<string, unknown>): OverlaySearch {
  const out: OverlaySearch = {}
  const set = <K extends keyof OverlaySearch>(k: K, v: OverlaySearch[K] | undefined) => {
    if (v !== undefined) out[k] = v
  }
  if (typeof s.lang === 'string' && s.lang) out.lang = s.lang
  if (typeof s.preset === 'string' && presetIdPattern.test(s.preset)) out.preset = s.preset
  set('fontSize', int(s.fontSize, lookLimits.fontSizePx))
  set('fontWeight', int(s.fontWeight, lookLimits.fontWeight))
  set('color', safeColor(s.color))
  set('outlineColor', safeColor(s.outlineColor))
  set('outlineWidth', int(s.outlineWidth, lookLimits.outlineWidthPx))
  set('background', safeBackground(s.background))
  if (s.position === 'top' || s.position === 'bottom') out.position = s.position
  if (s.align === 'left' || s.align === 'center' || s.align === 'right') out.align = s.align
  set('margin', int(s.margin, lookLimits.marginPx))
  set('maxLines', int(s.maxLines, lookLimits.maxLines))
  set('fadeAfter', int(s.fadeAfter, lookLimits.fadeAfterMs))
  const interim = s.interim
  if (interim === 0 || interim === '0' || interim === false || interim === 'false')
    out.interim = false
  if (interim === 1 || interim === '1' || interim === true || interim === 'true') out.interim = true
  return out
}

/**
 * A saved preset's style as a look: fields it leaves out, or values the
 * browser wouldn't take, come from `base`.
 */
export function lookFromStyle(
  style: Partial<OverlayStyle>,
  base: OverlayLook = classic,
): OverlayLook {
  return {
    fontSizePx: int(style.fontSizePx, lookLimits.fontSizePx) ?? base.fontSizePx,
    fontWeight: int(style.fontWeight, lookLimits.fontWeight) ?? base.fontWeight,
    color: safeColor(style.color) ?? base.color,
    outlineColor: safeColor(style.outlineColor) ?? base.outlineColor,
    outlineWidthPx: int(style.outlineWidthPx, lookLimits.outlineWidthPx) ?? base.outlineWidthPx,
    background: safeBackground(style.background) ?? base.background,
    position:
      style.position === 'top' || style.position === 'bottom' ? style.position : base.position,
    align:
      style.align === 'left' || style.align === 'center' || style.align === 'right'
        ? style.align
        : base.align,
    marginPx: int(style.marginPx, lookLimits.marginPx) ?? base.marginPx,
    maxLines: int(style.maxLines, lookLimits.maxLines) ?? base.maxLines,
    fadeAfterMs: int(style.fadeAfterMs, lookLimits.fadeAfterMs) ?? base.fadeAfterMs,
    showInterim: typeof style.showInterim === 'boolean' ? style.showInterim : base.showInterim,
  }
}

/** A look as the OverlayStyle a preset saves. The overlay always uses the body font. */
export function styleFromLook(look: OverlayLook): OverlayStyle {
  return { ...look, fontFamily: 'var(--font-body)' }
}

/**
 * The look for a link: its preset (a built-in, or `saved` when the link
 * names a saved preset) with the link's overrides on top.
 */
export function resolveLook(s: OverlaySearch, saved?: OverlayLook): OverlayLook {
  const base = saved ?? (isBuiltinPreset(s.preset) ? presetLooks[s.preset] : classic)
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

/** Converts px at 1080p into a CSS length. */
export type LengthUnit = (px: number) => string

/** px at 1080p → a length that scales with the browser source's height. */
export const vh: LengthUnit = (px) => `${+(px / 10.8).toFixed(4)}vh`

/** px at 1080p → a length that scales with a 16:9 container's width (previews). */
export const cqw: LengthUnit = (px) => `${+(px / 19.2).toFixed(4)}cqw`

/** An outline drawn with text-shadow in eight directions (OBS's CEF lacks paint-order for HTML text). */
export function outlineShadow(
  widthPx: number,
  color: string,
  unit: LengthUnit = vh,
): string | undefined {
  if (widthPx <= 0) return undefined
  const w = unit(widthPx)
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
