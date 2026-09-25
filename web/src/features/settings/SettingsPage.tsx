// SPDX-License-Identifier: Apache-2.0
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import FormControl from '@mui/material/FormControl'
import FormControlLabel from '@mui/material/FormControlLabel'
import FormGroup from '@mui/material/FormGroup'
import FormHelperText from '@mui/material/FormHelperText'
import FormLabel from '@mui/material/FormLabel'
import Switch from '@mui/material/Switch'
import TextField, { type TextFieldProps } from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useQueryClient } from '@tanstack/react-query'
import { useId, useState, type FormEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import { toApiError } from '../../components/apiError'
import { ErrorAlert } from '../../components/ErrorAlert'
import { nativeLanguageName } from '../../components/languageNames'
import { Notice } from '../../components/Notice'
import { Panel } from '../../components/Panel'
import { StatusChip } from '../../components/StatusChip'
import { AdminPage } from '../admin/AdminLayout'
import {
  bitrates,
  commonLanguages,
  fieldPath,
  sourceLanguages,
  toSettings,
  validate,
  valuesFrom,
  type Bitrate,
  type Field,
  type Problem,
  type Settings,
  type SettingsValues,
  type SourceLanguage,
} from './settingsForm'

/** `/admin/settings`: server-wide defaults (ADM-2, OUT-4). */
export function SettingsPage() {
  const { t } = useTranslation('settings')
  const { t: ta } = useTranslation('admin')
  const settings = api.useQuery('get', '/api/settings')
  const error: unknown = settings.error

  if (settings.data) return <SettingsForm settings={settings.data} />
  return (
    <AdminPage title={ta('nav.settings')}>
      {error != null && (
        <ErrorAlert
          error={error}
          action={
            <Button variant="outlined" color="secondary" onClick={() => void settings.refetch()}>
              {t('retry')}
            </Button>
          }
        />
      )}
    </AdminPage>
  )
}

const fieldGrid = {
  display: 'grid',
  gap: 'var(--space-sm)',
  gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 16rem), 1fr))',
} as const

function SettingsForm({ settings }: { settings: Settings }) {
  const { t } = useTranslation('settings')
  const { t: ta } = useTranslation('admin')
  const formId = useId()
  const queryClient = useQueryClient()
  const [base, setBase] = useState(settings)
  const [v, setV] = useState<SettingsValues>(() => valuesFrom(settings))
  const [touched, setTouched] = useState(false)
  const [saved, setSaved] = useState(false)
  const network = api.useQuery('get', '/api/network')

  const save = api.useMutation('put', '/api/settings', {
    onSuccess: (data) => {
      setBase(data)
      setV(valuesFrom(data))
      setTouched(false)
      setSaved(true)
      void queryClient.invalidateQueries({ queryKey: ['get', '/api/settings'] })
      void queryClient.invalidateQueries({ queryKey: ['get', '/api/network'] })
    },
  })
  const saveError: unknown = save.error

  const dirty = JSON.stringify(v) !== JSON.stringify(valuesFrom(base))
  const problems = touched ? validate(v) : {}
  const serverFields = toApiError(saveError)?.fields ?? {}
  const problemText = (p: Problem) =>
    p.kind === 'integer' ? t('invalid.integer', { min: p.min, max: p.max }) : t(`invalid.${p.kind}`)
  const fieldError = (f: Field): string | undefined => {
    const p = problems[f]
    if (p) return problemText(p)
    return serverFields[fieldPath[f]] ? t('invalid.server') : undefined
  }
  const set = <K extends Field>(k: K, value: SettingsValues[K]) => {
    setSaved(false)
    setV((prev) => ({ ...prev, [k]: value }))
  }

  /** Props for a text field bound to `f`, with the error replacing the helper. */
  const text = (
    f: Field,
    label: string,
    help: ReactNode = ' ',
    opts: { mono?: boolean; numeric?: boolean } = {},
  ): TextFieldProps => {
    const err = fieldError(f)
    return {
      label,
      value: String(v[f]),
      onChange: (e) => set(f, e.target.value as never),
      error: !!err,
      helperText: err ?? help,
      slotProps: {
        inputLabel: { shrink: true },
        htmlInput: {
          'aria-invalid': !!err || undefined,
          spellCheck: false,
          ...(opts.numeric && { inputMode: 'numeric', sx: { fontVariantNumeric: 'tabular-nums' } }),
          ...(opts.mono && { sx: { fontFamily: 'var(--font-mono)' } }),
        },
      },
    }
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    setTouched(true)
    if (Object.keys(validate(v)).length > 0) return
    save.mutate({ body: toSettings(v, base) })
  }

  const languages = [...new Set([...commonLanguages, ...v.targetLanguages])]
  const toggleLanguage = (lang: string, on: boolean) =>
    set(
      'targetLanguages',
      on ? [...v.targetLanguages, lang] : v.targetLanguages.filter((l) => l !== lang),
    )

  const interfaces = (network.data?.interfaces ?? []).filter((i) => i.family === 'ipv4')
  const interfaceNames = [...new Set(interfaces.map((i) => i.name))]
  const knownInterface = !v.preferredInterface || interfaceNames.includes(v.preferredInterface)

  return (
    <AdminPage
      title={ta('nav.settings')}
      actions={
        <>
          {dirty ? (
            <StatusChip status="warn" label={t('unsaved')} />
          ) : (
            saved && <StatusChip status="ok" label={t('saved')} />
          )}
          <Button type="submit" form={formId} variant="contained" loading={save.isPending}>
            {t('save')}
          </Button>
        </>
      }
    >
      <Box
        component="form"
        id={formId}
        onSubmit={submit}
        noValidate
        sx={{ display: 'grid', gap: 'var(--space-md)', maxInlineSize: '64rem' }}
      >
        {saveError != null && <ErrorAlert error={saveError} />}
        {Object.keys(problems).length > 0 && <Notice>{t('invalidForm')}</Notice>}

        <Panel title={t('languages.title')}>
          <Intro>{t('languages.intro')}</Intro>
          <Box sx={fieldGrid}>
            <TextField
              select
              label={t('languages.source')}
              value={v.sourceLanguage}
              onChange={(e) => set('sourceLanguage', e.target.value as SourceLanguage)}
              helperText={t('languages.sourceHelp')}
              slotProps={{ select: { native: true }, inputLabel: { shrink: true } }}
            >
              {sourceLanguages.map((l) => (
                <option key={l} value={l}>
                  {t(`languages.sourceOption.${l}`)}
                </option>
              ))}
            </TextField>
          </Box>
          <FormControl component="fieldset" error={!!fieldError('targetLanguages')}>
            <FormLabel component="legend">{t('languages.targets')}</FormLabel>
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
                  label={<span lang={l}>{nativeLanguageName(l)}</span>}
                />
              ))}
            </FormGroup>
            <FormHelperText>
              {fieldError('targetLanguages') ?? t('languages.targetsHelp')}
            </FormHelperText>
          </FormControl>
        </Panel>

        <Panel title={t('providers.title')}>
          <Intro>{t('providers.intro')}</Intro>
          <Typography variant="h6" component="h3">
            {t('providers.gemini')}
          </Typography>
          <Box sx={fieldGrid}>
            <TextField {...text('liveModel', t('providers.liveModel'), ' ', { mono: true })} />
            <TextField
              {...text('translationModel', t('providers.translationModel'), ' ', { mono: true })}
            />
          </Box>
          <Typography variant="h6" component="h3">
            {t('providers.local')}
          </Typography>
          <Box sx={fieldGrid}>
            <TextField {...text('whisperUrl', t('providers.whisperUrl'), ' ', { mono: true })} />
            <TextField
              {...text('whisperModel', t('providers.whisperModel'), ' ', { mono: true })}
            />
            <TextField {...text('ollamaUrl', t('providers.ollamaUrl'), ' ', { mono: true })} />
            <TextField {...text('gemmaModel', t('providers.gemmaModel'), ' ', { mono: true })} />
          </Box>
          <Box sx={fieldGrid}>
            <TextField
              {...text(
                'contextSentences',
                t('providers.contextSentences'),
                t('providers.contextSentencesHelp'),
                { numeric: true },
              )}
            />
          </Box>
          <SwitchField
            checked={v.fallback}
            onChange={(on) => set('fallback', on)}
            label={t('providers.fallback')}
            help={t('providers.fallbackHelp')}
          />
        </Panel>

        <Panel title={t('captions.title')}>
          <Box sx={fieldGrid}>
            <TextField
              {...text(
                'maxCharsPerLine',
                t('captions.maxCharsPerLine'),
                t('captions.maxCharsPerLineHelp'),
                { numeric: true },
              )}
            />
            <TextField
              {...text('maxLines', t('captions.maxLines'), t('captions.maxLinesHelp'), {
                numeric: true,
              })}
            />
          </Box>
        </Panel>

        <Panel title={t('network.title')}>
          <Box sx={fieldGrid}>
            <TextField
              {...text(
                'publicBaseUrl',
                t('network.publicBaseUrl'),
                t('network.publicBaseUrlHelp'),
                {
                  mono: true,
                },
              )}
            />
            {interfaceNames.length > 0 && knownInterface ? (
              <TextField
                select
                label={t('network.preferredInterface')}
                value={v.preferredInterface}
                onChange={(e) => set('preferredInterface', e.target.value)}
                helperText={t('network.preferredInterfaceHelp')}
                slotProps={{ select: { native: true }, inputLabel: { shrink: true } }}
              >
                <option value="">{t('network.automatic')}</option>
                {interfaceNames.map((name) => (
                  <option key={name} value={name}>
                    {[name, ...interfaces.filter((i) => i.name === name).map((i) => i.ip)].join(
                      ' · ',
                    )}
                  </option>
                ))}
              </TextField>
            ) : (
              <TextField
                {...text(
                  'preferredInterface',
                  t('network.preferredInterface'),
                  t('network.preferredInterfaceHelp'),
                  { mono: true },
                )}
              />
            )}
          </Box>
        </Panel>

        <Panel title={t('recording.title')}>
          <SwitchField
            checked={v.recordingEnabled}
            onChange={(on) => set('recordingEnabled', on)}
            label={t('recording.enabledByDefault')}
            help={t('recording.enabledByDefaultHelp')}
          />
          <Box sx={fieldGrid}>
            <TextField
              select
              label={t('recording.bitrate')}
              value={v.bitrateKbps}
              onChange={(e) => set('bitrateKbps', Number(e.target.value) as Bitrate)}
              helperText={t('recording.bitrateHelp')}
              slotProps={{ select: { native: true }, inputLabel: { shrink: true } }}
            >
              {bitrates.map((b) => (
                <option key={b} value={b}>
                  {t('recording.bitrateOption', { kbps: b })}
                </option>
              ))}
            </TextField>
            <TextField
              {...text(
                'retentionDays',
                t('recording.retentionDays'),
                t('recording.retentionDaysHelp'),
                { numeric: true },
              )}
            />
          </Box>
        </Panel>

        <Panel
          title={t('srt.title')}
          actions={
            <StatusChip
              status={base.srt?.passphraseSet ? 'ok' : 'idle'}
              label={base.srt?.passphraseSet ? t('srt.passphraseSet') : t('srt.passphraseUnset')}
            />
          }
        >
          <SwitchField
            checked={v.srtEnabled}
            onChange={(on) => set('srtEnabled', on)}
            label={t('srt.enabled')}
            help={t('srt.enabledHelp')}
          />
          <Box sx={fieldGrid}>
            <TextField {...text('srtPort', t('srt.port'), t('srt.portHelp'), { numeric: true })} />
            <TextField
              {...text('srtLatencyMs', t('srt.latencyMs'), t('srt.latencyMsHelp'), {
                numeric: true,
              })}
            />
          </Box>
        </Panel>

        <Panel title={t('obs.title')}>
          <Box sx={fieldGrid}>
            <TextField
              {...text('obsUrl', t('obs.websocketUrl'), t('obs.websocketUrlHelp'), { mono: true })}
            />
          </Box>
        </Panel>
      </Box>
    </AdminPage>
  )
}

function Intro({ children }: { children: ReactNode }) {
  return (
    <Typography
      sx={{ color: 'var(--color-ink-2)', maxInlineSize: 'var(--measure)', marginBlockStart: 0 }}
    >
      {children}
    </Typography>
  )
}

function SwitchField({
  checked,
  onChange,
  label,
  help,
}: {
  checked: boolean
  onChange: (on: boolean) => void
  label: string
  help: string
}) {
  const helpId = useId()
  return (
    <Box sx={{ display: 'grid', gap: 'var(--space-3xs)' }}>
      <FormControlLabel
        control={
          <Switch
            checked={checked}
            onChange={(e) => onChange(e.target.checked)}
            slotProps={{ input: { 'aria-describedby': helpId } }}
          />
        }
        label={label}
      />
      <FormHelperText id={helpId} sx={{ marginBlockStart: 0 }}>
        {help}
      </FormHelperText>
    </Box>
  )
}
