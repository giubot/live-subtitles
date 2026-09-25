// SPDX-License-Identifier: Apache-2.0
import AddOutlined from '@mui/icons-material/AddOutlined'
import LockOutlined from '@mui/icons-material/LockOutlined'
import SubtitlesOutlined from '@mui/icons-material/SubtitlesOutlined'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Typography from '@mui/material/Typography'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../api/client'
import { ErrorAlert } from '../../components/ErrorAlert'
import { Panel } from '../../components/Panel'
import { AdminPage } from '../admin/AdminLayout'
import { isBuiltinPreset, lookFromStyle, type OverlayLook } from '../overlay/overlayStyle'
import { PresetEditor, type EditorTarget } from './PresetEditor'
import type { SavedPreset } from './presetForm'

type Selection =
  { kind: 'preset'; id: string } | { kind: 'new'; name: string; look: OverlayLook; n: number }

const wide = '@media (min-width: 60rem)'

/**
 * `/admin/overlays` (P3-14, OUT-5): the server's presets, built-ins
 * (`builtIn`, read-only: duplicate to change one) first, then the saved
 * ones, each with a live preview and its overlay links.
 */
export function OverlayPresetsPage() {
  const { t } = useTranslation('presets')
  const presets = api.useQuery('get', '/api/overlay-presets')
  // Listed without error responses, but it can still fail (network, proxy).
  const listError: unknown = presets.error
  const [selection, setSelection] = useState<Selection>({ kind: 'preset', id: 'classic' })
  const [created, setCreated] = useState(0)
  const builtIns = presets.data?.filter((p) => p.builtIn) ?? []
  const saved = presets.data?.filter((p) => !p.builtIn) ?? []
  /** Built-ins have translated names; the server's are English. */
  const nameOf = (p: SavedPreset) =>
    p.builtIn && isBuiltinPreset(p.id) ? t(`builtin.${p.id}`) : p.name

  const startNew = (name: string, look: OverlayLook) => {
    setCreated((n) => n + 1)
    setSelection({ kind: 'new', name, look, n: created + 1 })
  }

  let target: EditorTarget | undefined
  let key = ''
  if (selection.kind === 'preset') {
    const p = presets.data?.find((x) => x.id === selection.id)
    if (p) {
      target = {
        kind: p.builtIn ? 'builtin' : 'saved',
        id: p.id,
        name: nameOf(p),
        look: lookFromStyle(p.style),
      }
      key = `${p.builtIn ? 'builtin' : 'saved'}:${p.id}`
    }
  } else {
    target = { kind: 'new', name: selection.name, look: selection.look }
    key = `new:${selection.n}`
  }

  return (
    <AdminPage
      title={t('title')}
      actions={
        <Button
          variant="outlined"
          color="secondary"
          startIcon={<AddOutlined aria-hidden />}
          onClick={() => startNew(t('newName'), lookFromStyle(builtIns[0]?.style ?? {}))}
        >
          {t('newPreset')}
        </Button>
      }
    >
      <Box
        sx={{
          display: 'grid',
          gap: 'var(--space-md)',
          alignItems: 'start',
          [wide]: { gridTemplateColumns: '16rem minmax(0, 1fr)' },
        }}
      >
        <Panel title={t('list.heading')}>
          <Box
            component="nav"
            aria-label={t('list.heading')}
            sx={{ display: 'grid', gap: 'var(--space-md)' }}
          >
            <PresetGroup label={t('list.builtin')}>
              {listError != null && <ErrorAlert error={listError} />}
              {builtIns.map((p) => (
                <PresetItem
                  key={p.id}
                  selected={selection.kind === 'preset' && selection.id === p.id}
                  onClick={() => setSelection({ kind: 'preset', id: p.id })}
                  icon={<LockOutlined aria-hidden />}
                >
                  {nameOf(p)}
                </PresetItem>
              ))}
            </PresetGroup>
            <PresetGroup label={t('list.saved')}>
              {presets.data && saved.length === 0 && selection.kind !== 'new' && (
                <Typography variant="body2" sx={{ color: 'var(--color-neutral)' }}>
                  {t('list.empty')}
                </Typography>
              )}
              {saved.map((p) => (
                <PresetItem
                  key={p.id}
                  selected={selection.kind === 'preset' && selection.id === p.id}
                  onClick={() => setSelection({ kind: 'preset', id: p.id })}
                  icon={<SubtitlesOutlined aria-hidden />}
                >
                  {p.name}
                </PresetItem>
              ))}
              {selection.kind === 'new' && (
                <PresetItem selected onClick={() => undefined} icon={<AddOutlined aria-hidden />}>
                  {t('list.unsaved')}
                </PresetItem>
              )}
            </PresetGroup>
          </Box>
        </Panel>
        {target && (
          <PresetEditor
            key={key}
            target={target}
            onSaved={(id) => setSelection({ kind: 'preset', id })}
            onDeleted={() => setSelection({ kind: 'preset', id: builtIns[0]?.id ?? 'classic' })}
            onDuplicate={startNew}
          />
        )}
      </Box>
    </AdminPage>
  )
}

function PresetGroup({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Box sx={{ display: 'grid', gap: 'var(--space-2xs)' }}>
      <Typography variant="overline" component="h3">
        {label}
      </Typography>
      {children}
    </Box>
  )
}

function PresetItem({
  selected,
  onClick,
  icon,
  children,
}: {
  selected: boolean
  onClick: () => void
  icon: ReactNode
  children: ReactNode
}) {
  return (
    <Box
      component="button"
      type="button"
      aria-current={selected || undefined}
      onClick={onClick}
      sx={{
        display: 'flex',
        alignItems: 'center',
        gap: 'var(--space-xs)',
        minBlockSize: '2.5rem',
        paddingInline: 'var(--space-sm)',
        border: 0,
        borderRadius: 'var(--radius-input)',
        backgroundColor: 'transparent',
        color: 'var(--color-ink-2)',
        font: 'inherit',
        textAlign: 'start',
        cursor: 'pointer',
        overflowWrap: 'anywhere',
        transition: 'background-color var(--dur-micro) var(--ease-out)',
        '& svg': { flex: 'none', fontSize: '1.1rem', color: 'var(--color-muted)' },
        '@media (hover: hover)': { '&:hover': { backgroundColor: 'var(--color-paper-3)' } },
        '&:focus-visible': {
          outline: 'var(--rule-fine) solid var(--color-focus)',
          outlineOffset: 'var(--rule-fine)',
        },
        '&[aria-current="true"]': {
          backgroundColor: 'var(--color-accent-soft)',
          color: 'var(--color-accent-text)',
          fontWeight: 700,
          '& svg': { color: 'var(--color-accent-text)' },
        },
      }}
    >
      {icon}
      {children}
    </Box>
  )
}
