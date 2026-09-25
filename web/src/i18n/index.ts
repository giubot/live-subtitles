// SPDX-License-Identifier: Apache-2.0
import i18n from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'

// Every web/src/locales/<lang>/<namespace>.json is bundled. A new language
// is a new folder, with no code changes (UI-8).
const files = import.meta.glob<{ default: Record<string, unknown> }>('../locales/*/*.json', {
  eager: true,
})

export const resources: Record<string, Record<string, Record<string, unknown>>> = {}
for (const [path, mod] of Object.entries(files)) {
  const [, lang, ns] = /\/locales\/([^/]+)\/([^/]+)\.json$/.exec(path) ?? []
  if (lang && ns) (resources[lang] ??= {})[ns] = mod.default
}

export const uiLanguages = Object.keys(resources).sort()
export const fallbackLanguage = 'en'

/** Where the chosen UI language is stored per device (UI-2). */
export const storageKey = 'ls.ui'

// Detection order: ?ui=<lang> (es, en, pt…), then the stored choice, then the browser.
// Browser languages match on the base code (es-AR → es); anything
// unsupported falls back to English.
void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources,
    supportedLngs: uiLanguages,
    nonExplicitSupportedLngs: true,
    load: 'languageOnly',
    fallbackLng: fallbackLanguage,
    ns: Object.keys(resources[fallbackLanguage] ?? {}),
    defaultNS: 'common',
    interpolation: { escapeValue: false }, // React escapes
    returnNull: false,
    detection: {
      order: ['querystring', 'localStorage', 'navigator'],
      lookupQuerystring: 'ui',
      lookupLocalStorage: storageKey,
      caches: ['localStorage'],
    },
  })

/** The active UI language reduced to a supported base code. */
export function currentLanguage(): string {
  const lang = i18n.resolvedLanguage ?? i18n.language ?? fallbackLanguage
  return lang.split('-')[0] ?? fallbackLanguage
}

function syncHtmlLang() {
  document.documentElement.lang = currentLanguage()
}
i18n.on('languageChanged', syncHtmlLang)
if (i18n.isInitialized) syncHtmlLang()

export default i18n
