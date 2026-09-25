// SPDX-License-Identifier: Apache-2.0
import DeleteOutlined from '@mui/icons-material/DeleteOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import FormControl from '@mui/material/FormControl'
import FormControlLabel from '@mui/material/FormControlLabel'
import FormGroup from '@mui/material/FormGroup'
import FormHelperText from '@mui/material/FormHelperText'
import FormLabel from '@mui/material/FormLabel'
import Switch from '@mui/material/Switch'
import TextField from '@mui/material/TextField'
import { useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import { useLanguages } from '../../api/languages'
import { toApiError } from '../../components/apiError'
import { ErrorAlert } from '../../components/ErrorAlert'
import { nativeLanguageName } from '../../components/languageNames'
import { StreamCaptionsFields } from './StreamCaptionsFields'
import { liveSources, useSessionSources, useSrtAvailability } from './sessionSource'
import {
  newSessionValues,
  providers,
  slugify,
  toBody,
  validate,
  valuesFrom,
  type Session,
  type SessionFormValues,
} from './sessionForm'

const invalidKey = {
  name: 'form.invalid.name',
  slug: 'form.invalid.slug',
  room: 'form.invalid.room',
  targetLanguages: 'form.invalid.targetLanguages',
} as const
type Field = keyof typeof invalidKey

export interface SessionDialogProps {
  /** The session to edit; absent to create one. */
  session?: Session
  onClose: () => void
  /** A new session, with the ingest token only shown now. */
  onCreated?: (id: string, ingestToken: string) => void
}

/**
 * Create or edit a session (SES-1). Name, room, recording and styles can
 * change any time; languages and provider only while it isn't running,
 * which the server enforces (409). The audio input applies at the next
 * start, so it's locked while the session runs.
 */
export function SessionDialog({ session, onClose, onCreated }: SessionDialogProps) {
  const { t } = useTranslation('admin')
  const queryClient = useQueryClient()
  const creating = !session
  const setSource = useSessionSources((s) => s.setSource)
  const [v, setV] = useState<SessionFormValues>(() =>
    session
      ? valuesFrom(session, useSessionSources.getState().sources[session.id])
      : newSessionValues,
  )
  const srt = useSrtAvailability(session)
  const running = !!session && !['idle', 'error'].includes(session.state)
  const [slugEdited, setSlugEdited] = useState(false)
  const [touched, setTouched] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['get', '/api/sessions'] })

  const create = api.useMutation('post', '/api/sessions', {
    onSuccess: (data) => {
      void refresh()
      setSource(data.id, v.source)
      onCreated?.(data.id, data.ingestToken)
      onClose()
    },
  })
  const update = api.useMutation('patch', '/api/sessions/{sessionId}', {
    onSuccess: (data) => {
      void refresh()
      setSource(data.id, v.source)
      onClose()
    },
  })
  const remove = api.useMutation('delete', '/api/sessions/{sessionId}', {
    onSuccess: () => {
      void refresh()
      onClose()
    },
  })
  const putUrl = api.useMutation('put', '/api/sessions/{sessionId}/stream-captions/youtube-url')
  const glossaries = api.useQuery('get', '/api/glossaries')
  const error: unknown = create.error ?? update.error ?? remove.error ?? putUrl.error
  const pending = create.isPending || update.isPending || remove.isPending || putUrl.isPending

  const local = touched ? validate(v, creating) : {}
  const server = toApiError(error)?.fields ?? {}
  const fieldError = (f: Field) => local[f] ?? server[f]
  const help = (f: Field, fallback = ' ') => (fieldError(f) ? t(invalidKey[f]) : fallback)
  const set = <K extends keyof SessionFormValues>(k: K, value: SessionFormValues[K]) =>
    setV((prev) => ({ ...prev, [k]: value }))

  const submit = (e: FormEvent) => {
    e.preventDefault()
    setTouched(true)
    if (Object.keys(validate(v, creating)).length > 0) return
    if (creating) create.mutate({ body: { ...toBody(v), slug: v.slug } })
    else {
      const params = { path: { sessionId: session.id } }
      const save = () => update.mutate({ params, body: toBody(v) })
      const url = v.ccYoutubeUrl.trim()
      // The ingestion URL is a secret with its own write-only endpoint.
      if (v.ccEnabled && v.ccTarget === 'youtube_http' && url)
        putUrl.mutate({ params, body: { value: url, validate: true } }, { onSuccess: save })
      else save()
    }
  }

  const catalog = useLanguages()
  const languages = [...new Set([...catalog.targets, ...v.targetLanguages])]
  const sourceLanguages = catalog.sources.includes(v.sourceLanguage)
    ? catalog.sources
    : [...catalog.sources, v.sourceLanguage]
  const languageName = (l: string) => catalog.byCode.get(l)?.nativeName ?? nativeLanguageName(l)
  const toggleLanguage = (lang: string, on: boolean) =>
    set(
      'targetLanguages',
      on ? [...v.targetLanguages, lang] : v.targetLanguages.filter((l) => l !== lang),
    )

  return (
    <Dialog
      open
      onClose={pending ? undefined : onClose}
      fullWidth
      maxWidth="sm"
      aria-labelledby="session-dialog-title"
    >
      <Box component="form" onSubmit={submit} noValidate>
        <DialogTitle id="session-dialog-title">
          {creating ? t('form.createTitle') : t('form.editTitle', { name: session.name })}
        </DialogTitle>
        <DialogContent sx={{ display: 'grid', gap: 'var(--space-sm)' }}>
          {error != null && <ErrorAlert error={error} />}
          <TextField
            label={t('form.name')}
            value={v.name}
            autoFocus
            onChange={(e) => {
              const name = e.target.value
              setV((prev) => ({
                ...prev,
                name,
                slug: creating && !slugEdited ? slugify(name) : prev.slug,
              }))
            }}
            error={!!fieldError('name')}
            helperText={help('name')}
            slotProps={{
              inputLabel: { shrink: true },
              htmlInput: { 'aria-invalid': !!fieldError('name') || undefined, maxLength: 120 },
            }}
          />
          <TextField
            label={t('form.slug')}
            value={v.slug}
            disabled={!creating}
            onChange={(e) => {
              setSlugEdited(true)
              set('slug', e.target.value.toLowerCase())
            }}
            error={!!fieldError('slug')}
            helperText={help('slug', creating ? t('form.slugHelp') : t('form.slugFixed'))}
            slotProps={{
              inputLabel: { shrink: true },
              htmlInput: {
                'aria-invalid': !!fieldError('slug') || undefined,
                spellCheck: false,
                sx: { fontFamily: 'var(--font-mono)' },
              },
            }}
          />
          <TextField
            label={t('form.room')}
            value={v.room}
            onChange={(e) => set('room', e.target.value)}
            error={!!fieldError('room')}
            helperText={help('room', t('form.roomHelp'))}
            slotProps={{ inputLabel: { shrink: true }, htmlInput: { maxLength: 120 } }}
          />
          <Box
            sx={{
              display: 'grid',
              gap: 'var(--space-sm)',
              gridTemplateColumns: 'repeat(auto-fit, minmax(12rem, 1fr))',
            }}
          >
            <TextField
              select
              label={t('form.sourceLanguage')}
              value={v.sourceLanguage}
              onChange={(e) =>
                set('sourceLanguage', e.target.value as SessionFormValues['sourceLanguage'])
              }
              helperText={t('form.sourceLanguageHelp')}
              slotProps={{ select: { native: true }, inputLabel: { shrink: true } }}
            >
              {sourceLanguages.map((l) => (
                <option key={l} value={l}>
                  {t(`form.source.${l}`, { defaultValue: languageName(l) })}
                </option>
              ))}
            </TextField>
            <TextField
              select
              label={t('form.provider')}
              value={v.provider}
              onChange={(e) => set('provider', e.target.value as SessionFormValues['provider'])}
              helperText={t(`form.providerHelp.${v.provider}`)}
              slotProps={{ select: { native: true }, inputLabel: { shrink: true } }}
            >
              {providers.map((p) => (
                <option key={p} value={p}>
                  {t(`provider.${p}`)}
                </option>
              ))}
            </TextField>
          </Box>
          <FormControl component="fieldset" error={!!fieldError('targetLanguages')}>
            <FormLabel component="legend">{t('form.targetLanguages')}</FormLabel>
            <FormGroup row>
              {languages.map((l) => (
                <FormControlLabel
                  key={l}
                  control={
                    <Checkbox
                      checked={v.targetLanguages.includes(l)}
                      onChange={(e) => toggleLanguage(l, e.target.checked)}
                    />
                  }
                  label={<span lang={l}>{languageName(l)}</span>}
                />
              ))}
            </FormGroup>
            <FormHelperText>
              {help('targetLanguages', t('form.targetLanguagesHelp'))}
            </FormHelperText>
          </FormControl>
          <TextField
            select
            label={t('form.glossary')}
            value={v.glossaryId}
            onChange={(e) => set('glossaryId', e.target.value)}
            helperText={glossaries.error ? t('form.glossaryUnavailable') : t('form.glossaryHelp')}
            slotProps={{ select: { native: true }, inputLabel: { shrink: true } }}
          >
            <option value="">{t('form.glossaryNone')}</option>
            {glossaries.data?.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
            {v.glossaryId && !glossaries.data?.some((g) => g.id === v.glossaryId) && (
              <option value={v.glossaryId}>{v.glossaryId}</option>
            )}
          </TextField>
          <TextField
            select
            label={t('form.audioSource')}
            value={v.source}
            disabled={running}
            onChange={(e) => set('source', e.target.value as SessionFormValues['source'])}
            helperText={
              running
                ? t('form.audioSourceRunning')
                : v.source === 'srt' && !srt.available && !srt.loading
                  ? t(`form.srtBlocked.${srt.blocker ?? 'unknown'}`)
                  : !srt.available && !srt.loading
                    ? `${t('form.audioSourceHelp.browser')} ${t(`form.srtBlocked.${srt.blocker ?? 'unknown'}`)}`
                    : t(`form.audioSourceHelp.${v.source}`)
            }
            error={!running && v.source === 'srt' && !srt.available && !srt.loading}
            slotProps={{ select: { native: true }, inputLabel: { shrink: true } }}
          >
            {liveSources.map((src) => (
              <option
                key={src}
                value={src}
                disabled={src === 'srt' && !srt.available && v.source !== 'srt'}
              >
                {t(`form.audioSources.${src}`)}
              </option>
            ))}
          </TextField>
          <FormControlLabel
            control={
              <Switch
                checked={v.recordingEnabled}
                onChange={(e) => set('recordingEnabled', e.target.checked)}
              />
            }
            label={t('form.recording')}
          />
          <StreamCaptionsFields session={session} values={v} set={set} urlError={server.value} />
        </DialogContent>
        <DialogActions
          sx={{ flexWrap: 'wrap', gap: 'var(--space-xs)', padding: 'var(--space-md)' }}
        >
          {!creating &&
            (confirmDelete ? (
              <Box
                sx={{
                  display: 'flex',
                  flexWrap: 'wrap',
                  alignItems: 'center',
                  gap: 'var(--space-xs)',
                  marginInlineEnd: 'auto',
                }}
              >
                <Box
                  component="span"
                  sx={{ fontSize: 'var(--text-sm)', color: 'var(--color-danger)' }}
                >
                  {t('form.deleteConfirm')}
                </Box>
                <Button
                  variant="outlined"
                  color="error"
                  loading={remove.isPending}
                  onClick={() => remove.mutate({ params: { path: { sessionId: session.id } } })}
                >
                  {t('form.deleteYes')}
                </Button>
                <Button variant="text" color="secondary" onClick={() => setConfirmDelete(false)}>
                  {t('form.deleteNo')}
                </Button>
              </Box>
            ) : (
              <Button
                variant="outlined"
                color="error"
                startIcon={<DeleteOutlined aria-hidden />}
                onClick={() => setConfirmDelete(true)}
                sx={{ marginInlineEnd: 'auto' }}
              >
                {t('form.delete')}
              </Button>
            ))}
          <Button variant="outlined" color="secondary" onClick={onClose} disabled={pending}>
            {t('form.cancel')}
          </Button>
          <Button type="submit" variant="contained" loading={create.isPending || update.isPending}>
            {creating ? t('form.create') : t('form.save')}
          </Button>
        </DialogActions>
      </Box>
    </Dialog>
  )
}
