# Packaging and publishing

Build the package (works on any machine with Node 20+):

    npm ci
    npm run prepackage      # copies the repo's LICENSE in
    npm run package         # writes reminal-<version>.vsix

The same `.vsix` goes to both registries, so what is tested is what is
published. Before the first publish:

1. The publisher is `reminal` on the VS Code Marketplace
   (https://marketplace.visualstudio.com/manage), and the same name is needed
   as a namespace on Open VSX (https://open-vsx.org). `publisher` in
   `package.json` must match it.
2. Get a Marketplace personal access token (Azure DevOps, organization "All
   accessible organizations", scope "Marketplace: Manage") and an Open VSX
   access token.

Then, from the `.vsix` built above:

    export VSCE_PAT=<marketplace token> OVSX_PAT=<open vsx token>
    npx vsce publish --packagePath reminal-<version>.vsix
    npx ovsx publish reminal-<version>.vsix

The tokens go in the environment rather than on the command line, so they
stay out of shell history. The `reminal` namespace on Open VSX exists.

VS Code installs from the Marketplace. Cursor, Windsurf, VSCodium and
code-server install from Open VSX.

## Testing

    scripts/vscode-test/run.sh

Runs the unit tests and a real code-server with the extension, a real reminal
and a local relay, all in Docker, and drives it with Playwright: the profile,
the title-bar button, the right-click item, the web viewer reaching each
session, and the error when reminal is missing. Screenshots land in
`scripts/vscode-test/out/`.
