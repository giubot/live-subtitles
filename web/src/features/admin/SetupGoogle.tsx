// SPDX-License-Identifier: Apache-2.0
import KeyOutlined from '@mui/icons-material/KeyOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import { ErrorAlert } from '../../components/ErrorAlert'
import { StatusChip } from '../../components/StatusChip'
import { SetupFrame, StepActions } from './SetupFrame'

/** Step 5 (AI-11): an optional Google API key, which makes Gemini the default. */
export function SetupGoogle({ onBack, onNext }: { onBack: () => void; onNext: () => void }) {
  const { t } = useTranslation('setup')
  const queryClient = useQueryClient()
  const providers = api.useQuery('get', '/api/providers')
  const setup = api.useQuery('get', '/api/setup')
  const [key, setKey] = useState('')
  const save = api.useMutation('put', '/api/secrets/{name}', {
    onSuccess: async () => {
      setKey('')
      await queryClient.invalidateQueries({ queryKey: ['get', '/api/providers'] })
      await queryClient.invalidateQueries({ queryKey: ['get', '/api/setup'] })
    },
  })
  const providersError: unknown = providers.error

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const value = key.trim()
    if (value)
      save.mutate({ params: { path: { name: 'google_api_key' } }, body: { value, validate: true } })
  }
  const saved = save.data ?? (setup.data?.googleApiKeySet ? { hint: undefined } : undefined)
  const def = providers.data

  return (
    <SetupFrame step="google" heading={t('google.heading')} intro={t('google.intro')} canLeave>
      {providersError != null && <ErrorAlert error={providersError} />}
      {def && (
        <Box sx={{ display: 'grid', gap: 'var(--space-2xs)' }}>
          <Typography variant="overline" component="h3">
            {t('google.defaultLabel')}
          </Typography>
          <Box
            sx={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 'var(--space-xs)' }}
          >
            <StatusChip status="ok" noDot label={t(`google.provider.${def.defaultProvider}`)} />
            {def.defaultReason && (
              <Typography variant="body2" sx={{ color: 'var(--color-neutral)' }}>
                {t(`google.reason.${def.defaultReason}`)}
              </Typography>
            )}
          </Box>
        </Box>
      )}
      <Box
        component="form"
        onSubmit={submit}
        noValidate
        sx={{ display: 'grid', gap: 'var(--space-xs)' }}
      >
        {save.error && <ErrorAlert error={save.error} />}
        <TextField
          label={t('google.key')}
          type="password"
          autoComplete="off"
          value={key}
          onChange={(e) => setKey(e.target.value)}
          helperText={
            saved
              ? saved.hint
                ? t('google.savedHint', { hint: saved.hint })
                : t('google.saved')
              : t('google.keyHelp')
          }
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
      <StepActions onBack={onBack} onNext={onNext} nextLabel={saved ? undefined : t('skip')} />
    </SetupFrame>
  )
}
