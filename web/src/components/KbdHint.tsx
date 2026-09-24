// SPDX-License-Identifier: Apache-2.0
import Box from '@mui/material/Box'
import type { SxProps, Theme } from '@mui/material/styles'

/** True on Apple platforms, where "Mod" is ⌘ rather than Ctrl. */
function isApple(): boolean {
  if (typeof navigator === 'undefined') return false
  const nav = navigator as Navigator & { userAgentData?: { platform?: string } }
  const platform = nav.userAgentData?.platform ?? nav.platform ?? ''
  return /mac|iphone|ipad|ipod/i.test(platform)
}

export interface KbdHintProps {
  /**
   * The keys pressed together, e.g. `['Mod', 'K']` or `['Esc']`. `Mod` shows
   * as ⌘ on Apple platforms and Ctrl elsewhere.
   */
  keys: string[]
  sx?: SxProps<Theme>
}

/** Keyboard shortcut hint in mono (`.kbar kbd` in design/preview.html). */
export function KbdHint({ keys, sx }: KbdHintProps) {
  const apple = isApple()
  const shown = keys.map((k) => (k === 'Mod' ? (apple ? '⌘' : 'Ctrl') : k))
  return (
    <Box
      component="kbd"
      sx={[
        {
          display: 'inline-flex',
          alignItems: 'center',
          gap: 'var(--space-3xs)',
          fontFamily: 'var(--font-mono)',
          fontSize: 'var(--text-xs)',
          fontWeight: 500,
          lineHeight: 1,
          color: 'var(--color-neutral)',
          whiteSpace: 'nowrap',
        },
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
    >
      {shown.map((k, i) => (
        <Box
          key={i}
          component="kbd"
          sx={{
            font: 'inherit',
            border: 'var(--rule-hair) solid var(--color-rule-2)',
            borderRadius: 'var(--radius-chip)',
            paddingBlock: 'var(--space-3xs)',
            paddingInline: 'var(--space-2xs)',
            // A lone ⌘ is narrower than Ctrl; keep single glyphs square-ish.
            minInlineSize: '1.5em',
            textAlign: 'center',
          }}
        >
          {k}
        </Box>
      ))}
    </Box>
  )
}
