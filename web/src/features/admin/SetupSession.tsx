// SPDX-License-Identifier: Apache-2.0
import ArrowForwardOutlined from '@mui/icons-material/ArrowForwardOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import { CopyField } from '../../components/CopyField'
import { ErrorAlert } from '../../components/ErrorAlert'
import { QrCode } from '../../components/QrCode'
import { nativeLanguageName } from '../../components/languageNames'
import { SetupFrame } from './SetupFrame'
import {
  captureBase,
  captureUrl,
  newSessionValues,
  slugify,
  toBody,
  validate,
  type Session,
} from './sessionForm'

export interface CreatedSession {
  session: Session
  token: string
}

export interface SetupSessionProps {
  onBack: () => void
  onCreated: (created: CreatedSession) => void
  onSkip: () => void
}

/** Step 6: the first session, with the default languages and provider. */
export function SetupSession({ onBack, onCreated, onSkip }: SetupSessionProps) {
  const { t } = useTranslation('setup')
  const queryClient = useQueryClient()
  const [v, setV] = useState(newSessionValues)
  const [slugEdited, setSlugEdited] = useState(false)
  const [touched, setTouched] = useState(false)
  const create = api.useMutation('post', '/api/sessions', {
    onSuccess: async (data) => {
      const { ingestToken, ...session } = data
      onCreated({ session, token: ingestToken })
      await queryClient.invalidateQueries({ queryKey: ['get', '/api/sessions'] })
    },
  })
  const bad = touched ? validate(v, true) : {}

  const submit = (e: FormEvent) => {
    e.preventDefault()
    setTouched(true)
    if (Object.keys(validate(v, true)).length > 0) return
    create.mutate({ body: { ...toBody(v), slug: v.slug } })
  }

  return (
    <SetupFrame
      step="session"
      heading={t('session.heading')}
      intro={t('session.intro', {
        languages: v.targetLanguages.map(nativeLanguageName).join(', '),
      })}
      canLeave
    >
      <Box
        component="form"
        onSubmit={submit}
        noValidate
        sx={{ display: 'grid', gap: 'var(--space-sm)' }}
      >
        {create.error && <ErrorAlert error={create.error} />}
        <TextField
          label={t('session.name')}
          value={v.name}
          autoFocus
          onChange={(e) => {
            const name = e.target.value
            setV((prev) => ({ ...prev, name, slug: slugEdited ? prev.slug : slugify(name) }))
          }}
          error={!!bad.name}
          helperText={bad.name ? t('session.nameRequired') : t('session.nameHelp')}
          slotProps={{
            inputLabel: { shrink: true },
            htmlInput: { 'aria-invalid': !!bad.name || undefined },
          }}
        />
        <TextField
          label={t('session.slug')}
          value={v.slug}
          onChange={(e) => {
            setSlugEdited(true)
            setV((prev) => ({ ...prev, slug: e.target.value }))
          }}
          error={!!bad.slug}
          helperText={bad.slug ? t('session.slugInvalid') : t('session.slugHelp')}
          slotProps={{
            inputLabel: { shrink: true },
            htmlInput: { 'aria-invalid': !!bad.slug || undefined, spellCheck: false },
          }}
        />
        <Box
          sx={{
            display: 'flex',
            flexWrap: 'wrap',
            justifyContent: 'space-between',
            alignItems: 'center',
            gap: 'var(--space-xs)',
            paddingBlockStart: 'var(--space-sm)',
            borderBlockStart: 'var(--rule-hair) solid var(--color-rule)',
          }}
        >
          <Button variant="text" color="secondary" onClick={onBack}>
            {t('back')}
          </Button>
          <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-xs)' }}>
            <Button variant="text" color="secondary" onClick={onSkip}>
              {t('skip')}
            </Button>
            <Button type="submit" variant="contained" size="large" loading={create.isPending}>
              {t('session.create')}
            </Button>
          </Box>
        </Box>
      </Box>
    </SetupFrame>
  )
}

/** Last step: where to open the capture page, or straight to the admin. */
export function SetupDone({ created, onBack }: { created?: CreatedSession; onBack: () => void }) {
  const { t } = useTranslation('setup')
  const link = created ? captureUrl(captureBase(created.session), created.token) : undefined

  return (
    <SetupFrame
      step="done"
      heading={t('finish.heading')}
      intro={created ? t('finish.intro', { name: created.session.name }) : t('finish.noSession')}
    >
      {created && link && (
        <Box
          sx={{
            display: 'grid',
            gap: 'var(--space-md)',
            '@media (min-width: 40rem)': { gridTemplateColumns: 'minmax(0, 1fr) auto' },
          }}
        >
          <Box sx={{ display: 'grid', gap: 'var(--space-sm)', minInlineSize: 0 }}>
            <CopyField label={t('finish.capture')} value={link} />
            <Typography variant="body2" sx={{ color: 'var(--color-muted)' }}>
              {t('finish.captureHint')}
            </Typography>
            <CopyField label={t('finish.viewer')} value={created.session.urls.viewer} />
          </Box>
          <Box
            sx={{
              display: 'grid',
              gap: 'var(--space-2xs)',
              justifyItems: 'center',
              alignContent: 'start',
            }}
          >
            <QrCode value={link} size="9rem" />
            <Typography variant="body2" sx={{ color: 'var(--color-muted)' }}>
              {t('finish.qr')}
            </Typography>
          </Box>
        </Box>
      )}
      <Box
        sx={{
          display: 'flex',
          flexWrap: 'wrap',
          justifyContent: 'space-between',
          alignItems: 'center',
          gap: 'var(--space-xs)',
          paddingBlockStart: 'var(--space-sm)',
          borderBlockStart: 'var(--rule-hair) solid var(--color-rule)',
        }}
      >
        {created ? (
          <span />
        ) : (
          <Button variant="text" color="secondary" onClick={onBack}>
            {t('back')}
          </Button>
        )}
        <Button
          component={Link}
          to="/admin"
          variant="contained"
          size="large"
          endIcon={<ArrowForwardOutlined aria-hidden />}
        >
          {t('toAdmin')}
        </Button>
      </Box>
    </SetupFrame>
  )
}
