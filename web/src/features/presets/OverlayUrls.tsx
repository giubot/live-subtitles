// SPDX-License-Identifier: Apache-2.0
import Box from '@mui/material/Box'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import { CopyField } from '../../components/CopyField'
import { ErrorAlert } from '../../components/ErrorAlert'
import { nativeLanguageName } from '../../components/languageNames'
import { Panel } from '../../components/Panel'
import { overlayUrl } from './presetForm'

export interface OverlayUrlsProps {
  /** The server's preset id (built-in or saved); absent while a new preset isn't saved yet. */
  presetId?: string
}

/** "Copy overlay URL" for each caption track of a session, with this preset. */
export function OverlayUrls({ presetId }: OverlayUrlsProps) {
  const { t } = useTranslation('presets')
  const sessions = api.useQuery('get', '/api/sessions')
  const [chosen, setChosen] = useState<string>()
  const session = sessions.data?.find((s) => s.id === chosen) ?? sessions.data?.[0]

  return (
    <Panel title={t('urls.heading')} titleAs="h2">
      {sessions.error && <ErrorAlert error={sessions.error} />}
      {sessions.data?.length === 0 && (
        <Typography sx={{ color: 'var(--color-neutral)' }}>{t('urls.noSessions')}</Typography>
      )}
      {session && (
        <Box sx={{ display: 'grid', gap: 'var(--space-sm)' }}>
          <TextField
            select
            label={t('urls.session')}
            value={session.id}
            onChange={(e) => setChosen(e.target.value)}
            helperText={t('urls.sessionHelp')}
            slotProps={{ select: { native: true }, inputLabel: { shrink: true } }}
            sx={{ maxInlineSize: '24rem' }}
          >
            {sessions.data?.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name ?? s.id}
              </option>
            ))}
          </TextField>
          {(session.targetLanguages ?? []).map((lang) => (
            <CopyField
              key={lang}
              label={t('urls.label', { lang: nativeLanguageName(lang) })}
              value={overlayUrl(session.urls.overlay, lang, presetId ?? 'classic')}
              copyLabel={t('urls.copy')}
              disabled={!presetId}
              disabledReason={t('urls.saveFirst')}
            />
          ))}
        </Box>
      )}
    </Panel>
  )
}
