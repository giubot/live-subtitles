// SPDX-License-Identifier: Apache-2.0
import LoginOutlined from '@mui/icons-material/LoginOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import { ErrorAlert } from '../../components/ErrorAlert'
import { Panel } from '../../components/Panel'
import { ThemeToggle } from '../../components/ThemeToggle'
import { UiLanguageSwitcher } from '../../components/UiLanguageSwitcher'
import { Wordmark } from './AdminLayout'

/** Narrow centred card for the login and setup forms. */
export function AuthFrame({
  title,
  intro,
  children,
}: {
  title: string
  intro: ReactNode
  children: ReactNode
}) {
  return (
    <Box
      component="main"
      sx={{
        maxInlineSize: '26rem',
        marginInline: 'auto',
        paddingBlock: 'var(--space-xl)',
        paddingInline: 'var(--space-md)',
      }}
    >
      <Box
        sx={{
          display: 'flex',
          flexWrap: 'wrap',
          justifyContent: 'space-between',
          alignItems: 'center',
          gap: 'var(--space-sm)',
          marginBlockEnd: 'var(--space-lg)',
        }}
      >
        <Wordmark />
        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-xs)' }}>
          <UiLanguageSwitcher />
          <ThemeToggle />
        </Box>
      </Box>
      <Panel sx={{ padding: 'var(--space-lg)' }}>
        <Box sx={{ display: 'grid', gap: 'var(--space-2xs)' }}>
          <Typography variant="h2" component="h1">
            {title}
          </Typography>
          <Typography sx={{ color: 'var(--color-neutral)' }}>{intro}</Typography>
        </Box>
        {children}
      </Panel>
    </Box>
  )
}

/** Admin login with the PIN chosen at setup (ADM-3). */
export function LoginPage() {
  const { t } = useTranslation('admin')
  const queryClient = useQueryClient()
  const [pin, setPin] = useState('')
  const login = api.useMutation('post', '/api/auth/login', {
    onSuccess: () => queryClient.invalidateQueries(),
  })

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (pin) login.mutate({ body: { pin } })
  }

  return (
    <AuthFrame title={t('login.title')} intro={t('login.intro')}>
      <Box
        component="form"
        onSubmit={submit}
        noValidate
        sx={{ display: 'grid', gap: 'var(--space-md)' }}
      >
        {login.error && <ErrorAlert error={login.error} />}
        <TextField
          label={t('login.pin')}
          type="password"
          autoComplete="current-password"
          autoFocus
          value={pin}
          onChange={(e) => setPin(e.target.value)}
          helperText=" "
          slotProps={{ inputLabel: { shrink: true } }}
        />
        <Button
          type="submit"
          variant="contained"
          size="large"
          startIcon={<LoginOutlined aria-hidden />}
          loading={login.isPending}
          loadingPosition="start"
          disabled={!pin}
        >
          {t('login.submit')}
        </Button>
      </Box>
    </AuthFrame>
  )
}
