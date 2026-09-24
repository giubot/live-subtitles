// SPDX-License-Identifier: Apache-2.0
import WarningAmberOutlined from '@mui/icons-material/WarningAmberOutlined'
import Box from '@mui/material/Box'
import type { SxProps, Theme } from '@mui/material/styles'
import type { ReactNode } from 'react'

export interface NoticeProps {
  /** Plain-language warning: what's happening and what to do. */
  children: ReactNode
  /** An Outlined icon; defaults to a warning sign. Rendered aria-hidden. */
  icon?: ReactNode
  sx?: SxProps<Theme>
}

/**
 * Attention message on warn-soft (`.notice` in design/preview.html), e.g.
 * "Clipping. Lower the mixer's output a little." For failures with a code,
 * use ErrorAlert. It's a polite live region, so changes are announced.
 */
export function Notice({ children, icon, sx }: NoticeProps) {
  return (
    <Box
      role="status"
      sx={[
        {
          display: 'flex',
          alignItems: 'flex-start',
          gap: 'var(--space-xs)',
          padding: 'var(--space-sm)',
          borderRadius: 'var(--radius-input)',
          backgroundColor: 'var(--color-warn-soft)',
          color: 'var(--color-ink-2)',
          fontSize: 'var(--text-sm)',
          lineHeight: 'var(--leading-body)',
          '& > svg': {
            flex: 'none',
            fontSize: '1.1rem',
            color: 'var(--color-warn)',
            marginBlockStart: 'var(--space-3xs)',
          },
        },
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
    >
      {icon ?? <WarningAmberOutlined aria-hidden />}
      <span>{children}</span>
    </Box>
  )
}
