/**
 * Commit-message linting for tusk.
 *
 * Extends the conventional-commits config; tightens the header length
 * and subject case to match the project's existing strictness.
 */
export default {
  extends: ['@commitlint/config-conventional'],
  rules: {
    'header-max-length': [2, 'always', 89],
    'subject-case': [2, 'always', 'lower-case'],
  },
};
