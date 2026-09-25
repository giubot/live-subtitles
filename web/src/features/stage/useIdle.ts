// SPDX-License-Identifier: Apache-2.0
import { useEffect, useState } from 'react'

/**
 * True once the pointer and keyboard have been still for `ms`, e.g. to
 * hide the cursor and hints on a projector. Any movement shows them again.
 */
export function useIdle(ms: number): boolean {
  const [idle, setIdle] = useState(false)
  useEffect(() => {
    let timer = setTimeout(() => setIdle(true), ms)
    const wake = () => {
      clearTimeout(timer)
      setIdle(false)
      timer = setTimeout(() => setIdle(true), ms)
    }
    const events = ['pointermove', 'pointerdown', 'keydown'] as const
    for (const e of events) window.addEventListener(e, wake)
    return () => {
      clearTimeout(timer)
      for (const e of events) window.removeEventListener(e, wake)
    }
  }, [ms])
  return idle
}
