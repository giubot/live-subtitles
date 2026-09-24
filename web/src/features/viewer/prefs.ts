// SPDX-License-Identifier: Apache-2.0

// Per-device viewer preferences. Storage can be missing or blocked
// (private mode, embedded browsers); the viewer then just forgets.

export const langStorageKey = 'ls.viewer.lang'
export const sizeStorageKey = 'ls.viewer.size'

/** Caption size steps, as multiples of --text-caption. */
export const sizeSteps = [0.8, 1, 1.25, 1.5, 1.8] as const
export const defaultSizeStep = 1

/** The multiplier of a size step. */
export const sizeStep = (i: number): number => sizeSteps[i] ?? 1

export function readPref(key: string): string | undefined {
  try {
    return localStorage.getItem(key) ?? undefined
  } catch {
    return undefined
  }
}

export function writePref(key: string, value: string): void {
  try {
    localStorage.setItem(key, value)
  } catch {
    // Not stored; the choice lasts for this visit.
  }
}

export function readSizeStep(): number {
  const n = Number(readPref(sizeStorageKey))
  return Number.isInteger(n) && n >= 0 && n < sizeSteps.length ? n : defaultSizeStep
}

/**
 * The caption track to show: the link's `?lang=`, else the last choice on
 * this device, else the UI language, else the session's first language.
 * Only tracks the session has (or `source`) are picked.
 */
export function pickTrack(
  available: readonly string[],
  opts: { requested?: string; stored?: string; uiLanguage?: string },
): string | undefined {
  const ok = (l?: string): l is string => !!l && (l === 'source' || available.includes(l))
  for (const l of [opts.requested, opts.stored, opts.uiLanguage]) if (ok(l)) return l
  return available[0]
}
