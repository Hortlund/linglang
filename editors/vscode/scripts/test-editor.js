const { spawnSync } = require('node:child_process');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

// Use a native VS Code executable, rather than a CLI that may forward to an
// existing editor. Every run gets its own profile and extension directory.
const executable = process.argv[2] || process.env.VSCODE_EXECUTABLE ||
  (process.platform === 'darwin' ? '/Applications/Visual Studio Code.app/Contents/MacOS/Code' : '');
if (!executable || process.argv.length > 3) {
  throw new Error('Usage: npm run test:editor -- <native VS Code executable>');
}
const extension = path.resolve(__dirname, '..');
if (!fs.existsSync(path.join(extension, 'dist/extension.js'))) {
  throw new Error('Build the host package first: npm run package');
}
const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'linglang-vscode-test-'));
const report = path.join(directory, 'result.json');
const env = { ...process.env, LINGLANG_EDITOR_TEST_RESULT: report };
delete env.ELECTRON_RUN_AS_NODE;
const result = spawnSync(executable, [
  '--new-window', '--disable-workspace-trust', '--skip-welcome', '--skip-release-notes',
  '--user-data-dir', path.join(directory, 'profile'),
  '--extensions-dir', path.join(directory, 'extensions'),
  `--extensionDevelopmentPath=${extension}`,
  `--extensionTestsPath=${path.join(extension, 'test/extension-host.js')}`,
  directory,
], { env, encoding: 'utf8', timeout: 120000, maxBuffer: 8 * 1024 * 1024 });
let passed = false;
try {
  const data = JSON.parse(fs.readFileSync(report, 'utf8'));
  passed = !result.error && result.status === 0 && data.ok === true;
  if (!passed) console.error(data.error || result.error || `VS Code exited ${result.status}`);
} catch (error) {
  console.error(result.error || error);
}
if (passed) {
  fs.rmSync(directory, { recursive: true, force: true });
  console.log('VS Code: activation, Unicode diagnostics, outline, formatting, unsaved updates, standalone boundaries, and return types passed');
} else {
  console.error(result.stdout, result.stderr);
  console.error(`Test profile and logs: ${directory}`);
  process.exitCode = 1;
}
