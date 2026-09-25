// SPDX-License-Identifier: Apache-2.0
import { outlineShadow, vh, type LengthUnit, type OverlayLook } from './overlayStyle'

const lineHeight = 1.3

export interface OverlayLine {
  id: string
  text: string
  lang?: string
}

export interface OverlayCaptionsProps {
  look: OverlayLook
  lines: OverlayLine[]
  visible: boolean
  /**
   * How px at 1080p become CSS lengths: `vh` on the overlay page, `cqw`
   * inside a 16:9 preview frame.
   */
  unit?: LengthUnit
  /** `fixed` fills the browser source; `absolute` fills a preview frame. */
  placement?: 'fixed' | 'absolute'
  /** Announce new lines (the live overlay); previews stay quiet. */
  live?: boolean
}

/**
 * The overlay's caption box: a window of `maxLines` lines, bottom-aligned,
 * so a long sentence shows its end. Plain elements and inline styles only,
 * so the overlay page stays MUI-free and the admin preview looks the same.
 */
export function OverlayCaptions({
  look,
  lines,
  visible,
  unit = vh,
  placement = 'fixed',
  live = true,
}: OverlayCaptionsProps) {
  const boxed = look.background !== 'transparent'
  const padBlock = boxed ? 0.15 : 0
  const padInline = boxed ? 0.4 : 0

  return (
    <div
      data-overlay
      style={{
        position: placement,
        inset: 0,
        overflow: 'hidden',
        display: 'flex',
        flexDirection: 'column',
        justifyContent: look.position === 'top' ? 'flex-start' : 'flex-end',
        alignItems: { left: 'flex-start', center: 'center', right: 'flex-end' }[look.align],
        padding: `${unit(look.marginPx)} ${unit(look.marginPx * 1.6)}`,
        pointerEvents: 'none',
        background: 'transparent',
      }}
    >
      <div
        aria-live={live ? 'polite' : undefined}
        style={{
          maxInlineSize: '42ch',
          maxBlockSize: `calc(${look.maxLines * lineHeight}em + ${padBlock * 2}em)`,
          overflow: 'hidden',
          display: 'flex',
          flexDirection: 'column',
          justifyContent: 'flex-end',
          fontFamily: 'var(--font-body)',
          fontSize: unit(look.fontSizePx),
          fontWeight: look.fontWeight,
          lineHeight,
          textAlign: look.align,
          color: look.color,
          textShadow: outlineShadow(look.outlineWidthPx, look.outlineColor, unit),
          opacity: visible ? 1 : 0,
          transition: 'opacity var(--dur-long) var(--ease-out)',
        }}
      >
        {lines.map((c) => (
          <p key={c.id} lang={c.lang} style={{ margin: 0 }}>
            <span
              style={{
                backgroundColor: look.background,
                padding: boxed ? `${padBlock}em ${padInline}em` : undefined,
                borderRadius: boxed ? 'var(--radius-chip)' : undefined,
                boxDecorationBreak: 'clone',
                WebkitBoxDecorationBreak: 'clone',
              }}
            >
              {c.text}
            </span>
          </p>
        ))}
      </div>
    </div>
  )
}
