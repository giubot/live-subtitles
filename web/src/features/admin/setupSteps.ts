// SPDX-License-Identifier: Apache-2.0
import type { Schemas } from '../../api/types'

/** The setup wizard's steps, in order (P3-06). Language and theme share the PIN step. */
export const setupSteps = ['pin', 'hardware', 'models', 'google', 'session', 'done'] as const
export type SetupStep = (typeof setupSteps)[number]

/** The local provider's sidecars that don't answer; the benchmark and local sessions need both. */
export function localRuntimesMissing(report?: Schemas['HardwareReport']) {
  if (!report) return []
  return (['whisper', 'ollama'] as const).filter((n) => !report.runtimes[n].reachable)
}
