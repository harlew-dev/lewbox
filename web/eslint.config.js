import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import tseslint from 'typescript-eslint'

// Page URLs are built by src/lib/routes.ts. The route registry (app.tsx) and
// the sign-in return-path allowlist (lib/next.ts) are the only places that
// may name a page path themselves.
const pagePrefixes = String.raw`^\/(admin|sandboxes|snapshots|keys|usage|plan|settings|onboarding|login|signup|setup|account|docs|platform|users|servers|clusters|nodes|organizations|team)(\/|$|\?)`

export default tseslint.config(
  { ignores: ['dist/**', 'node_modules/**', 'vendor/**'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  reactHooks.configs['flat/recommended'],
  {
    files: ['src/**/*.{ts,tsx}'],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: 'module',
      globals: { ...globals.browser },
    },
    rules: {
      // TypeScript already reports both; the core rules do not understand types.
      'no-undef': 'off',
      'no-unused-vars': 'off',
      '@typescript-eslint/no-unused-vars': [
        'error',
        { argsIgnorePattern: '^_', varsIgnorePattern: '^_' },
      ],
    },
  },
  {
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['src/lib/api/client.ts'],
    rules: {
      'no-restricted-globals': [
        'error',
        {
          name: 'fetch',
          message: 'HTTP calls live only in src/lib/api/client.ts; add a method there and call it.',
        },
        {
          name: 'XMLHttpRequest',
          message: 'HTTP calls live only in src/lib/api/client.ts; add a method there and call it.',
        },
      ],
    },
  },
  {
    files: ['src/**/*.{ts,tsx}'],
    ignores: [
      'src/app.tsx',
      'src/lib/routes.ts',
      'src/lib/next.ts',
      'src/**/*.test.ts',
      'src/**/*.test.tsx',
    ],
    rules: {
      'no-restricted-syntax': [
        'error',
        {
          selector: `Literal[value=/${pagePrefixes}/]`,
          message: 'Build page URLs with src/lib/routes.ts, not a literal path.',
        },
        {
          selector: `TemplateElement[value.raw=/${pagePrefixes}/]`,
          message: 'Build page URLs with src/lib/routes.ts, not a literal path.',
        },
      ],
    },
  },
)
