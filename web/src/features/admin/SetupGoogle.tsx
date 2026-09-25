// SPDX-License-Identifier: Apache-2.0
import KeyOutlined from '@mui/icons-material/KeyOutlined'
import RefreshOutlined from '@mui/icons-material/RefreshOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import { ErrorAlert } from '../../components/ErrorAlert'
import { Notice } from '../../components/Notice'
import { StatusChip } from '../../components/StatusChip'
import { SetupFrame, StepActions } from './SetupFrame'

const keyName = 'google_api_key'

/**
 * Step 4 (AI-11): an optional Google API key. The server checks it with
 * Google on save; a valid key makes Gemini the default provider. An
 * invalid key is still stored, and the default stays local.
 */
export function SetupGoogle({ onBack, onNext }: { onBack: () => void; onNext: () => void }) {
  const { t } = useTranslation('setup')
  const queryClient = useQueryClient()
  const providers = api.useQuery('get', '/api/providers')
  const secrets = api.useQuery('get', '/api/secrets')
  const [key, setKey] = useState('')
  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ['get', '/api/providers'] })
    await queryClient.invalidateQueries({ queryKey: ['get', '/api/secrets'] })
    await queryClient.invalidateQueries({ queryKey: ['get', '/api/setup'] })
  }
  const save = api.useMutation('put', '/api/secrets/{name}', {
    onSuccess: async () => {
      setKey('')
      await refresh()
    },
  })
  const check = api.useMutation('post', '/api/secrets/{name}/validate', { onSettled: refresh })
  const providersError: unknown = providers.error
  const secretsError: unknown = secrets.error

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const value = key.trim()
    if (!value) return
    check.reset()
    save.mutate({ params: { path: { name: keyName } }, body: { value, validate: true } })
  }
  const info = save.data ?? secrets.data?.find((s) => s.name === keyName)
  const saved = info?.set ? info : undefined
  const fromEnv = saved?.source === 'env'
  // A check that couldn't reach Google answers valid: false with
  // provider.key_unverified: that's "unknown", not "rejected".
  const valid = check.data
    ? check.data.code === 'provider.key_unverified'
      ? null
      : check.data.valid
    : saved?.valid
  const def = providers.data
  const gemini = def?.providers.find((p) => p.kind === 'gemini')
  const local = def?.providers.find((p) => p.kind === 'local')

  return (
    <SetupFrame step="google" heading={t('google.heading')} intro={t('google.intro')} canLeave>
      {providersError != null && <ErrorAlert error={providersError} />}
      {secretsError != null && <ErrorAlert error={secretsError} />}
      {def && (
        <Box sx={{ display: 'grid', gap: 'var(--space-2xs)' }}>
          <Typography variant="overline" component="h3">
            {t('google.defaultLabel')}
          </Typography>
          <Box
            sx={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 'var(--space-xs)' }}
          >
            <StatusChip status="ok" noDot label={t(`google.provider.${def.defaultProvider}`)} />
            <Typography variant="body2" sx={{ color: 'var(--color-neutral)' }}>
              {def.defaultReason
                ? t(`google.reason.${def.defaultReason}`)
                : gemini?.reasonCode === 'provider.key_unverified'
                  ? t('google.reason.unverified')
                  : null}
            </Typography>
          </Box>
          {def.defaultProvider === 'local' && local && !local.available && (
            <Notice>{t('google.localNotReady')}</Notice>
          )}
        </Box>
      )}
      {saved && (
        <Box sx={{ display: 'grid', gap: 'var(--space-xs)' }}>
          <Box
            sx={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 'var(--space-xs)' }}
          >
            <StatusChip
              status={valid === true ? 'ok' : valid === false ? 'error' : 'warn'}
              label={
                valid === true
                  ? t('google.valid')
                  : valid === false
                    ? t('google.invalid')
                    : t('google.unchecked')
              }
            />
            <Typography variant="body2" sx={{ color: 'var(--color-neutral)' }}>
              {fromEnv
                ? t('google.fromEnv')
                : saved.hint
                  ? t('google.savedHint', { hint: saved.hint })
                  : t('google.saved')}
            </Typography>
            <Button
              variant="text"
              color="secondary"
              size="small"
              startIcon={<RefreshOutlined aria-hidden />}
              loading={check.isPending}
              loadingPosition="start"
              onClick={() => check.mutate({ params: { path: { name: keyName } } })}
            >
              {t('google.check')}
            </Button>
          </Box>
          {check.error && <ErrorAlert error={check.error} />}
          {valid === false && <ErrorAlert error={{ code: 'provider.key_invalid' }} />}
          {valid == null && !check.isPending && <Notice>{t('google.uncheckedHelp')}</Notice>}
        </Box>
      )}
      {!fromEnv && (
        <Box
          component="form"
          onSubmit={submit}
          noValidate
          sx={{ display: 'grid', gap: 'var(--space-xs)' }}
        >
          {save.error && <ErrorAlert error={save.error} />}
          <TextField
            label={saved ? t('google.replaceKey') : t('google.key')}
            type="password"
            autoComplete="off"
            value={key}
            onChange={(e) => setKey(e.target.value)}
            helperText={t('google.keyHelp')}
            slotProps={{ inputLabel: { shrink: true } }}
          />
          <Button
            type="submit"
            variant="outlined"
            color="secondary"
            startIcon={<KeyOutlined aria-hidden />}
            loading={save.isPending}
            loadingPosition="start"
            disabled={!key.trim()}
            sx={{ justifySelf: 'start' }}
          >
            {t('google.save')}
          </Button>
        </Box>
      )}
      <StepActions onBack={onBack} onNext={onNext} nextLabel={saved ? undefined : t('skip')} />
    </SetupFrame>
  )
}
