// SPDX-License-Identifier: Apache-2.0
import MuiTooltip, { type TooltipProps } from '@mui/material/Tooltip'
import { useEffect, useRef, useState, type SyntheticEvent } from 'react'

/** Hover waits this long; keyboard focus shows the tooltip at once (docs/design.md § Components). */
const hoverDelayMs = 800

/** MUI Tooltip with the design-system timing. Use it instead of @mui/material/Tooltip. */
export function Tooltip(props: Omit<TooltipProps, 'open' | 'enterDelay' | 'enterNextDelay'>) {
  const { onOpen, onClose, ...rest } = props
  const [open, setOpen] = useState(false)
  const timer = useRef<number | undefined>(undefined)
  useEffect(() => () => window.clearTimeout(timer.current), [])

  const show = (event: SyntheticEvent) => {
    window.clearTimeout(timer.current)
    const reveal = () => {
      setOpen(true)
      onOpen?.(event)
    }
    if (event.type.startsWith('mouse')) timer.current = window.setTimeout(reveal, hoverDelayMs)
    else reveal()
  }
  const hide = (event: Event | SyntheticEvent) => {
    window.clearTimeout(timer.current)
    setOpen(false)
    onClose?.(event)
  }
  // MUI doesn't report a leave while the tooltip is still closed. It forwards
  // this prop to the child next to its own handler, so the pending hover
  // timer is cancelled here.
  const cancelPending = () => window.clearTimeout(timer.current)

  return (
    <MuiTooltip
      {...rest}
      open={open}
      enterDelay={0}
      enterNextDelay={0}
      onOpen={show}
      onClose={hide}
      onMouseLeave={cancelPending}
    />
  )
}
