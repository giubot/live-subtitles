// SPDX-License-Identifier: Apache-2.0
import CssBaseline from '@mui/material/CssBaseline'
import { createTheme, ThemeProvider } from '@mui/material/styles'
import { enUS, esES } from '@mui/material/locale'
import { useMemo, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

const muiLocales: Record<string, typeof enUS> = { en: enUS, es: esES }

/**
 * MUI theme for the themed surfaces (admin, setup, capture, viewer, replay).
 * The stage screen and the overlay render outside it (UI-7).
 * P0-09 replaces the base theme with the design system (tokens, fonts,
 * light/dark); the MUI component locale follows the UI language (UI-3).
 */
export function AppThemeProvider({ children }: { children: ReactNode }) {
  const { i18n } = useTranslation()
  const lang = i18n.resolvedLanguage ?? 'en'
  const theme = useMemo(() => createTheme({}, muiLocales[lang] ?? enUS), [lang])
  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      {children}
    </ThemeProvider>
  )
}
