// SPDX-License-Identifier: Apache-2.0
import TextField from '@mui/material/TextField'
import type { SxProps, Theme } from '@mui/material/styles'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { nativeLanguageName, sourceTrack } from './languageNames'

export interface LanguagePickerProps {
  /** Selected language code, or `source`. */
  value: string
  onChange: (lang: string) => void
  /** BCP-47 codes of the available caption tracks, e.g. `['es', 'en']`. */
  languages: string[]
  /** Offer the original (untranslated) track first. */
  includeSource?: boolean
  /** Visible label above the field. Defaults to "Caption language". */
  label?: ReactNode
  /** Helper text below; while disabled, say why here. */
  helperText?: ReactNode
  disabled?: boolean
  error?: boolean
  sx?: SxProps<Theme>
}

/**
 * Caption-language select. Each language is named in itself ("Español",
 * "English") with a matching `lang`, so viewers find theirs whatever the UI
 * language. A native <select>: it's the best picker on phones.
 */
export function LanguagePicker({
  value,
  onChange,
  languages,
  includeSource,
  label,
  helperText,
  disabled,
  error,
  sx,
}: LanguagePickerProps) {
  const { t } = useTranslation()
  return (
    <TextField
      select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      label={label ?? t('captionLanguage.label')}
      helperText={helperText ?? ' '}
      disabled={disabled}
      error={error}
      sx={sx}
      slotProps={{
        select: { native: true },
        htmlInput: { 'aria-invalid': error || undefined },
      }}
    >
      {includeSource && <option value={sourceTrack}>{t('captionLanguage.source')}</option>}
      {languages.map((lang) => (
        <option key={lang} value={lang} lang={lang}>
          {nativeLanguageName(lang)}
        </option>
      ))}
    </TextField>
  )
}
