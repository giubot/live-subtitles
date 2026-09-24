// SPDX-License-Identifier: Apache-2.0
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import { useTranslation } from 'react-i18next'
import { uiLanguages } from '../i18n'

/** Language names in their own language, so each is recognisable whatever the UI language. */
function nativeName(lang: string): string {
  const name = new Intl.DisplayNames([lang], { type: 'language' }).of(lang) ?? lang
  return name.charAt(0).toLocaleUpperCase(lang) + name.slice(1)
}

/** Segmented UI language switch (UI-2). P1-12 gives it the design-system look. */
export function UiLanguageSwitcher() {
  const { t, i18n } = useTranslation()
  return (
    <ToggleButtonGroup
      size="small"
      exclusive
      value={i18n.resolvedLanguage}
      onChange={(_, lang: string | null) => lang && void i18n.changeLanguage(lang)}
      aria-label={t('uiLanguage.label')}
    >
      {uiLanguages.map((lang) => (
        <ToggleButton key={lang} value={lang} lang={lang}>
          {nativeName(lang)}
        </ToggleButton>
      ))}
    </ToggleButtonGroup>
  )
}
