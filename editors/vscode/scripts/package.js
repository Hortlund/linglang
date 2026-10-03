const { spawnSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const { buildSync } = require('esbuild');

const root = path.resolve(__dirname, '../../..');
const extension = path.resolve(__dirname, '..');
const targets = {
  'darwin-arm64': ['darwin', 'arm64'], 'darwin-x64': ['darwin', 'amd64'],
  'linux-arm64': ['linux', 'arm64'], 'linux-x64': ['linux', 'amd64'],
  'win32-arm64': ['windows', 'arm64'], 'win32-x64': ['windows', 'amd64'],
};
const target = process.argv[2] || `${process.platform}-${process.arch}`;
if (!targets[target] || process.argv.length > 3) {
  throw new Error(`Usage: npm run package -- <${Object.keys(targets).join('|')}>`);
}
const [goOS, goArch] = targets[target];
const server = path.join(extension, 'server');
buildSync({
  entryPoints: [path.join(extension, 'extension.js')],
  bundle: true, platform: 'node', format: 'cjs', target: 'node18',
  external: ['vscode'], outfile: path.join(extension, 'dist/extension.js'),
});
const thirdParty = ['vscode-languageclient', 'vscode-jsonrpc', 'vscode-languageserver-protocol', 'vscode-languageserver-types', 'semver'];
const notices = thirdParty.map(name => {
  let directory = path.dirname(require.resolve(name));
  while (!fs.existsSync(path.join(directory, 'package.json'))) {
    const parent = path.dirname(directory);
    if (parent === directory) throw new Error(`Cannot locate bundled dependency: ${name}`);
    directory = parent;
  }
  const license = ['LICENSE', 'License.txt', 'LICENSE.txt', 'LICENSE.md'].find(file => fs.existsSync(path.join(directory, file)));
  if (!license) throw new Error(`Missing bundled dependency license: ${name}`);
  return `${name}\n${fs.readFileSync(path.join(directory, license), 'utf8')}`;
});
fs.writeFileSync(path.join(extension, 'dist/THIRD_PARTY_NOTICES.txt'), notices.join('\n\n'));
fs.mkdirSync(server, { recursive: true });
// A package contains exactly one server for its advertised platform.
for (const name of ['linglang', 'linglang.exe']) fs.rmSync(path.join(server, name), { force: true });
const executable = path.join(server, goOS === 'windows' ? 'linglang.exe' : 'linglang');
function run(command, args, options = {}) {
  const result = spawnSync(command, args, { stdio: 'inherit', ...options });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status || 1);
}
run('go', ['build', '-trimpath', '-o', executable, './cmd/linglang'], {
  cwd: root, env: { ...process.env, CGO_ENABLED: '0', GOOS: goOS, GOARCH: goArch },
});
const output = path.join(root, 'dist', `linglang-vscode-${target}.vsix`);
fs.mkdirSync(path.dirname(output), { recursive: true });
run(process.execPath, [require.resolve('@vscode/vsce/vsce'), 'package', '--no-dependencies', '--skip-license', '--target', target, '--out', output], { cwd: extension });
