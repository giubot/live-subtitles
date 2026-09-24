// SPDX-License-Identifier: Apache-2.0
import { useCallback, useLayoutEffect, useRef, useState, type RefObject } from 'react'

/** How close to the bottom (px) still counts as following live. */
const slack = 48

/**
 * Auto-scroll for a live log (OUT-2): while the reader is at the bottom,
 * new content keeps it there; once they scroll up to reread, it stays put
 * and `atBottom` turns false so the page can offer "jump to live".
 * `content` is anything that changes when the log grows.
 */
export function useStickToBottom(
  ref: RefObject<HTMLElement | null>,
  content: unknown,
): { atBottom: boolean; onScroll: () => void; jumpToLive: () => void } {
  const [atBottom, setAtBottom] = useState(true)
  const follow = useRef(true)

  useLayoutEffect(() => {
    const el = ref.current
    if (el && follow.current) el.scrollTop = el.scrollHeight
  }, [ref, content])

  const onScroll = useCallback(() => {
    const el = ref.current
    if (!el) return
    const near = el.scrollHeight - el.scrollTop - el.clientHeight <= slack
    follow.current = near
    setAtBottom(near)
  }, [ref])

  const jumpToLive = useCallback(() => {
    const el = ref.current
    follow.current = true
    setAtBottom(true)
    if (el) el.scrollTop = el.scrollHeight
  }, [ref])

  return { atBottom, onScroll, jumpToLive }
}
