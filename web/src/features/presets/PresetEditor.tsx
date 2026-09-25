// SPDX-License-Identifier: Apache-2.0
import ContentCopyOutlined from '@mui/icons-material/ContentCopyOutlined'
import DeleteOutlined from '@mui/icons-material/DeleteOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import FormControlLabel from '@mui/material/FormControlLabel'
import Switch from '@mui/material/Switch'
import TextField from '@mui/material/TextField'
import ToggleButton from '@mui/material/ToggleButton'
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup'
import Typography from '@mui/material/Typography'
import { useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import { ErrorAlert } from '../../components/ErrorAlert'
import { Panel } from '../../components/Panel'
import { segmentedSx } from '../../components/segmented'
import type { OverlayLook } from '../overlay/overlayStyle'
import { OverlayUrls } from './OverlayUrls'
import { PresetPreview } from './PresetPreview'
import {
  fontWeights,
  formFromLook,
  limitsOf,
  lookFromForm,
  styleFromForm,
  validate,
  type ColorField,
  type NumberField,
  type PresetForm,
} from './presetForm'

/** What the editor shows: a built-in (read-only), a saved preset, or a new one. */
export type EditorTarget =
  | { kind: 'builtin'; id: string; name: string; look: OverlayLook }
  | { kind: 'saved'; id: string; name: string; look: OverlayLook }
  | { kind: 'new'; name: string; look: OverlayLook }

export interface PresetEditorProps {
  target: EditorTarget
  onSaved: (id: string) => void
  onDeleted: () => void
  onDuplicate: (name: string, look: OverlayLook) => void
}

const listKey = ['get', '/api/overlay-presets'] as const

/** Style fields with the live preview and the overlay links (OUT-5). */
export function PresetEditor({ target, onSaved, onDeleted, onDuplicate }: PresetEditorProps) {
  const { t } = useTranslation('presets')
  const queryClient = useQueryClient()
  const readOnly = target.kind === 'builtin'
  const [form, setForm] = useState<PresetForm>(() => formFromLook(target.name, target.look))
  const [touched, setTouched] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const refresh = () => queryClient.invalidateQueries({ queryKey: listKey })

  const create = api.useMutation('post', '/api/overlay-presets', {
    onSuccess: (data) => {
      void refresh()
      onSaved(data.id)
    },
  })
  const update = api.useMutation('put', '/api/overlay-presets/{presetId}', {
    onSuccess: (data) => {
      void refresh()
      onSaved(data.id)
    },
  })
  const remove = api.useMutation('delete', '/api/overlay-presets/{presetId}', {
    onSuccess: () => {
      void refresh()
      onDeleted()
    },
  })
  const error: unknown = create.error ?? update.error ?? remove.error

  const problems = touched ? validate(form) : {}
  const set = <K extends keyof PresetForm>(k: K, v: PresetForm[K]) =>
    setForm((prev) => ({ ...prev, [k]: v }))
  const look = lookFromForm(form)

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (readOnly) return
    setTouched(true)
    if (Object.keys(validate(form)).length > 0) return
    const body = { name: form.name.trim(), style: styleFromForm(form) }
    if (target.kind === 'saved') update.mutate({ params: { path: { presetId: target.id } }, body })
    else create.mutate({ body })
  }

  const numberField = (f: NumberField, label: string, help = ' ', step = 1) => {
    const { min, max } = limitsOf(f)
    const bad = problems[f] !== undefined
    return (
      <TextField
        label={label}
        type="number"
        value={form[f]}
        disabled={readOnly}
        onChange={(e) => set(f, e.target.value)}
        error={bad}
        helperText={bad ? t('editor.invalidRange', { min, max }) : help}
        slotProps={{
          inputLabel: { shrink: true },
          htmlInput: { min, max, step, 'aria-invalid': bad || undefined, inputMode: 'decimal' },
        }}
      />
    )
  }

  const colorField = (f: ColorField, label: string, help: string) => {
    const bad = problems[f] !== undefined
    return (
      <TextField
        label={label}
        value={form[f]}
        disabled={readOnly}
        onChange={(e) => set(f, e.target.value)}
        error={bad}
        helperText={bad ? t('editor.invalidColor') : help}
        slotProps={{
          inputLabel: { shrink: true },
          input: {
            startAdornment: (
              <Box
                aria-hidden
                sx={{
                  flex: 'none',
                  inlineSize: '1.25rem',
                  blockSize: '1.25rem',
                  marginInlineEnd: 'var(--space-xs)',
                  borderRadius: 'var(--radius-chip)',
                  border: 'var(--rule-hair) solid var(--color-rule-2)',
                  backgroundColor: look[f],
                }}
              />
            ),
          },
          htmlInput: {
            'aria-invalid': bad || undefined,
            spellCheck: false,
            sx: { fontFamily: 'var(--font-mono)' },
          },
        }}
      />
    )
  }

  const saving = create.isPending || update.isPending
  const actions = readOnly ? (
    <Button
      variant="contained"
      startIcon={<ContentCopyOutlined aria-hidden />}
      onClick={() => onDuplicate(t('editor.copyName', { name: target.name }), look)}
    >
      {t('editor.duplicate')}
    </Button>
  ) : (
    <>
      {target.kind === 'saved' &&
        (confirmDelete ? (
          <>
            <Box component="span" sx={{ fontSize: 'var(--text-sm)', color: 'var(--color-danger)' }}>
              {t('editor.deleteConfirm')}
            </Box>
            <Button
              variant="outlined"
              color="error"
              loading={remove.isPending}
              onClick={() => remove.mutate({ params: { path: { presetId: target.id } } })}
            >
              {t('editor.deleteYes')}
            </Button>
            <Button variant="text" color="secondary" onClick={() => setConfirmDelete(false)}>
              {t('editor.deleteNo')}
            </Button>
          </>
        ) : (
          <Button
            variant="outlined"
            color="error"
            startIcon={<DeleteOutlined aria-hidden />}
            onClick={() => setConfirmDelete(true)}
          >
            {t('editor.delete')}
          </Button>
        ))}
      <Button type="submit" variant="contained" loading={saving}>
        {t('editor.save')}
      </Button>
    </>
  )

  return (
    <Box sx={{ display: 'grid', gap: 'var(--space-md)', minInlineSize: 0 }}>
      <Panel title={t('preview.heading')}>
        <PresetPreview look={look} />
      </Panel>
      <Box component="form" onSubmit={submit} noValidate sx={{ display: 'contents' }}>
        <Panel title={target.name || t('editor.untitled')} actions={actions}>
          {error != null && <ErrorAlert error={error} />}
          {readOnly && (
            <Typography
              variant="body2"
              sx={{ color: 'var(--color-neutral)', maxInlineSize: 'var(--measure)' }}
            >
              {t('editor.readOnly')}
            </Typography>
          )}
          {!readOnly && (
            <TextField
              label={t('editor.name')}
              value={form.name}
              onChange={(e) => set('name', e.target.value)}
              error={problems.name !== undefined}
              helperText={problems.name ? t('editor.invalidName') : ' '}
              slotProps={{
                inputLabel: { shrink: true },
                htmlInput: { maxLength: 80, 'aria-invalid': !!problems.name || undefined },
              }}
              sx={{ maxInlineSize: '28rem' }}
            />
          )}
          <Box
            sx={{
              display: 'grid',
              gap: 'var(--space-sm)',
              gridTemplateColumns: 'repeat(auto-fit, minmax(12rem, 1fr))',
            }}
          >
            {numberField('fontSizePx', t('editor.fontSize'), t('editor.pxHelp'))}
            <TextField
              select
              label={t('editor.fontWeight')}
              value={form.fontWeight}
              disabled={readOnly}
              onChange={(e) => set('fontWeight', e.target.value)}
              helperText=" "
              slotProps={{ select: { native: true }, inputLabel: { shrink: true } }}
            >
              {[...new Set([...fontWeights, form.fontWeight])].map((w) => (
                <option key={w} value={w}>
                  {w}
                </option>
              ))}
            </TextField>
            {numberField('maxLines', t('editor.maxLines'))}
            {colorField('color', t('editor.color'), t('editor.colorHelp'))}
            {colorField('outlineColor', t('editor.outlineColor'), t('editor.colorHelp'))}
            {numberField('outlineWidthPx', t('editor.outlineWidth'), t('editor.outlineWidthHelp'))}
            {colorField('background', t('editor.background'), t('editor.backgroundHelp'))}
            {numberField('marginPx', t('editor.margin'), t('editor.pxHelp'))}
            {numberField('fadeAfterS', t('editor.fadeAfter'), t('editor.fadeAfterHelp'), 0.5)}
          </Box>
          <Box
            sx={{
              display: 'flex',
              flexWrap: 'wrap',
              gap: 'var(--space-md)',
              alignItems: 'center',
            }}
          >
            <Box sx={{ display: 'grid', gap: 'var(--space-2xs)' }}>
              <Typography variant="overline" component="span" id="preset-position">
                {t('editor.position')}
              </Typography>
              <ToggleButtonGroup
                exclusive
                aria-labelledby="preset-position"
                value={form.position}
                disabled={readOnly}
                onChange={(_, v: PresetForm['position'] | null) => v && set('position', v)}
                sx={segmentedSx}
              >
                <ToggleButton value="top">{t('editor.top')}</ToggleButton>
                <ToggleButton value="bottom">{t('editor.bottom')}</ToggleButton>
              </ToggleButtonGroup>
            </Box>
            <Box sx={{ display: 'grid', gap: 'var(--space-2xs)' }}>
              <Typography variant="overline" component="span" id="preset-align">
                {t('editor.align')}
              </Typography>
              <ToggleButtonGroup
                exclusive
                aria-labelledby="preset-align"
                value={form.align}
                disabled={readOnly}
                onChange={(_, v: PresetForm['align'] | null) => v && set('align', v)}
                sx={segmentedSx}
              >
                <ToggleButton value="left">{t('editor.left')}</ToggleButton>
                <ToggleButton value="center">{t('editor.center')}</ToggleButton>
                <ToggleButton value="right">{t('editor.right')}</ToggleButton>
              </ToggleButtonGroup>
            </Box>
            <FormControlLabel
              control={
                <Switch
                  checked={form.showInterim}
                  disabled={readOnly}
                  onChange={(e) => set('showInterim', e.target.checked)}
                />
              }
              label={t('editor.interim')}
            />
          </Box>
        </Panel>
      </Box>
      <OverlayUrls presetId={target.kind === 'new' ? undefined : target.id} />
    </Box>
  )
}
