/**
 * Commit-message linting for tusk.
 *
 * Extends the conventional-commits config; tightens the header length
 * and subject case to match the project's existing strictness, and
 * relaxes body / footer line-length rules — squash-merges of stacked
 * PRs aggregate child commit subjects into the merge body, where lines
 * routinely exceed the conventional-commits 100-char default.
 */
export default {
  extends: ['@commitlint/config-conventional'],
  rules: {
    'header-max-length': [2, 'always', 89],
    'subject-case': [2, 'always', 'lower-case'],
    'body-max-line-length': [0, 'always'],
    'footer-max-line-length': [0, 'always'],
  },
};
