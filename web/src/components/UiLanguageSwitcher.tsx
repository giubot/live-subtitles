// SPDX-License-Identifier: Apache-2.0
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import { useTranslation } from 'react-i18next'
import { uiLanguages } from '../i18n'
import { nativeLanguageName } from './languageNames'
import { segmentedSx } from './segmented'

/** Segmented UI language switch (UI-2). Each language is named in itself. */
export function UiLanguageSwitcher() {
  const { t, i18n } = useTranslation()
  return (
    <ToggleButtonGroup
      exclusive
      value={i18n.resolvedLanguage}
      onChange={(_, lang: string | null) => lang && void i18n.changeLanguage(lang)}
      aria-label={t('uiLanguage.label')}
      sx={segmentedSx}
    >
      {uiLanguages.map((lang) => (
        <ToggleButton key={lang} value={lang} lang={lang}>
          {nativeLanguageName(lang)}
        </ToggleButton>
      ))}
    </ToggleButtonGroup>
  )
}
