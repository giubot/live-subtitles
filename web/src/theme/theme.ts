// SPDX-License-Identifier: Apache-2.0
import type { ThemeOptions } from '@mui/material/styles'
import { dark, light, type Palette } from './palette'

/**
 * MUI theme built from the design tokens (docs/design.md § Components).
 * Colours come from palette.ts (the hex mirror of tokens.css); everything
 * else references the CSS variables in tokens.css directly.
 */

/** Where the Light / Dark / System choice is stored per device (UI-5). */
export const themeStorageKey = 'ls.theme'
export const colorSchemeStorageKey = 'ls.theme-scheme'

const focusRing = {
  outline: 'var(--rule-fine) solid var(--color-focus)',
  outlineOffset: 'var(--rule-fine)',
  transition: 'none', // the ring appears instantly
}

const display = {
  fontFamily: 'var(--font-display)',
  fontWeight: 'var(--weight-display)',
  letterSpacing: 'var(--tracking-display)',
  lineHeight: 'var(--leading-tight)',
  color: 'var(--color-ink)',
} as const

function palette(p: Record<keyof Palette, string>): NonNullable<ThemeOptions['palette']> {
  return {
    primary: { main: p.accent, dark: p.accentHover, contrastText: p.accentInk },
    // Secondary = neutral ink: outlined secondary buttons read as ink text on a control border.
    secondary: { main: p.ink, contrastText: p.paper },
    error: { main: p.danger, contrastText: p.paper },
    warning: { main: p.warn, contrastText: p.paper },
    success: { main: p.ok, contrastText: p.paper },
    info: { main: p.accent, contrastText: p.accentInk },
    background: { default: p.paper, paper: p.paper },
    text: { primary: p.ink, secondary: p.neutral, disabled: p.muted },
    divider: p.rule,
    action: { hover: p.paper3, selected: p.accentSoft },
  }
}

export const themeOptions: ThemeOptions = {
  cssVariables: { colorSchemeSelector: '[data-theme="%s"]' },
  colorSchemes: {
    light: { palette: palette(light) },
    dark: { palette: palette(dark) },
  },
  shape: { borderRadius: 6 },
  typography: {
    fontFamily: 'var(--font-body)',
    fontWeightRegular: 400,
    fontWeightMedium: 500,
    fontWeightBold: 700,
    h1: { ...display, fontSize: 'var(--text-2xl)' },
    h2: { ...display, fontSize: 'var(--text-xl)' },
    h3: { ...display, fontSize: 'var(--text-lg)' },
    h4: { ...display, fontSize: 'var(--text-lg)' },
    h5: { ...display, fontSize: 'var(--text-md)' },
    h6: { ...display, fontSize: 'var(--text-md)' },
    subtitle1: { fontSize: 'var(--text-base)', fontWeight: 'var(--weight-strong)' },
    subtitle2: { fontSize: 'var(--text-sm)', fontWeight: 'var(--weight-strong)' },
    body1: {
      fontSize: 'var(--text-base)',
      lineHeight: 'var(--leading-body)',
      fontWeight: 'var(--weight-body)',
    },
    body2: {
      fontSize: 'var(--text-sm)',
      lineHeight: 'var(--leading-body)',
      fontWeight: 'var(--weight-body)',
    },
    caption: { fontSize: 'var(--text-sm)', lineHeight: 'var(--leading-body)' },
    // Mono uppercase label: status chips, table headers, kbd hints.
    overline: {
      fontFamily: 'var(--font-mono)',
      fontSize: 'var(--text-xs)',
      fontWeight: 500,
      letterSpacing: 'var(--tracking-label)',
      lineHeight: 'var(--leading-tight)',
      textTransform: 'uppercase',
    },
    button: { fontWeight: 'var(--weight-strong)', textTransform: 'none', letterSpacing: 0 },
  },
  components: {
    MuiCssBaseline: {
      styleOverrides: {
        body: {
          color: 'var(--color-ink-2)',
          fontWeight: 'var(--weight-body)',
          fontVariantNumeric: 'tabular-nums',
        },
        ':focus-visible': focusRing,
        '::selection': { background: 'var(--color-accent-soft)', color: 'var(--color-ink)' },
      },
    },
    // No ripple: states are drawn by the design system, not Material ink.
    MuiButtonBase: {
      defaultProps: { disableRipple: true },
      styleOverrides: { root: { '&.Mui-focusVisible': focusRing } },
    },
    MuiButton: {
      defaultProps: { disableElevation: true },
      styleOverrides: {
        root: {
          minHeight: 'var(--control-height)',
          borderRadius: 'var(--radius-input)',
          paddingInline: 'var(--space-md)',
          transition:
            'background-color var(--dur-micro) var(--ease-out), border-color var(--dur-micro) var(--ease-out), color var(--dur-micro) var(--ease-out), transform var(--dur-micro) var(--ease-out)',
          '&:active': { transform: 'translateY(1px)' },
          '&.Mui-disabled': { opacity: 0.5 },
          '&.MuiButton-loading': { opacity: 1 },
          // Disabled keeps the variant's colours and fades to 50 % (docs/design.md
          // § States); MUI's grey would fade it twice. Loading stays at full
          // opacity so the label reads.
          variants: [
            {
              props: { variant: 'contained' },
              style: {
                '&.Mui-disabled': {
                  color: 'var(--variant-containedColor)',
                  backgroundColor: 'var(--variant-containedBg)',
                },
              },
            },
            {
              props: { variant: 'outlined' },
              style: {
                '&.Mui-disabled': {
                  color: 'var(--variant-outlinedColor)',
                  border: 'var(--rule-hair) solid var(--color-control)',
                },
              },
            },
            {
              props: { variant: 'text' },
              style: { '&.Mui-disabled': { color: 'var(--variant-textColor)' } },
            },
            {
              props: { variant: 'contained', color: 'primary' },
              style: {
                '@media (hover: hover)': {
                  '&:hover': { backgroundColor: 'var(--color-accent-hover)' },
                },
              },
            },
          ],
        },
        outlined: {
          borderColor: 'var(--color-control)',
          '@media (hover: hover)': {
            '&:hover': {
              borderColor: 'var(--color-control)',
              backgroundColor: 'var(--color-paper-3)',
            },
          },
        },
      },
    },
    MuiIconButton: {
      styleOverrides: { root: { borderRadius: 'var(--radius-input)' } },
    },
    MuiPaper: {
      defaultProps: { elevation: 0 },
      styleOverrides: {
        root: { backgroundImage: 'none' },
        outlined: { borderColor: 'var(--color-rule-2)', borderRadius: 'var(--radius-card)' },
        // Menus, popovers, dialogs: the only lifted surfaces.
        elevation: {
          border: 'var(--rule-hair) solid var(--color-rule-2)',
          boxShadow: 'var(--shadow-lift)',
        },
      },
    },
    MuiCard: {
      defaultProps: { variant: 'outlined' },
      styleOverrides: { root: { borderRadius: 'var(--radius-card)' } },
    },
    MuiDialog: {
      styleOverrides: { paper: { borderRadius: 'var(--radius-card)' } },
    },
    MuiAppBar: {
      defaultProps: { elevation: 0, color: 'inherit', position: 'sticky' },
      styleOverrides: {
        root: {
          backgroundColor: 'var(--color-paper)',
          color: 'var(--color-ink)',
          borderBottom: 'var(--rule-hair) solid var(--color-rule)',
          zIndex: 'var(--z-sticky)',
        },
      },
    },
    MuiDrawer: {
      styleOverrides: {
        paper: {
          backgroundColor: 'var(--color-paper-2)',
          borderInlineEnd: 'var(--rule-hair) solid var(--color-rule)',
          border: 'none',
          width: 'var(--rail-width)',
        },
      },
    },
    MuiOutlinedInput: {
      styleOverrides: {
        root: {
          borderRadius: 'var(--radius-input)',
          minHeight: 'var(--control-height)',
          '& .MuiOutlinedInput-notchedOutline': { borderColor: 'var(--color-control)' },
          '&.Mui-error .MuiOutlinedInput-notchedOutline': { borderColor: 'var(--color-danger)' },
        },
      },
    },
    // Label above the field, never a placeholder standing in for it.
    MuiInputLabel: {
      defaultProps: { shrink: true },
      styleOverrides: {
        root: { color: 'var(--color-neutral)', fontWeight: 'var(--weight-strong)' },
      },
    },
    // Reserve the helper line so an error message doesn't shift the layout.
    MuiFormHelperText: {
      styleOverrides: {
        root: {
          minHeight: 'calc(var(--text-sm) * var(--leading-body))',
          marginInline: 0,
          fontSize: 'var(--text-sm)',
          color: 'var(--color-muted)',
        },
      },
    },
    MuiToggleButton: {
      styleOverrides: {
        root: {
          textTransform: 'none',
          fontWeight: 'var(--weight-strong)',
          borderColor: 'var(--color-control)',
          color: 'var(--color-ink-2)',
          minHeight: 'var(--control-height)',
          paddingInline: 'var(--space-sm)',
          '&.Mui-selected': {
            backgroundColor: 'var(--color-accent-soft)',
            color: 'var(--color-accent-text)',
          },
        },
      },
    },
    MuiToggleButtonGroup: {
      styleOverrides: { root: { borderRadius: 'var(--radius-input)' } },
    },
    MuiTooltip: {
      styleOverrides: {
        tooltip: {
          backgroundColor: 'var(--color-graphite)',
          color: 'var(--color-graphite-ink)',
          fontSize: 'var(--text-sm)',
          borderRadius: 'var(--radius-chip)',
        },
      },
    },
    MuiLink: {
      defaultProps: { underline: 'hover' },
      styleOverrides: { root: { color: 'var(--color-accent-text)' } },
    },
    MuiDivider: {
      styleOverrides: { root: { borderColor: 'var(--color-rule)' } },
    },
  },
}
