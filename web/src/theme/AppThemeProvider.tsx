// SPDX-License-Identifier: Apache-2.0
import CssBaseline from '@mui/material/CssBaseline'
import { enUS, esES } from '@mui/material/locale'
import { createTheme, ThemeProvider } from '@mui/material/styles'
import { useMemo, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { colorSchemeStorageKey, themeOptions, themeStorageKey } from './theme'

const muiLocales: Record<string, typeof enUS> = { en: enUS, es: esES }

/**
 * Design-system theme for the themed surfaces (admin, setup, capture, viewer,
 * replay). The stage screen and the overlay render outside it (UI-7).
 * Light / Dark / System is stored per device and mirrored to
 * <html data-theme>, where tokens.css picks it up; index.html applies the
 * stored choice before the first paint. MUI's component locale follows the
 * UI language (UI-3).
 */
export function AppThemeProvider({ children }: { children: ReactNode }) {
  const { i18n } = useTranslation()
  const lang = i18n.resolvedLanguage ?? 'en'
  const theme = useMemo(() => createTheme(themeOptions, muiLocales[lang] ?? enUS), [lang])
  return (
    <ThemeProvider
      theme={theme}
      defaultMode="system"
      modeStorageKey={themeStorageKey}
      colorSchemeStorageKey={colorSchemeStorageKey}
      disableTransitionOnChange
    >
      <CssBaseline enableColorScheme />
      {children}
    </ThemeProvider>
  )
}
