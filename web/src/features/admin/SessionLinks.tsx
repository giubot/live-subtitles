// SPDX-License-Identifier: Apache-2.0
import KeyOutlined from '@mui/icons-material/KeyOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Typography from '@mui/material/Typography'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import { CopyField } from '../../components/CopyField'
import { ErrorAlert } from '../../components/ErrorAlert'
import { QrCode } from '../../components/QrCode'
import { captureBase, captureUrl, type Session } from './sessionForm'

export interface SessionLinksProps {
  session: Session
  token?: string
  onToken: (token: string) => void
}

/**
 * Links to share for a session (OUT-4): viewer (with a QR code for the
 * audience), stage, overlay and capture. The ingest token is stored
 * hashed, so the capture link can only be shown right after creating the
 * session or after making a new token, which retires older links.
 */
export function SessionLinks({ session, token, onToken }: SessionLinksProps) {
  const { t } = useTranslation('admin')
  const [confirm, setConfirm] = useState(false)
  const rotate = api.useMutation('post', '/api/sessions/{sessionId}/ingest-token', {
    onSuccess: (data) => {
      setConfirm(false)
      onToken(data.ingestToken)
    },
  })

  return (
    <Box
      sx={{
        display: 'grid',
        gap: 'var(--space-md)',
        paddingBlockStart: 'var(--space-md)',
        borderBlockStart: 'var(--rule-hair) solid var(--color-rule)',
        '@media (min-width: 48rem)': { gridTemplateColumns: 'minmax(0, 1fr) auto' },
      }}
    >
      <Box sx={{ display: 'grid', gap: 'var(--space-sm)', minInlineSize: 0 }}>
        <CopyField label={t('links.viewer')} value={session.urls.viewer} />
        <CopyField label={t('links.stage')} value={session.urls.stage} />
        <CopyField label={t('links.overlay')} value={session.urls.overlay} />
        {token ? (
          <CopyField label={t('links.capture')} value={captureUrl(captureBase(session), token)} />
        ) : (
          <Box sx={{ display: 'grid', gap: 'var(--space-xs)', justifyItems: 'start' }}>
            <Typography variant="overline" component="span">
              {t('links.capture')}
            </Typography>
            <Typography
              variant="body2"
              sx={{ color: 'var(--color-neutral)', maxInlineSize: 'var(--measure)' }}
            >
              {confirm ? t('links.rotateConfirm') : t('links.captureHidden')}
            </Typography>
            {rotate.error && <ErrorAlert error={rotate.error} />}
            <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-xs)' }}>
              {confirm ? (
                <>
                  <Button
                    variant="outlined"
                    color="secondary"
                    size="small"
                    loading={rotate.isPending}
                    onClick={() => rotate.mutate({ params: { path: { sessionId: session.id } } })}
                  >
                    {t('links.rotateYes')}
                  </Button>
                  <Button
                    variant="text"
                    color="secondary"
                    size="small"
                    onClick={() => setConfirm(false)}
                  >
                    {t('links.rotateNo')}
                  </Button>
                </>
              ) : (
                <Button
                  variant="outlined"
                  color="secondary"
                  size="small"
                  startIcon={<KeyOutlined aria-hidden />}
                  onClick={() => setConfirm(true)}
                >
                  {t('links.rotate')}
                </Button>
              )}
            </Box>
          </Box>
        )}
        {token && (
          <Typography variant="body2" sx={{ color: 'var(--color-muted)' }}>
            {t('links.captureHint')}
          </Typography>
        )}
      </Box>
      <Box
        sx={{
          display: 'grid',
          gap: 'var(--space-2xs)',
          justifyItems: 'center',
          alignContent: 'start',
        }}
      >
        <QrCode value={session.urls.viewer} size="9rem" />
        <Typography variant="body2" sx={{ color: 'var(--color-muted)' }}>
          {t('links.qr')}
        </Typography>
      </Box>
    </Box>
  )
}
