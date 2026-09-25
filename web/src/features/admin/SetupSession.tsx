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

/**
 * Step 5: the first session. Only its name and address are sent, so the
 * server fills in the settings defaults (languages, glossary, recording)
 * and the default provider, as for any new session.
 */
export function SetupSession({ onBack, onCreated, onSkip }: SetupSessionProps) {
  const { t } = useTranslation('setup')
  const queryClient = useQueryClient()
  const settings = api.useQuery('get', '/api/settings')
  const providers = api.useQuery('get', '/api/providers')
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
  // The server's verdict on the fields, e.g. a slug that's taken.
  const serverFields = create.error && 'fields' in create.error ? (create.error.fields ?? {}) : {}
  const slugTaken = create.error?.code === 'session.slug_taken'
  const slugBad = !!bad.slug || !!serverFields.slug || slugTaken
  const nameBad = !!bad.name || !!serverFields.name
  const languages = settings.data?.defaultTargetLanguages ?? newSessionValues.targetLanguages
  const provider = providers.data?.defaultProvider

  const submit = (e: FormEvent) => {
    e.preventDefault()
    setTouched(true)
    if (Object.keys(validate(v, true)).length > 0) return
    create.mutate({ body: { name: v.name.trim(), slug: v.slug } })
  }

  return (
    <SetupFrame
      step="session"
      heading={t('session.heading')}
      intro={
        provider
          ? t('session.introProvider', {
              languages: languages.map(nativeLanguageName).join(', '),
              provider: t(`google.provider.${provider}`),
            })
          : t('session.intro', { languages: languages.map(nativeLanguageName).join(', ') })
      }
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
          error={nameBad}
          helperText={nameBad ? t('session.nameRequired') : t('session.nameHelp')}
          slotProps={{
            inputLabel: { shrink: true },
            htmlInput: { 'aria-invalid': nameBad || undefined },
          }}
        />
        <TextField
          label={t('session.slug')}
          value={v.slug}
          onChange={(e) => {
            setSlugEdited(true)
            if (slugTaken) create.reset()
            setV((prev) => ({ ...prev, slug: e.target.value }))
          }}
          error={slugBad}
          helperText={
            slugTaken
              ? t('session.slugTaken')
              : slugBad
                ? t('session.slugInvalid')
                : t('session.slugHelp')
          }
          slotProps={{
            inputLabel: { shrink: true },
            htmlInput: { 'aria-invalid': slugBad || undefined, spellCheck: false },
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
            <Typography variant="body2" sx={{ color: 'var(--color-neutral)' }}>
              {t('finish.provider', {
                provider: t(`google.provider.${created.session.effectiveProvider}`),
              })}
            </Typography>
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
