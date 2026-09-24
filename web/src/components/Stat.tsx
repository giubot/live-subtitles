// SPDX-License-Identifier: Apache-2.0
import Box from '@mui/material/Box'
import type { SxProps, Theme } from '@mui/material/styles'
import type { ReactNode } from 'react'

export interface StatProps {
  /** Mono uppercase label ("Latency p95"). */
  label: ReactNode
  /** The readout ("2.4 s", "128"), or a LevelMeter. */
  value: ReactNode
  /** A smaller second line under the value ("EN 1.3 s"). */
  detail?: ReactNode
  sx?: SxProps<Theme>
}

/** Label + tabular value readout for dashboards (`.stat` in design/preview.html). */
export function Stat({ label, value, detail, sx }: StatProps) {
  return (
    <Box
      component="dl"
      sx={[
        {
          margin: 0,
          display: 'grid',
          gap: 'var(--space-2xs)',
          alignContent: 'start',
          minInlineSize: 0,
        },
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
    >
      <Box
        component="dt"
        sx={{
          fontFamily: 'var(--font-mono)',
          fontSize: 'var(--text-xs)',
          fontWeight: 500,
          lineHeight: 1,
          letterSpacing: 'var(--tracking-label)',
          textTransform: 'uppercase',
          color: 'var(--color-muted)',
        }}
      >
        {label}
      </Box>
      <Box
        component="dd"
        sx={{
          margin: 0,
          fontFamily: 'var(--font-mono)',
          fontSize: 'var(--text-base)',
          fontWeight: 500,
          lineHeight: 1.2,
          color: 'var(--color-ink)',
          fontVariantNumeric: 'tabular-nums',
          whiteSpace: 'nowrap',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
        }}
      >
        {value}
        {detail != null && (
          <Box
            component="span"
            sx={{
              display: 'block',
              marginBlockStart: 'var(--space-3xs)',
              fontSize: 'var(--text-xs)',
              color: 'var(--color-muted)',
            }}
          >
            {detail}
          </Box>
        )}
      </Box>
    </Box>
  )
}
