// SPDX-License-Identifier: Apache-2.0
import js from '@eslint/js'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import globals from 'globals'
import tseslint from 'typescript-eslint'

// Attributes whose value a person reads or hears, so it must come from i18n.
const textAttributes = new Set([
  'aria-label',
  'aria-description',
  'aria-roledescription',
  'alt',
  'title',
  'placeholder',
  'label',
  'helperText',
])
const hasWords = (s) => /\p{L}{2,}/u.test(s)
const isAriaHidden = (el) =>
  el.openingElement.attributes.some(
    (a) =>
      a.type === 'JSXAttribute' &&
      a.name.name === 'aria-hidden' &&
      (a.value == null || a.value.expression?.value === true || a.value.value === 'true'),
  )

// UI-1: user-facing text comes from i18n. A small local rule instead of
// eslint-plugin-react's jsx-no-literals, which would add a dependency and
// needs a long allowlist. Text inside aria-hidden elements (brand marks) and
// path-like attribute values (`/…`, URLs) are allowed.
const i18nPlugin = {
  rules: {
    'no-literal-text': {
      meta: { type: 'problem', schema: [] },
      create(context) {
        const report = (node) =>
          context.report({ node, message: 'Move user-facing text into i18n (t(...)).' })
        return {
          JSXText(node) {
            if (!hasWords(node.value)) return
            for (let p = node.parent; p && p.type === 'JSXElement'; p = p.parent) {
              if (isAriaHidden(p)) return
            }
            report(node)
          },
          JSXAttribute(node) {
            if (!textAttributes.has(node.name.name) || node.value == null) return
            const v = node.value
            const s =
              v.type === 'Literal'
                ? v.value
                : v.type === 'JSXExpressionContainer' && v.expression.type === 'Literal'
                  ? v.expression.value
                  : v.type === 'JSXExpressionContainer' &&
                      v.expression.type === 'TemplateLiteral' &&
                      v.expression.expressions.length === 0
                    ? v.expression.quasis[0].value.cooked
                    : undefined
            if (typeof s === 'string' && hasWords(s) && !s.includes('/')) report(node)
          },
        }
      },
    },
  },
}

export default tseslint.config(
  { ignores: ['dist', 'src/routeTree.gen.ts', 'src/api/schema.d.ts'] },
  {
    files: ['**/*.{ts,tsx,js,mjs}'],
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    languageOptions: { globals: { ...globals.browser, ...globals.node } },
    plugins: { 'react-hooks': reactHooks, 'react-refresh': reactRefresh },
    rules: {
      ...reactHooks.configs.recommended.rules,
      'react-refresh/only-export-components': 'warn',
    },
  },
  {
    // Route files export `Route` next to their components; the router plugin
    // splits them for HMR.
    files: ['src/routes/**'],
    rules: { 'react-refresh/only-export-components': 'off' },
  },
  {
    files: ['src/**/*.tsx'],
    // The design catalogue shows sample data; tests assert on literal text.
    ignores: ['src/routes/_themed/dev/**', 'src/**/*.test.tsx', 'src/test/**'],
    plugins: { i18n: i18nPlugin },
    rules: { 'i18n/no-literal-text': 'error' },
  },
)
