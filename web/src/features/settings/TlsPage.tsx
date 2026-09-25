// SPDX-License-Identifier: Apache-2.0
import DownloadOutlined from '@mui/icons-material/DownloadOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import Typography from '@mui/material/Typography'
import { useId, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import type { Schemas } from '../../api/types'
import { CopyField } from '../../components/CopyField'
import { ErrorAlert } from '../../components/ErrorAlert'
import { Notice } from '../../components/Notice'
import { Panel } from '../../components/Panel'
import { StatusChip } from '../../components/StatusChip'
import { segmentedSx } from '../../components/segmented'
import { AdminPage } from '../admin/AdminLayout'

type TlsInfo = Schemas['TlsInfo']

const systems = ['macos', 'windows', 'ios', 'android', 'linux'] as const
type Os = (typeof systems)[number]

/** Where the CA certificate is served (api/openapi.yaml downloadCaCert). */
const caCertUrl = '/api/tls/ca.crt'

/** A best guess at this device's OS, so its steps show first. */
function detectOs(): Os {
  const ua = globalThis.navigator?.userAgent ?? ''
  if (/iPhone|iPad|iPod/i.test(ua)) return 'ios'
  if (/Android/i.test(ua)) return 'android'
  if (/Mac OS X|Macintosh/i.test(ua)) return 'macos'
  if (/Windows/i.test(ua)) return 'windows'
  if (/Linux|X11/i.test(ua)) return 'linux'
  return 'windows'
}

/** `/admin/tls`: the certificate in use and how to trust it on each device (TLS-4). */
export function TlsPage() {
  const { t } = useTranslation('settings')
  const { t: ta } = useTranslation('admin')
  const tls = api.useQuery('get', '/api/tls')
  const error: unknown = tls.error

  return (
    <AdminPage title={ta('nav.tls')}>
      <Box sx={{ display: 'grid', gap: 'var(--space-md)', maxInlineSize: '64rem' }}>
        {error != null && (
          <ErrorAlert
            error={error}
            action={
              <Button variant="outlined" color="secondary" onClick={() => void tls.refetch()}>
                {t('retry')}
              </Button>
            }
          />
        )}
        {tls.data && <Certificate info={tls.data} />}
        {/* The guide is useful even when the server can't describe its certificate. */}
        {(error != null || (tls.data && tls.data.mode !== 'disabled')) && (
          <InstallGuide localCa={!tls.data || tls.data.mode === 'local-ca'} />
        )}
      </Box>
    </AdminPage>
  )
}

function Certificate({ info }: { info: TlsInfo }) {
  const { t, i18n } = useTranslation('settings')
  const expires =
    info.notAfter &&
    new Intl.DateTimeFormat(i18n.language, { dateStyle: 'long' }).format(new Date(info.notAfter))

  return (
    <Panel
      title={t('tls.certTitle')}
      actions={
        <StatusChip
          status={info.enabled ? 'ok' : 'idle'}
          label={`${t('tls.status')} · ${info.enabled ? t('tls.on') : t('tls.off')}`}
        />
      }
    >
      {!info.enabled && <Notice>{t('tls.offNotice')}</Notice>}
      <Box
        component="dl"
        sx={{
          display: 'grid',
          gridTemplateColumns: 'max-content minmax(0, 1fr)',
          gap: 'var(--space-2xs) var(--space-md)',
          margin: 0,
          fontSize: 'var(--text-sm)',
          '& dt': { color: 'var(--color-muted)' },
          '& dd': { margin: 0, color: 'var(--color-ink)', overflowWrap: 'anywhere' },
        }}
      >
        <Row label={t('tls.mode')}>{t(`tls.modeOption.${info.mode}`)}</Row>
        {info.httpsPort != null && (
          <Row label={t('tls.port')}>
            <Box component="span" sx={{ fontVariantNumeric: 'tabular-nums' }}>
              {info.httpsPort}
            </Box>
          </Row>
        )}
        {info.sans && info.sans.length > 0 && (
          <Row label={t('tls.sans')}>
            <Box component="span" sx={{ fontFamily: 'var(--font-mono)' }}>
              {info.sans.join(', ')}
            </Box>
          </Row>
        )}
        {expires && <Row label={t('tls.notAfter')}>{expires}</Row>}
      </Box>
      {info.caFingerprintSha256 && (
        <CopyField label={t('tls.fingerprint')} value={info.caFingerprintSha256} />
      )}
    </Panel>
  )
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt>{label}</dt>
      <dd>{children}</dd>
    </>
  )
}

function InstallGuide({ localCa }: { localCa: boolean }) {
  const { t } = useTranslation('settings')
  const [os, setOs] = useState<Os>(detectOs)
  const labelId = useId()
  const steps = t(`tls.steps.${os}`, { returnObjects: true })

  return (
    <Panel title={t('tls.installTitle')}>
      {localCa ? (
        <>
          <Typography sx={{ color: 'var(--color-ink-2)', maxInlineSize: 'var(--measure)' }}>
            {t('tls.installIntro')}
          </Typography>
          <Box>
            <Button
              variant="contained"
              href={caCertUrl}
              download="livesubs-ca.crt"
              startIcon={<DownloadOutlined aria-hidden />}
            >
              {t('tls.download')}
            </Button>
          </Box>
          <Box
            sx={{
              display: 'grid',
              gap: 'var(--space-xs)',
              justifyItems: 'start',
              '& .MuiToggleButtonGroup-root': { flexWrap: 'wrap' },
            }}
          >
            <Typography id={labelId} variant="overline" component="span">
              {t('tls.os')}
            </Typography>
            <ToggleButtonGroup
              exclusive
              value={os}
              onChange={(_, next: Os | null) => next && setOs(next)}
              aria-labelledby={labelId}
              sx={segmentedSx}
            >
              {systems.map((s) => (
                <ToggleButton key={s} value={s}>
                  {t(`tls.osName.${s}`)}
                </ToggleButton>
              ))}
            </ToggleButtonGroup>
          </Box>
          <Box
            component="ol"
            sx={{
              display: 'grid',
              gap: 'var(--space-xs)',
              margin: 0,
              paddingInlineStart: 'var(--space-lg)',
              maxInlineSize: 'var(--measure)',
              color: 'var(--color-ink)',
            }}
          >
            {(Array.isArray(steps) ? (steps as string[]) : []).map((step) => (
              <li key={step}>{step}</li>
            ))}
          </Box>
          <Typography sx={{ fontSize: 'var(--text-sm)', color: 'var(--color-ink-2)' }}>
            {t('tls.checkFingerprint')}
          </Typography>
        </>
      ) : (
        <Typography sx={{ color: 'var(--color-ink-2)', maxInlineSize: 'var(--measure)' }}>
          {t('tls.notLocalCa')}
        </Typography>
      )}
    </Panel>
  )
}
