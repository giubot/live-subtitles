// SPDX-License-Identifier: Apache-2.0
import CheckOutlined from '@mui/icons-material/CheckOutlined'
import DeleteOutlined from '@mui/icons-material/DeleteOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useQueryClient } from '@tanstack/react-query'
import { useId, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import type { Schemas } from '../../api/types'
import { ErrorAlert } from '../../components/ErrorAlert'
import { Panel } from '../../components/Panel'
import { StatusChip } from '../../components/StatusChip'

type SecretInfo = Schemas['SecretInfo']
type SecretName = Schemas['SecretName']

export interface SecretPanelProps {
  name: SecretName
  /** What the server knows about it; absent while loading or unset. */
  info?: SecretInfo
  /** Its Save button is the page's one filled action. */
  primary?: boolean
  /** Offer "Check key" (POST /api/secrets/{name}/validate). */
  validatable?: boolean
  /** Accepted length, checked before saving (the server doesn't check it). */
  length?: { min: number; max: number }
}

/**
 * A write-only secret (SEC-1): the value goes in and never comes back; the
 * page only shows the masked hint, where it's stored and whether it passed
 * its last check (SEC-5).
 */
export function SecretPanel({ name, info, primary, validatable, length }: SecretPanelProps) {
  const { t, i18n } = useTranslation('settings')
  const queryClient = useQueryClient()
  const formId = useId()
  const [value, setValue] = useState('')
  const [confirmRemove, setConfirmRemove] = useState(false)
  const [tooShort, setTooShort] = useState(false)
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['get', '/api/secrets'] })
    void queryClient.invalidateQueries({ queryKey: ['get', '/api/providers'] })
    // Settings report srt.passphraseSet.
    void queryClient.invalidateQueries({ queryKey: ['get', '/api/settings'] })
  }
  const path = { params: { path: { name } } }

  const put = api.useMutation('put', '/api/secrets/{name}', {
    onSuccess: () => {
      setValue('')
      check.reset()
      refresh()
    },
  })
  const check = api.useMutation('post', '/api/secrets/{name}/validate', { onSettled: refresh })
  const remove = api.useMutation('delete', '/api/secrets/{name}', {
    onSuccess: () => {
      setConfirmRemove(false)
      check.reset()
      put.reset()
      refresh()
    },
  })
  const error: unknown = put.error ?? check.error ?? remove.error

  const set = info?.set ?? false
  const fromEnv = info?.source === 'env'
  const busy = put.isPending || check.isPending || remove.isPending
  const valid = check.data?.valid ?? info?.valid
  // Saving a key checks it too: say why when Google rejected it.
  const rejected = check.data ? !check.data.valid : put.data?.valid === false
  const rejectedCode = check.data?.code ?? 'provider.key_invalid'
  const badLength =
    length != null && (value.trim().length < length.min || value.trim().length > length.max)

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!value.trim()) return
    setTooShort(badLength)
    if (badLength) return
    put.mutate({ ...path, body: { value: value.trim(), validate: validatable ?? false } })
  }

  const updated =
    info?.updatedAt &&
    new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' }).format(
      new Date(info.updatedAt),
    )

  return (
    <Panel
      title={t(`keys.secret.${name}.title`)}
      actions={
        set ? (
          validatable && (
            <StatusChip
              status={valid == null ? 'idle' : valid ? 'ok' : 'error'}
              label={
                valid == null ? t('keys.unchecked') : valid ? t('keys.valid') : t('keys.invalid')
              }
            />
          )
        ) : (
          <StatusChip status="idle" label={t('keys.notSet')} />
        )
      }
    >
      <Typography sx={{ color: 'var(--color-ink-2)', maxInlineSize: 'var(--measure)' }}>
        {t(`keys.secret.${name}.intro`)}
      </Typography>

      {set && (
        <Box
          component="dl"
          sx={{
            display: 'grid',
            gridTemplateColumns: 'max-content minmax(0, 1fr)',
            gap: 'var(--space-2xs) var(--space-md)',
            margin: 0,
            fontSize: 'var(--text-sm)',
            '& dt': { color: 'var(--color-muted)' },
            '& dd': { margin: 0, color: 'var(--color-ink)' },
          }}
        >
          <dt>{t('keys.current')}</dt>
          <Box component="dd" sx={{ fontFamily: 'var(--font-mono)' }}>
            {info?.hint ?? '••••'}
          </Box>
          {info?.source && (
            <>
              <dt>{t('keys.stored')}</dt>
              <dd>
                {t(`keys.source.${info.source}`)}
                {updated && ` · ${t('keys.updated', { date: updated })}`}
              </dd>
            </>
          )}
        </Box>
      )}

      {error != null && <ErrorAlert error={error} />}
      {rejected && <ErrorAlert error={{ code: rejectedCode }} />}
      {check.data?.valid && (
        <Box
          role="status"
          sx={{
            display: 'flex',
            alignItems: 'center',
            gap: 'var(--space-xs)',
            fontSize: 'var(--text-sm)',
            color: 'var(--color-ok)',
          }}
        >
          <CheckOutlined aria-hidden sx={{ fontSize: '1.1rem' }} />
          {t('keys.accepted')}
        </Box>
      )}

      {fromEnv ? (
        <Typography sx={{ fontSize: 'var(--text-sm)', color: 'var(--color-ink-2)' }}>
          {t('keys.envReadOnly')}
        </Typography>
      ) : (
        <Box component="form" id={formId} onSubmit={submit} noValidate>
          <TextField
            label={t(`keys.secret.${name}.field`)}
            type="password"
            value={value}
            onChange={(e) => {
              setValue(e.target.value)
              setTooShort(false)
            }}
            error={tooShort}
            helperText={tooShort && length ? t('invalid.passphrase', length) : t('keys.fieldHelp')}
            fullWidth
            slotProps={{
              inputLabel: { shrink: true },
              htmlInput: {
                'aria-invalid': tooShort || undefined,
                ...(length && { maxLength: length.max }),
                autoComplete: 'new-password',
                spellCheck: false,
                sx: { fontFamily: 'var(--font-mono)' },
              },
            }}
          />
        </Box>
      )}

      <Box sx={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 'var(--space-xs)' }}>
        {!fromEnv && (
          <Button
            type="submit"
            form={formId}
            variant={primary ? 'contained' : 'outlined'}
            color={primary ? 'primary' : 'secondary'}
            loading={put.isPending}
            disabled={busy || !value.trim()}
          >
            {t(`keys.secret.${name}.save`)}
          </Button>
        )}
        {validatable && set && (
          <Button
            variant="outlined"
            color="secondary"
            loading={check.isPending}
            disabled={busy}
            onClick={() => check.mutate(path)}
          >
            {t('keys.validate')}
          </Button>
        )}
        {set &&
          !fromEnv &&
          (confirmRemove ? (
            <Box
              sx={{
                display: 'flex',
                flexWrap: 'wrap',
                alignItems: 'center',
                gap: 'var(--space-xs)',
                marginInlineStart: 'auto',
              }}
            >
              <Box
                component="span"
                sx={{ fontSize: 'var(--text-sm)', color: 'var(--color-danger)' }}
              >
                {t(`keys.secret.${name}.removeConfirm`)}
              </Box>
              <Button
                variant="outlined"
                color="error"
                loading={remove.isPending}
                onClick={() => remove.mutate(path)}
              >
                {t('keys.removeYes')}
              </Button>
              <Button variant="text" color="secondary" onClick={() => setConfirmRemove(false)}>
                {t('keys.removeNo')}
              </Button>
            </Box>
          ) : (
            <Button
              variant="outlined"
              color="error"
              startIcon={<DeleteOutlined aria-hidden />}
              disabled={busy}
              onClick={() => setConfirmRemove(true)}
              sx={{ marginInlineStart: 'auto' }}
            >
              {t('keys.remove')}
            </Button>
          ))}
      </Box>
    </Panel>
  )
}
