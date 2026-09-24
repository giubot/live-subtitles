// SPDX-License-Identifier: Apache-2.0
import MicOutlined from '@mui/icons-material/MicOutlined'
import StopOutlined from '@mui/icons-material/StopOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Container from '@mui/material/Container'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { api, errorCode } from '../../api/client'
import { ErrorAlert } from '../../components/ErrorAlert'
import { LevelMeter } from '../../components/LevelMeter'
import { Notice } from '../../components/Notice'
import { Panel } from '../../components/Panel'
import { StatusChip, type ChipStatus } from '../../components/StatusChip'
import { ThemeToggle } from '../../components/ThemeToggle'
import { UiLanguageSwitcher } from '../../components/UiLanguageSwitcher'
import type { WebSocketFactory } from '../../realtime/socket'
import { captureSupported } from './microphone'
import { useCapture, type CaptureStatus } from './useCapture'
import { useWakeLock } from './useWakeLock'

const chip: Record<CaptureStatus, ChipStatus> = {
  off: 'idle',
  closed: 'idle',
  opening: 'starting',
  connecting: 'starting',
  ready: 'ok',
  reconnecting: 'warn',
  replaced: 'error',
}

/** How often the session's state is refreshed while the page is open. */
const sessionPollMs = 5000

export interface CapturePageProps {
  sessionId: string
  /** The session's ingest token, from the capture URL (`?token=`). */
  token?: string
  WebSocket?: WebSocketFactory
}

/**
 * Capture station (AUD-1, AUD-2): answers one question, whether audio is
 * reaching the server. One input picker, one big meter, one big action and
 * plain-language warnings.
 */
export function CapturePage({ sessionId, token, WebSocket }: CapturePageProps) {
  const { t, i18n } = useTranslation('capture')
  const session = api.useQuery(
    'get',
    '/api/public/sessions/{sessionId}',
    { params: { path: { sessionId } } },
    { refetchInterval: sessionPollMs },
  )
  const cap = useCapture({ sessionId, token: token ?? '', WebSocket })
  const sending =
    cap.status === 'connecting' || cap.status === 'ready' || cap.status === 'reconnecting'
  useWakeLock(sending)

  const supported = captureSupported()
  const notFound = errorCode(session.error) === 'session.not_found'
  const name = session.data?.name ?? sessionId
  const state = session.data?.state
  const title = t('title', { id: name })
  useEffect(() => {
    document.title = title
  }, [title])

  const readout =
    sending && Number.isFinite(cap.level)
      ? t('meter.readout', {
          value: new Intl.NumberFormat(i18n.resolvedLanguage, { maximumFractionDigits: 0 })
            .format(Math.round(cap.level))
            .replace('-', '−'),
        })
      : t('meter.silent')

  const devices = cap.devices.filter((d) => d.label)
  const inputHelp = devices.length > 0 ? t('device.help') : t('device.locked')

  return (
    <Container component="main" maxWidth="sm" sx={{ paddingBlock: 'var(--space-lg)' }}>
      <Box
        sx={{
          display: 'flex',
          flexWrap: 'wrap',
          justifyContent: 'flex-end',
          gap: 'var(--space-xs)',
          marginBlockEnd: 'var(--space-md)',
        }}
      >
        <UiLanguageSwitcher />
        <ThemeToggle />
      </Box>

      <Panel sx={{ padding: 'var(--space-lg)', gap: 'var(--space-lg)' }}>
        <Box
          sx={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 'var(--space-sm)' }}
        >
          <Typography variant="h2" component="h1" sx={{ marginInlineEnd: 'auto' }}>
            {name}
          </Typography>
          <StatusChip status={chip[cap.status]} label={t(`status.${cap.status}`)} />
        </Box>

        {notFound ? (
          <ErrorAlert error={session.error} />
        ) : (
          <>
            {state && (
              <Typography
                variant="body2"
                sx={{
                  color: 'var(--color-neutral)',
                  marginBlockStart: 'calc(-1 * var(--space-sm))',
                }}
              >
                {t(`session.${state}`)}
              </Typography>
            )}

            {!supported && <ErrorAlert error={{ code: 'capture.insecure', message: '' }} />}
            {supported && !token && <Notice>{t('notice.noToken')}</Notice>}

            <TextField
              select
              label={t('device.label')}
              value={devices.some((d) => d.deviceId === cap.deviceId) ? cap.deviceId : ''}
              onChange={(e) => cap.setDevice(e.target.value)}
              helperText={inputHelp}
              disabled={!supported || devices.length === 0}
              slotProps={{ select: { native: true }, inputLabel: { shrink: true } }}
            >
              {devices.length === 0 && <option value="">{t('device.default')}</option>}
              {devices.map((d) => (
                <option key={d.deviceId} value={d.deviceId}>
                  {d.label}
                </option>
              ))}
            </TextField>

            <Box sx={{ display: 'grid', gap: 'var(--space-xs)' }}>
              <Box
                sx={{
                  display: 'flex',
                  flexWrap: 'wrap',
                  justifyContent: 'space-between',
                  gap: 'var(--space-sm)',
                }}
              >
                <Typography variant="overline" component="span">
                  {t('meter.label')}
                </Typography>
                <Box
                  component="span"
                  sx={{
                    fontFamily: 'var(--font-mono)',
                    fontSize: 'var(--text-sm)',
                    fontVariantNumeric: 'tabular-nums',
                    color: 'var(--color-ink)',
                  }}
                >
                  {readout}
                </Box>
              </Box>
              <LevelMeter db={sending ? cap.level : -Infinity} size="lg" />
            </Box>

            {cap.error && <ErrorAlert error={cap.error} />}
            {cap.status === 'ready' && cap.server?.clipping && (
              <Notice>{t('notice.clipping')}</Notice>
            )}
            {cap.status === 'ready' && cap.server?.silent && !cap.server.clipping && (
              <Notice>{t('notice.silent')}</Notice>
            )}
            {cap.status === 'reconnecting' && <Notice>{t('notice.reconnecting')}</Notice>}
            {cap.status === 'replaced' && <Notice>{t('notice.replaced')}</Notice>}

            <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-sm)' }}>
              {sending ? (
                <Button
                  variant="outlined"
                  color="error"
                  size="large"
                  startIcon={<StopOutlined aria-hidden />}
                  onClick={cap.stop}
                  sx={bigButton}
                >
                  {t('action.stop')}
                </Button>
              ) : (
                <Button
                  variant="contained"
                  size="large"
                  startIcon={<MicOutlined aria-hidden />}
                  onClick={() => {
                    if (cap.status === 'replaced') cap.stop()
                    void cap.start()
                  }}
                  loading={cap.status === 'opening'}
                  loadingPosition="start"
                  disabled={!supported || !token}
                  sx={bigButton}
                >
                  {cap.status === 'replaced' ? t('action.takeOver') : t('action.start')}
                </Button>
              )}
            </Box>
            <Typography variant="body2" sx={{ color: 'var(--color-muted)' }}>
              {t('hint')}
            </Typography>
          </>
        )}
      </Panel>
    </Container>
  )
}

const bigButton = {
  minHeight: '3.5rem',
  paddingInline: 'var(--space-xl)',
  fontSize: 'var(--text-md)',
}
