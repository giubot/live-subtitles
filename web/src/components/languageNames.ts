// SPDX-License-Identifier: Apache-2.0

/** LanguagePicker value for the untranslated source track. */
export const sourceTrack = 'source'

/**
 * A language's name in that language ("Español", "English", "Português"), so
 * each option is recognisable whatever the UI language. Falls back to the
 * code when the runtime has no name for it.
 */
export function nativeLanguageName(code: string): string {
  let name: string | undefined
  try {
    name = new Intl.DisplayNames([code], { type: 'language' }).of(code)
  } catch {
    name = undefined // not a valid BCP-47 tag
  }
  if (!name || name === code) return code
  return name.charAt(0).toLocaleUpperCase(code) + name.slice(1)
}
