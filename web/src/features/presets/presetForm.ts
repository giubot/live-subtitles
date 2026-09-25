// SPDX-License-Identifier: Apache-2.0
import type { Schemas } from '../../api/types'
import {
  lookFromStyle,
  lookLimits,
  presetLooks,
  safeBackground,
  safeColor,
  styleFromLook,
  type OverlayLook,
  type OverlayStyle,
} from '../overlay/overlayStyle'

export type SavedPreset = Schemas['OverlayPreset']
export type SavedPresetInput = Schemas['OverlayPresetInput']

/** The editor's fields as typed: numbers stay strings until they're valid. */
export interface PresetForm {
  name: string
  fontSizePx: string
  fontWeight: string
  color: string
  outlineColor: string
  outlineWidthPx: string
  background: string
  position: OverlayLook['position']
  align: OverlayLook['align']
  marginPx: string
  maxLines: string
  /** Seconds in the form, milliseconds in the style. */
  fadeAfterS: string
  showInterim: boolean
}

export type NumberField = 'fontSizePx' | 'outlineWidthPx' | 'marginPx' | 'maxLines' | 'fadeAfterS'
export type ColorField = 'color' | 'outlineColor' | 'background'
export type FormProblem = 'name' | 'range' | 'color'

export const fontWeights = ['400', '500', '600', '700', '800'] as const

const limits: Record<NumberField, readonly [number, number]> = {
  fontSizePx: lookLimits.fontSizePx,
  outlineWidthPx: lookLimits.outlineWidthPx,
  marginPx: lookLimits.marginPx,
  maxLines: lookLimits.maxLines,
  fadeAfterS: [lookLimits.fadeAfterMs[0] / 1000, lookLimits.fadeAfterMs[1] / 1000],
}

export function limitsOf(f: NumberField) {
  const [min, max] = limits[f]
  return { min, max }
}

export function formFromLook(name: string, look: OverlayLook): PresetForm {
  return {
    name,
    fontSizePx: String(look.fontSizePx),
    fontWeight: String(look.fontWeight),
    color: look.color,
    outlineColor: look.outlineColor,
    outlineWidthPx: String(look.outlineWidthPx),
    background: look.background,
    position: look.position,
    align: look.align,
    marginPx: String(look.marginPx),
    maxLines: String(look.maxLines),
    fadeAfterS: String(look.fadeAfterMs / 1000),
    showInterim: look.showInterim,
  }
}

function num(v: string): number | undefined {
  const n = Number(v.trim())
  return v.trim() !== '' && Number.isFinite(n) ? n : undefined
}

/** The form's style; values that aren't numbers yet are left out. */
function partialStyle(f: PresetForm): Partial<OverlayStyle> {
  const whole = (v: string) => {
    const n = num(v)
    return n === undefined ? undefined : Math.round(n)
  }
  const fade = num(f.fadeAfterS)
  return {
    fontSizePx: whole(f.fontSizePx),
    fontWeight: whole(f.fontWeight),
    color: f.color.trim(),
    outlineColor: f.outlineColor.trim(),
    outlineWidthPx: whole(f.outlineWidthPx),
    background: f.background.trim(),
    position: f.position,
    align: f.align,
    marginPx: whole(f.marginPx),
    maxLines: whole(f.maxLines),
    fadeAfterMs: fade === undefined ? undefined : Math.round(fade * 1000),
    showInterim: f.showInterim,
  }
}

/** What the preview shows: invalid fields fall back to Classic box. */
export function lookFromForm(f: PresetForm): OverlayLook {
  return lookFromStyle(partialStyle(f), presetLooks.classic)
}

/**
 * The style to save, once `validate` passes. The overlay always uses the
 * body font, so `fontFamily` isn't sent (the server only takes font names,
 * not a `var(--font-body)` reference).
 */
export function styleFromForm(f: PresetForm): OverlayStyle {
  const style: Partial<OverlayStyle> = styleFromLook(lookFromForm(f))
  delete style.fontFamily
  // The generated type marks defaulted fields required; the server fills a missing fontFamily.
  return style as OverlayStyle
}

/** The form field behind each path the server names in a 400's `fields`. */
const serverFieldOf: Record<string, keyof PresetForm> = {
  name: 'name',
  'style.fontSizePx': 'fontSizePx',
  'style.fontWeight': 'fontWeight',
  'style.color': 'color',
  'style.outlineColor': 'outlineColor',
  'style.outlineWidthPx': 'outlineWidthPx',
  'style.background': 'background',
  'style.marginPx': 'marginPx',
  'style.maxLines': 'maxLines',
  'style.fadeAfterMs': 'fadeAfterS',
}

/** Field → problem from the server's rejected fields (overlay.invalid_color, …). */
export function serverProblems(
  fields: Record<string, string> | undefined,
): Partial<Record<keyof PresetForm, FormProblem>> {
  const out: Partial<Record<keyof PresetForm, FormProblem>> = {}
  for (const [path, code] of Object.entries(fields ?? {})) {
    const f = serverFieldOf[path]
    if (!f) continue
    out[f] = f === 'name' ? 'name' : code === 'overlay.invalid_color' ? 'color' : 'range'
  }
  return out
}

/** Field → problem, checked before saving. */
export function validate(f: PresetForm): Partial<Record<keyof PresetForm, FormProblem>> {
  const bad: Partial<Record<keyof PresetForm, FormProblem>> = {}
  if (f.name.trim() === '' || f.name.length > 80) bad.name = 'name'
  for (const k of Object.keys(limits) as NumberField[]) {
    const n = num(f[k])
    const [min, max] = limits[k]
    if (n === undefined || n < min || n > max) bad[k] = 'range'
  }
  if (!safeColor(f.color.trim())) bad.color = 'color'
  if (!safeColor(f.outlineColor.trim())) bad.outlineColor = 'color'
  if (!safeBackground(f.background.trim())) bad.background = 'color'
  return bad
}

/** The overlay link for a session track with a preset. */
export function overlayUrl(sessionOverlayUrl: string, lang: string, presetId: string): string {
  const url = new URL(sessionOverlayUrl, window.location.origin)
  url.searchParams.set('lang', lang)
  url.searchParams.set('preset', presetId)
  return url.toString()
}
