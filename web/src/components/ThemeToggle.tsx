// SPDX-License-Identifier: Apache-2.0
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import { useColorScheme } from '@mui/material/styles'
import { useTranslation } from 'react-i18next'

const modes = ['light', 'dark', 'system'] as const
type Mode = (typeof modes)[number]

/** Segmented Light / Dark / System switch (UI-5). Must render inside AppThemeProvider. */
export function ThemeToggle() {
  const { t } = useTranslation()
  const { mode, setMode } = useColorScheme()
  return (
    <ToggleButtonGroup
      size="small"
      exclusive
      value={mode ?? 'system'}
      onChange={(_, next: Mode | null) => next && setMode(next)}
      aria-label={t('theme.label')}
    >
      {modes.map((m) => (
        <ToggleButton key={m} value={m}>
          {t(`theme.${m}`)}
        </ToggleButton>
      ))}
    </ToggleButtonGroup>
  )
}
