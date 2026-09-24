// SPDX-License-Identifier: Apache-2.0
import ArrowForwardOutlined from '@mui/icons-material/ArrowForwardOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import TextField from '@mui/material/TextField'
import { useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import { ErrorAlert } from '../../components/ErrorAlert'
import { AuthFrame } from './LoginPage'

/** Shortest PIN the server takes (SetupRequest.pin). */
const minPinLength = 4

/**
 * First run: choose the admin PIN (the full wizard is P3-06). Setting it
 * also signs this browser in.
 */
export function SetupPage() {
  const { t } = useTranslation('setup')
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const status = api.useQuery('get', '/api/setup')
  const statusError: unknown = status.error
  const [pin, setPin] = useState('')
  const [confirm, setConfirm] = useState('')
  const [touched, setTouched] = useState(false)
  const done = api.useMutation('post', '/api/setup', {
    onSuccess: async () => {
      await queryClient.invalidateQueries()
      await navigate({ to: '/admin' })
    },
  })
  useEffect(() => {
    document.title = t('title')
  }, [t])

  const tooShort = pin.length < minPinLength
  const mismatch = confirm !== pin
  const submit = (e: FormEvent) => {
    e.preventDefault()
    setTouched(true)
    if (!tooShort && !mismatch) done.mutate({ body: { pin } })
  }

  if (status.data?.adminPinSet) {
    return (
      <AuthFrame title={t('title')} intro={t('done')}>
        <Button
          component={Link}
          to="/admin"
          variant="contained"
          endIcon={<ArrowForwardOutlined aria-hidden />}
        >
          {t('toAdmin')}
        </Button>
      </AuthFrame>
    )
  }

  return (
    <AuthFrame title={t('title')} intro={t('intro')}>
      {statusError != null && <ErrorAlert error={statusError} />}
      <Box
        component="form"
        onSubmit={submit}
        noValidate
        sx={{ display: 'grid', gap: 'var(--space-sm)' }}
      >
        {done.error && <ErrorAlert error={done.error} />}
        <TextField
          label={t('pin')}
          type="password"
          autoComplete="new-password"
          autoFocus
          value={pin}
          onChange={(e) => setPin(e.target.value)}
          error={touched && tooShort}
          helperText={
            touched && tooShort
              ? t('pinTooShort', { min: minPinLength })
              : t('pinHelp', { min: minPinLength })
          }
          slotProps={{
            inputLabel: { shrink: true },
            htmlInput: { 'aria-invalid': (touched && tooShort) || undefined },
          }}
        />
        <TextField
          label={t('confirm')}
          type="password"
          autoComplete="new-password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          error={touched && !tooShort && mismatch}
          helperText={touched && !tooShort && mismatch ? t('mismatch') : ' '}
          slotProps={{
            inputLabel: { shrink: true },
            htmlInput: { 'aria-invalid': (touched && !tooShort && mismatch) || undefined },
          }}
        />
        <Button
          type="submit"
          variant="contained"
          size="large"
          loading={done.isPending}
          disabled={status.isPending}
        >
          {t('submit')}
        </Button>
      </Box>
    </AuthFrame>
  )
}
