// SPDX-License-Identifier: Apache-2.0
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import Dialog from '@mui/material/Dialog'
import DialogActions from '@mui/material/DialogActions'
import DialogContent from '@mui/material/DialogContent'
import DialogTitle from '@mui/material/DialogTitle'
import FormControlLabel from '@mui/material/FormControlLabel'
import TextField from '@mui/material/TextField'
import { useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import { toApiError } from '../../components/apiError'
import { ErrorAlert } from '../../components/ErrorAlert'
import type { Session } from './sessionForm'

export interface FileSourceDialogProps {
  session: Session
  onClose: () => void
}

/**
 * Starts an idle session fed by a server-side file or an http(s) URL at
 * real-time speed (AUD-3, for rehearsals and tests). The server checks the
 * path; its errors come back on the `uri` field.
 */
export function FileSourceDialog({ session, onClose }: FileSourceDialogProps) {
  const { t } = useTranslation('admin')
  const queryClient = useQueryClient()
  const [uri, setUri] = useState('')
  const [loop, setLoop] = useState(false)
  const [startAt, setStartAt] = useState('')
  const [touched, setTouched] = useState(false)
  const play = api.useMutation('post', '/api/sessions/{sessionId}/sources/file', {
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['get', '/api/sessions'] })
      onClose()
    },
  })

  const startAtSec = startAt.trim() === '' ? 0 : Number(startAt)
  const startAtInvalid = !Number.isFinite(startAtSec) || startAtSec < 0
  const uriMissing = uri.trim() === ''
  const fields = toApiError(play.error)?.fields ?? {}
  const uriError = (touched && uriMissing) || !!fields.uri
  const startAtError = startAtInvalid || !!fields.startAtSec

  const submit = (e: FormEvent) => {
    e.preventDefault()
    setTouched(true)
    if (uriMissing || startAtInvalid) return
    play.mutate({
      params: { path: { sessionId: session.id } },
      body: { uri: uri.trim(), loop, startAtSec },
    })
  }

  return (
    <Dialog open onClose={onClose} fullWidth maxWidth="sm" aria-labelledby="file-source-title">
      <Box component="form" noValidate onSubmit={submit}>
        <DialogTitle id="file-source-title">{t('file.title', { name: session.name })}</DialogTitle>
        <DialogContent sx={{ display: 'grid', gap: 'var(--space-md)' }}>
          {play.error != null && <ErrorAlert error={play.error} />}
          <TextField
            autoFocus
            required
            label={t('file.uri')}
            value={uri}
            onChange={(e) => setUri(e.target.value)}
            error={uriError}
            helperText={touched && uriMissing ? t('file.uriRequired') : t('file.uriHelp')}
            placeholder="testdata/audio/fixtures/en.wav"
            slotProps={{
              inputLabel: { shrink: true },
              htmlInput: {
                'aria-invalid': uriError || undefined,
                spellCheck: false,
                sx: { fontFamily: 'var(--font-mono)' },
              },
            }}
          />
          <Box
            sx={{
              display: 'flex',
              flexWrap: 'wrap',
              alignItems: 'center',
              gap: 'var(--space-sm) var(--space-md)',
            }}
          >
            <TextField
              label={t('file.startAt')}
              value={startAt}
              onChange={(e) => setStartAt(e.target.value)}
              error={startAtError}
              helperText={startAtError ? t('file.startAtInvalid') : undefined}
              placeholder="0"
              sx={{ inlineSize: '10rem' }}
              slotProps={{
                inputLabel: { shrink: true },
                htmlInput: {
                  inputMode: 'decimal',
                  'aria-invalid': startAtError || undefined,
                },
              }}
            />
            <FormControlLabel
              control={<Checkbox checked={loop} onChange={(e) => setLoop(e.target.checked)} />}
              label={t('file.loop')}
            />
          </Box>
        </DialogContent>
        <DialogActions
          sx={{ flexWrap: 'wrap', gap: 'var(--space-xs)', padding: 'var(--space-md)' }}
        >
          <Button variant="outlined" color="secondary" onClick={onClose} disabled={play.isPending}>
            {t('file.cancel')}
          </Button>
          <Button type="submit" variant="contained" loading={play.isPending}>
            {t('file.play')}
          </Button>
        </DialogActions>
      </Box>
    </Dialog>
  )
}
