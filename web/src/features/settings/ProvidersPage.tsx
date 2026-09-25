// SPDX-License-Identifier: Apache-2.0
import CheckCircleOutlined from '@mui/icons-material/CheckCircleOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Typography from '@mui/material/Typography'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import type { Schemas } from '../../api/types'
import { describeError } from '../../components/apiError'
import { ErrorAlert } from '../../components/ErrorAlert'
import { Notice } from '../../components/Notice'
import { Panel } from '../../components/Panel'
import { StatusChip } from '../../components/StatusChip'
import { AdminPage } from '../admin/AdminLayout'
import { SecretPanel } from './SecretPanel'

type ProvidersResponse = Schemas['ProvidersResponse']
type ProviderInfo = Schemas['ProviderInfo']

/**
 * `/admin/providers`: which provider new sessions use, and the write-only
 * secrets behind it (SEC-1, SEC-5, AI-11).
 */
export function ProvidersPage() {
  const { t } = useTranslation('settings')
  const { t: ta } = useTranslation('admin')
  const providers = api.useQuery('get', '/api/providers')
  const secrets = api.useQuery('get', '/api/secrets')
  const providersError: unknown = providers.error
  const secretsError: unknown = secrets.error
  const secret = (name: Schemas['SecretName']) => secrets.data?.find((s) => s.name === name)

  return (
    <AdminPage title={ta('nav.providers')}>
      <Box sx={{ display: 'grid', gap: 'var(--space-md)', maxInlineSize: '64rem' }}>
        {providersError != null && (
          <ErrorAlert
            error={providersError}
            action={
              <Button variant="outlined" color="secondary" onClick={() => void providers.refetch()}>
                {t('retry')}
              </Button>
            }
          />
        )}
        {providers.data && <DefaultBanner data={providers.data} />}
        {secretsError != null && <ErrorAlert error={secretsError} />}
        <SecretPanel name="google_api_key" info={secret('google_api_key')} primary validatable />
        {providers.data && providers.data.providers.length > 0 && (
          <Panel title={t('keys.providersTitle')}>
            <Box
              component="ul"
              sx={{ display: 'grid', gap: 'var(--space-sm)', margin: 0, padding: 0 }}
            >
              {providers.data.providers.map((p) => (
                <ProviderRow key={p.kind} info={p} />
              ))}
            </Box>
          </Panel>
        )}
        <SecretPanel name="obs_websocket_password" info={secret('obs_websocket_password')} />
      </Box>
    </AdminPage>
  )
}

/** "Using Gemini" or "Using local: add a Google API key to use Gemini" (AI-11). */
function DefaultBanner({ data }: { data: ProvidersResponse }) {
  const { t } = useTranslation('settings')
  if (data.defaultProvider === 'gemini')
    return (
      <Box
        role="status"
        sx={{
          display: 'flex',
          alignItems: 'flex-start',
          gap: 'var(--space-xs)',
          padding: 'var(--space-sm)',
          borderRadius: 'var(--radius-input)',
          border: 'var(--rule-hair) solid var(--color-ok)',
          color: 'var(--color-ink-2)',
          fontSize: 'var(--text-sm)',
          lineHeight: 'var(--leading-body)',
        }}
      >
        <CheckCircleOutlined
          aria-hidden
          sx={{
            fontSize: '1.1rem',
            color: 'var(--color-ok)',
            marginBlockStart: 'var(--space-3xs)',
          }}
        />
        <span>
          <Box component="strong" sx={{ color: 'var(--color-ink)' }}>
            {t('keys.banner.gemini')}
          </Box>{' '}
          {t('keys.banner.geminiBody')}
        </span>
      </Box>
    )
  // No defaultReason: a key is set but Google couldn't be reached to check it.
  const [title, body] =
    data.defaultProvider === 'mock'
      ? [t('keys.banner.mock'), t('keys.banner.mockBody')]
      : data.defaultReason === 'google_api_key_invalid'
        ? [t('keys.banner.localInvalid'), t('keys.banner.localBody')]
        : data.defaultReason == null
          ? [t('keys.banner.localUnverified'), t('keys.banner.localUnverifiedBody')]
          : [t('keys.banner.local'), t('keys.banner.localBody')]
  return (
    <Notice>
      <Box component="strong" sx={{ color: 'var(--color-ink)' }}>
        {title}
      </Box>{' '}
      {body}
    </Notice>
  )
}

function ProviderRow({ info }: { info: ProviderInfo }) {
  const { t, i18n } = useTranslation('settings')
  const reason = info.reasonCode ? describeError(i18n, { code: info.reasonCode }) : undefined
  return (
    <Box
      component="li"
      sx={{
        listStyle: 'none',
        display: 'flex',
        flexWrap: 'wrap',
        alignItems: 'center',
        gap: 'var(--space-xs) var(--space-sm)',
      }}
    >
      <Typography
        component="span"
        sx={{ fontWeight: 'var(--weight-strong)', marginInlineEnd: 'auto' }}
      >
        {t(`keys.provider.${info.kind}`)}
      </Typography>
      {info.realtimeCapable != null && (
        <StatusChip
          status={info.realtimeCapable ? 'ok' : 'warn'}
          label={info.realtimeCapable ? t('keys.realtime') : t('keys.notRealtime')}
        />
      )}
      <StatusChip
        status={info.available ? 'ok' : 'idle'}
        label={info.available ? t('keys.available') : t('keys.unavailable')}
      />
      {!info.available && reason && (
        <Box
          component="span"
          sx={{ flexBasis: '100%', fontSize: 'var(--text-sm)', color: 'var(--color-ink-2)' }}
        >
          {reason.known ? `${reason.title}. ${reason.fix}` : reason.code}
        </Box>
      )}
    </Box>
  )
}
