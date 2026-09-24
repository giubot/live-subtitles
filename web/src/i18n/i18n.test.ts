// SPDX-License-Identifier: Apache-2.0
import { beforeEach, describe, expect, it, vi } from 'vitest'

// i18n detects the language once, on import, so each case loads a fresh copy.
async function loadWith({
  url = '/',
  stored,
  browser,
}: {
  url?: string
  stored?: string
  browser: string[]
}) {
  vi.resetModules()
  window.history.replaceState(null, '', url)
  localStorage.clear()
  if (stored) localStorage.setItem('ls.ui', stored)
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(browser)
  vi.spyOn(navigator, 'language', 'get').mockReturnValue(browser[0] ?? '')
  const mod = await import('./index')
  await mod.default.loadNamespaces('common')
  return mod
}

describe('UI language detection (UI-2)', () => {
  beforeEach(() => vi.restoreAllMocks())

  it.each([
    { name: 'Spanish browser', browser: ['es-AR', 'en'], want: 'es' },
    { name: 'English browser', browser: ['en-US'], want: 'en' },
    { name: 'unsupported browser language falls back to English', browser: ['fr-FR'], want: 'en' },
    { name: '?ui= wins over the browser', url: '/s/main?ui=es', browser: ['en-US'], want: 'es' },
    { name: 'stored choice wins over the browser', stored: 'en', browser: ['es-ES'], want: 'en' },
    {
      name: '?ui= wins over the stored choice',
      url: '/?ui=en',
      stored: 'es',
      browser: ['es-ES'],
      want: 'en',
    },
  ])('$name → $want', async ({ url, stored, browser, want }) => {
    const { currentLanguage, default: i18n } = await loadWith({ url, stored, browser })
    expect(currentLanguage()).toBe(want)
    expect(document.documentElement.lang).toBe(want)
    expect(i18n.t('appName')).toBe(want === 'es' ? 'Subtítulos en vivo' : 'Live Subtitles')
  })

  it('stores ?ui= for the next visit', async () => {
    await loadWith({ url: '/?ui=es', browser: ['en-US'] })
    expect(localStorage.getItem('ls.ui')).toBe('es')
  })

  it('bundles every locale folder', async () => {
    const { uiLanguages } = await loadWith({ browser: ['en'] })
    expect(uiLanguages).toEqual(['en', 'es'])
  })
})
