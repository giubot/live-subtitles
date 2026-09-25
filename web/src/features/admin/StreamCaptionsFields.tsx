// SPDX-License-Identifier: Apache-2.0
import SendOutlined from '@mui/icons-material/SendOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import FormControlLabel from '@mui/material/FormControlLabel'
import Switch from '@mui/material/Switch'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import { toApiError } from '../../components/apiError'
import { ErrorAlert } from '../../components/ErrorAlert'
import { nativeLanguageName } from '../../components/languageNames'
import { StatusChip } from '../../components/StatusChip'
import { ccChip, streamCaptionTargets, type Session, type SessionFormValues } from './sessionForm'

export interface StreamCaptionsFieldsProps {
  /** The saved session; absent while creating one. */
  session?: Session
  values: SessionFormValues
  set: <K extends keyof SessionFormValues>(k: K, value: SessionFormValues[K]) => void
  /** The server's field error for the ingestion URL (`fields.value` of a failed PUT). */
  urlError?: string
}

/**
 * Session editor section for closed captions sent into the live stream
 * (CC-3): target, track, the write-only YouTube ingestion URL and a test
 * caption. The URL and the test need a saved session.
 */
export function StreamCaptionsFields({
  session,
  values: v,
  set,
  urlError,
}: StreamCaptionsFieldsProps) {
  const { t } = useTranslation('admin')
  const queryClient = useQueryClient()
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['get', '/api/sessions'] })
  const removeUrl = api.useMutation(
    'delete',
    '/api/sessions/{sessionId}/stream-captions/youtube-url',
    { onSettled: refresh },
  )
  const test = api.useMutation('post', '/api/sessions/{sessionId}/stream-captions/test')
  // Already gone (404 secret.not_found) is as good as removed.
  const removeGone = toApiError(removeUrl.error)?.code === 'secret.not_found'
  const removed = removeUrl.isSuccess || removeGone
  const urlSet = !!session?.streamCaptions?.youtubeUrlSet && !removed
  const tracks = [...new Set([...v.targetLanguages, 'source', v.ccTrack])]
  const youtube = v.ccTarget === 'youtube_http'

  return (
    <Box
      component="fieldset"
      sx={{
        display: 'grid',
        gap: 'var(--space-sm)',
        margin: 0,
        padding: 'var(--space-sm) 0 0',
        border: 0,
        borderBlockStart: 'var(--rule-hair) solid var(--color-rule)',
        minInlineSize: 0,
      }}
    >
      <Typography component="legend" variant="overline" sx={{ paddingInline: 0 }}>
        {t('cc.title')}
      </Typography>
      <FormControlLabel
        control={
          <Switch checked={v.ccEnabled} onChange={(e) => set('ccEnabled', e.target.checked)} />
        }
        label={t('cc.enabled')}
      />
      <Typography
        variant="body2"
        sx={{ color: 'var(--color-muted)', maxInlineSize: 'var(--measure)' }}
      >
        {t('cc.hint')}
      </Typography>
      {v.ccEnabled && (
        <>
          <Box
            sx={{
              display: 'grid',
              gap: 'var(--space-sm)',
              gridTemplateColumns: 'repeat(auto-fit, minmax(12rem, 1fr))',
            }}
          >
            <TextField
              select
              label={t('cc.target')}
              value={v.ccTarget}
              onChange={(e) => set('ccTarget', e.target.value as SessionFormValues['ccTarget'])}
              helperText={t(`cc.targetHelp.${v.ccTarget}`)}
              slotProps={{ select: { native: true }, inputLabel: { shrink: true } }}
            >
              {streamCaptionTargets.map((target) => (
                <option key={target} value={target}>
                  {t(`cc.targets.${target}`)}
                </option>
              ))}
            </TextField>
            <TextField
              select
              label={t('cc.track')}
              value={v.ccTrack}
              onChange={(e) => set('ccTrack', e.target.value)}
              helperText={t('cc.trackHelp')}
              slotProps={{ select: { native: true }, inputLabel: { shrink: true } }}
            >
              {tracks.map((track) => (
                <option key={track} value={track}>
                  {track === 'source' ? t('cc.sourceTrack') : nativeLanguageName(track)}
                </option>
              ))}
            </TextField>
          </Box>
          {youtube &&
            (session ? (
              <Box sx={{ display: 'grid', gap: 'var(--space-xs)' }}>
                <TextField
                  label={t('cc.youtubeUrl')}
                  value={v.ccYoutubeUrl}
                  onChange={(e) => set('ccYoutubeUrl', e.target.value)}
                  placeholder={urlSet ? '••••••••' : undefined}
                  error={!!urlError}
                  helperText={
                    urlError
                      ? t('cc.youtubeUrlInvalid')
                      : urlSet
                        ? t('cc.youtubeUrlSet')
                        : t('cc.youtubeUrlHelp')
                  }
                  autoComplete="off"
                  slotProps={{
                    inputLabel: { shrink: true },
                    htmlInput: { spellCheck: false, sx: { fontFamily: 'var(--font-mono)' } },
                  }}
                />
                {urlSet && (
                  <Box>
                    <Button
                      variant="text"
                      color="error"
                      size="small"
                      loading={removeUrl.isPending}
                      onClick={() =>
                        removeUrl.mutate({ params: { path: { sessionId: session.id } } })
                      }
                    >
                      {t('cc.youtubeUrlRemove')}
                    </Button>
                  </Box>
                )}
                {removeUrl.error != null && !removeGone && <ErrorAlert error={removeUrl.error} />}
              </Box>
            ) : (
              <Typography variant="body2" sx={{ color: 'var(--color-muted)' }}>
                {t('cc.saveFirst')}
              </Typography>
            ))}
          {session && (
            <Box
              sx={{
                display: 'flex',
                flexWrap: 'wrap',
                alignItems: 'center',
                gap: 'var(--space-xs)',
              }}
            >
              <Button
                variant="outlined"
                color="secondary"
                size="small"
                startIcon={<SendOutlined aria-hidden />}
                loading={test.isPending}
                onClick={() =>
                  test.mutate({
                    params: { path: { sessionId: session.id } },
                    body: { text: t('cc.testText') },
                  })
                }
              >
                {t('cc.test')}
              </Button>
              {test.data && (
                <StatusChip
                  status={ccChip[test.data.state]}
                  label={t(`cc.state.${test.data.state}`)}
                />
              )}
              <Typography variant="body2" sx={{ color: 'var(--color-muted)', flexBasis: '100%' }}>
                {t('cc.testHelp')}
              </Typography>
            </Box>
          )}
          {test.data?.error && <ErrorAlert error={test.data.error} />}
          {test.error != null && <ErrorAlert error={test.error} />}
        </>
      )}
    </Box>
  )
}
