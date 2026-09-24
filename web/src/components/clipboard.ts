// SPDX-License-Identifier: Apache-2.0

/**
 * Copies text to the clipboard. Uses the async Clipboard API when the page is
 * a secure context, and falls back to a hidden textarea + execCommand('copy')
 * otherwise: the admin is often opened over plain http://<LAN IP>, where
 * navigator.clipboard doesn't exist. Resolves to false when both fail.
 */
export async function copyText(text: string): Promise<boolean> {
  if (window.isSecureContext !== false && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // Permission denied or document not focused: try the legacy path.
    }
  }
  return legacyCopy(text)
}

function legacyCopy(text: string): boolean {
  if (typeof document.execCommand !== 'function') return false
  const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
  const area = document.createElement('textarea')
  area.value = text
  area.setAttribute('readonly', '')
  area.setAttribute('aria-hidden', 'true')
  Object.assign(area.style, { position: 'fixed', insetBlockStart: '0', opacity: '0' })
  document.body.append(area)
  area.select()
  let ok: boolean
  try {
    ok = document.execCommand('copy')
  } catch {
    ok = false
  }
  area.remove()
  previous?.focus()
  return ok
}
