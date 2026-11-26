import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  { ignores: ['dist', 'src/api'] },
  {
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    files: ['**/*.{ts,tsx}'],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
    },
  },
  {
    // A monitor or channel type's form files export only components: their schema, defaults
    // and codec live in schema.ts and codec.ts (docs/adr/0004-web-form-conventions.md)
    files: ['src/app/**/monitor-types/**/*.tsx', 'src/app/**/channel-types/**/*.tsx'],
    rules: {
      'react-refresh/only-export-components': 'error',
    },
  },
)
