// SPDX-License-Identifier: Apache-2.0
import { useEffect } from 'react'

/**
 * Keeps the screen on while `active` (AUD-2), so the capture computer
 * doesn't sleep mid-talk. The browser drops the lock when the page is
 * hidden, so it's requested again when the page comes back. Browsers
 * without the Screen Wake Lock API are left alone.
 */
export function useWakeLock(active: boolean): void {
  useEffect(() => {
    if (!active || !('wakeLock' in navigator)) return
    let lock: WakeLockSentinel | undefined
    let done = false
    const request = async () => {
      if (document.visibilityState !== 'visible' || (lock && !lock.released)) return
      try {
        const l = await navigator.wakeLock.request('screen')
        if (done) void l.release()
        else lock = l
      } catch {
        // Denied (battery saver, policy): capture works without it.
      }
    }
    void request()
    document.addEventListener('visibilitychange', request)
    return () => {
      done = true
      document.removeEventListener('visibilitychange', request)
      void lock?.release()
    }
  }, [active])
}
