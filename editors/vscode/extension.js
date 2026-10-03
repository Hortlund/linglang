const vscode = require('vscode');
const fs = require('node:fs');
const path = require('node:path');
const { LanguageClient } = require('vscode-languageclient/node');

let client;

async function activate(context) {
  const bundled = context.asAbsolutePath(path.join('server', process.platform === 'win32' ? 'linglang.exe' : 'linglang'));
  const configured = vscode.workspace.getConfiguration('linglang').get('serverPath', '').trim();
  const command = configured || (fs.existsSync(bundled) ? bundled : 'linglang');
  const watcher = vscode.workspace.createFileSystemWatcher('**/*.lang');
  const markerWatcher = vscode.workspace.createFileSystemWatcher('**/.linglang-standalone');
  context.subscriptions.push(watcher, markerWatcher);
  client = new LanguageClient('linglang', 'linglang', { command, args: ['lsp'] }, {
    documentSelector: [{ scheme: 'file', language: 'linglang' }],
    synchronize: { fileEvents: [watcher, markerWatcher] },
  });
  try {
    await client.start();
  } catch (error) {
    vscode.window.showErrorMessage(`linglang server could not start: ${error.message}. Set linglang.serverPath to your linglang executable, then reload the window.`);
  }
}

async function deactivate() {
  if (client) await client.stop();
}

module.exports = { activate, deactivate };
