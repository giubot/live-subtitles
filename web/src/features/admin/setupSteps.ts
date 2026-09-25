// SPDX-License-Identifier: Apache-2.0

/** The setup wizard's steps, in order (P3-06). Language and theme share the PIN step. */
export const setupSteps = ['pin', 'hardware', 'models', 'google', 'session', 'done'] as const
export type SetupStep = (typeof setupSteps)[number]
