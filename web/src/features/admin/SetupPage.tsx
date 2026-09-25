// SPDX-License-Identifier: Apache-2.0
import ArrowForwardOutlined from '@mui/icons-material/ArrowForwardOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import TextField from '@mui/material/TextField'
import { useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import { ErrorAlert } from '../../components/ErrorAlert'
import { AuthFrame } from './LoginPage'
import { SetupFrame } from './SetupFrame'
import { SetupGoogle } from './SetupGoogle'
import { SetupHardware } from './SetupHardware'
import { SetupModels } from './SetupModels'
import { SetupDone, SetupSession, type CreatedSession } from './SetupSession'
import { setupSteps, type SetupStep } from './setupSteps'

/** Shortest PIN the server takes (SetupRequest.pin). */
const minPinLength = 4

/**
 * First-run wizard (P3-06): UI language and theme (the switches at the
 * top) with the admin PIN, then hardware check and benchmark, local model
 * downloads, an optional Google API key, the first session and its
 * capture link. Setting the PIN signs this browser in; every later step
 * can be skipped, and a step whose request fails says why and lets the
 * operator carry on.
 */
export function SetupPage() {
  const { t } = useTranslation('setup')
  const status = api.useQuery('get', '/api/setup')
  const me = api.useQuery('get', '/api/auth/me', undefined, {
    enabled: status.data?.adminPinSet === true,
  })
  const statusError: unknown = status.error
  const [chosen, setChosen] = useState<SetupStep>()
  const [created, setCreated] = useState<CreatedSession>()
  useEffect(() => {
    document.title = t('title')
  }, [t])

  // Where a page load lands: the PIN on a fresh install, the next step for
  // a signed-in admin, else "already set up, log in".
  let step = chosen
  if (!step && status.data) {
    if (!status.data.adminPinSet) step = 'pin'
    else if (me.data?.authenticated) step = 'hardware'
  }

  if (!step && status.data?.adminPinSet) {
    if (me.isPending) return null
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

  const go = (s: SetupStep) => setChosen(s)
  const at = (s: SetupStep) => setupSteps.indexOf(s)
  const next = (s: SetupStep) => () => go(setupSteps[at(s) + 1] ?? 'done')
  const back = (s: SetupStep) => () => go(setupSteps[at(s) - 1] ?? 'pin')

  switch (step) {
    case 'hardware':
      return <SetupHardware onNext={next('hardware')} />
    case 'models':
      return <SetupModels onBack={back('models')} onNext={next('models')} />
    case 'google':
      return <SetupGoogle onBack={back('google')} onNext={next('google')} />
    case 'session':
      return (
        <SetupSession
          onBack={back('session')}
          onCreated={(s) => {
            setCreated(s)
            go('done')
          }}
          onSkip={next('session')}
        />
      )
    case 'done':
      return <SetupDone created={created} onBack={back('done')} />
    default:
      return (
        <PinStep statusError={statusError} statusPending={status.isPending} onDone={next('pin')} />
      )
  }
}

/** Step 1: the admin PIN, under the UI language and theme switches. */
function PinStep({
  statusError,
  statusPending,
  onDone,
}: {
  statusError: unknown
  statusPending: boolean
  onDone: () => void
}) {
  const { t } = useTranslation('setup')
  const queryClient = useQueryClient()
  const [pin, setPin] = useState('')
  const [confirm, setConfirm] = useState('')
  const [touched, setTouched] = useState(false)
  const done = api.useMutation('post', '/api/setup', {
    onSuccess: async () => {
      onDone()
      await queryClient.invalidateQueries()
    },
  })

  const tooShort = pin.length < minPinLength
  const mismatch = confirm !== pin
  const submit = (e: FormEvent) => {
    e.preventDefault()
    setTouched(true)
    if (!tooShort && !mismatch) done.mutate({ body: { pin } })
  }

  return (
    <SetupFrame step="pin" heading={t('pinStep.heading')} intro={t('intro')}>
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
          disabled={statusPending}
        >
          {t('submit')}
        </Button>
      </Box>
    </SetupFrame>
  )
}
