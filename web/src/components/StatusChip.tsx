// SPDX-License-Identifier: Apache-2.0
import Box from '@mui/material/Box'
import type { SxProps, Theme } from '@mui/material/styles'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

export type ChipStatus = 'live' | 'ok' | 'warn' | 'error' | 'idle' | 'starting'

const tones: Record<ChipStatus, SxProps<Theme>> = {
  // The on-air tally: the only filled chip.
  live: {
    backgroundColor: 'var(--color-live)',
    borderColor: 'var(--color-live)',
    color: 'var(--color-live-ink)',
  },
  ok: { color: 'var(--color-ok)', borderColor: 'currentColor' },
  warn: { color: 'var(--color-warn)', borderColor: 'currentColor' },
  error: {
    color: 'var(--color-danger)',
    borderColor: 'currentColor',
    backgroundColor: 'var(--color-danger-soft)',
  },
  idle: { color: 'var(--color-neutral)', borderColor: 'var(--color-rule-2)' },
  starting: { color: 'var(--color-accent-text)', borderColor: 'currentColor' },
}

export interface StatusChipProps {
  status: ChipStatus
  /** Visible text. Defaults to the translated status name ("Live", "Idle"…). Colour never stands alone. */
  label?: ReactNode
  /** Hide the leading dot, e.g. for a plain readout such as "EN → ES". */
  noDot?: boolean
  sx?: SxProps<Theme>
}

/** Status as a mono uppercase label with a leading dot (docs/design.md § Components). */
export function StatusChip({ status, label, noDot, sx }: StatusChipProps) {
  const { t } = useTranslation()
  return (
    <Box
      component="span"
      data-status={status}
      sx={[
        {
          display: 'inline-flex',
          alignItems: 'center',
          gap: 'var(--space-2xs)',
          whiteSpace: 'nowrap',
          fontFamily: 'var(--font-mono)',
          fontSize: 'var(--text-xs)',
          fontWeight: 500,
          lineHeight: 1,
          letterSpacing: 'var(--tracking-label)',
          textTransform: 'uppercase',
          paddingBlock: 'var(--space-2xs)',
          paddingInline: 'var(--space-xs)',
          borderRadius: 'var(--radius-chip)',
          border: 'var(--rule-hair) solid',
        },
        tones[status],
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
    >
      {!noDot && (
        <Box
          component="span"
          aria-hidden
          sx={{
            inlineSize: '0.5rem',
            blockSize: '0.5rem',
            borderRadius: '50%',
            backgroundColor: 'currentColor',
            flex: 'none',
          }}
        />
      )}
      {label ?? t(`status.${status}`)}
    </Box>
  )
}
