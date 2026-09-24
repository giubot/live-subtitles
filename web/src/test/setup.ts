// SPDX-License-Identifier: Apache-2.0
import '@testing-library/jest-dom/vitest'
import { cleanup, configure } from '@testing-library/react'
import { afterEach } from 'vitest'

afterEach(cleanup)

// Route components are code-split; on a cold run Vite transforms a chunk
// the first time a test visits it, which can take longer than the 1 s
// default of findBy* and waitFor.
configure({ asyncUtilTimeout: 5000 })
