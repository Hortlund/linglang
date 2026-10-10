const vscode = require('vscode');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');

async function until(check) {
  const deadline = Date.now() + 10000;
  while (Date.now() < deadline) {
    if (check()) return;
    await new Promise(resolve => setTimeout(resolve, 50));
  }
  throw new Error('Editor diagnostics timed out');
}

async function runEditorTests() {
  const dir = await fs.mkdtemp(path.join(vscode.workspace.workspaceFolders[0].uri.fsPath, 'linglang-editor-'));
  const uri = vscode.Uri.file(path.join(dir, '雪.lang'));
  try {
    const marker = path.join(dir, '.linglang-standalone');
    await fs.writeFile(marker, '');
    await fs.writeFile(path.join(dir, 'unrelated.lang'), 'package main\nfunc main(){}\n');
    await fs.writeFile(uri.fsPath, 'package main\r\ntype Record struct{n int}\r\nfunc main(){println("雪😀",missing)}\r\n');
    const doc = await vscode.workspace.openTextDocument(uri);
    assert.equal(doc.languageId, 'linglang');
    await vscode.extensions.getExtension('linglang.linglang').activate();
    await until(() => vscode.languages.getDiagnostics(uri).some(d => d.message.includes('undefined: missing')));
    const issue = vscode.languages.getDiagnostics(uri).find(d => d.message.includes('undefined: missing'));
    assert.equal(vscode.languages.getDiagnostics(uri).length, 1, 'standalone files must not merge sibling declarations');
    assert.equal(issue.range.start.line, 2);
    assert.equal(issue.range.start.character, doc.lineAt(2).text.indexOf('missing'));
    const symbols = await vscode.commands.executeCommand('vscode.executeDocumentSymbolProvider', uri);
    assert.ok(symbols.some(s => s.name === 'Record' && s.children.some(c => c.name === 'n')));
    assert.ok(symbols.some(s => s.name === 'main'));
    const edits = await vscode.commands.executeCommand('vscode.executeFormatDocumentProvider', uri, { tabSize: 4, insertSpaces: false });
    assert.ok(edits.length > 0);
    let formatted = doc.getText();
    // VS Code may minimize the server's full-file edit into several edits.
    for (const edit of [...edits].sort((a, b) => doc.offsetAt(b.range.start) - doc.offsetAt(a.range.start))) {
      formatted = formatted.slice(0, doc.offsetAt(edit.range.start)) + edit.newText + formatted.slice(doc.offsetAt(edit.range.end));
    }
    assert.ok(formatted.includes('func main() {'));
    const edit = new vscode.WorkspaceEdit();
    edit.replace(uri, new vscode.Range(0, 0, doc.lineCount - 1, doc.lineAt(doc.lineCount - 1).text.length), 'package main\nfunc main(){println(42)}\n');
    await vscode.workspace.applyEdit(edit);
    await until(() => vscode.languages.getDiagnostics(uri).length === 0);
    assert.ok((await fs.readFile(uri.fsPath, 'utf8')).includes('missing'), 'unsaved changes must not rewrite disk');
    const invalidReturn = new vscode.WorkspaceEdit();
    invalidReturn.replace(uri, new vscode.Range(0, 0, doc.lineCount - 1, doc.lineAt(doc.lineCount - 1).text.length), 'package main\nfunc bad() float64 { return 1.5 }\nfunc main(){}\n');
    await vscode.workspace.applyEdit(invalidReturn);
    await until(() => vscode.languages.getDiagnostics(uri).some(d => d.message.includes('unsupported return type float64')));
    await fs.rm(marker);
    await until(() => vscode.languages.getDiagnostics(uri).some(d => d.message.includes('main redeclared')));
    await fs.writeFile(marker, '');
    await until(() => {
      const issues = vscode.languages.getDiagnostics(uri);
      return issues.length === 1 && issues[0].message.includes('unsupported return type float64');
    });
    console.log('linglang extension: activation, Unicode diagnostics, outline, formatting, unsaved updates, standalone boundaries, and return types passed');
  } finally {
    await vscode.commands.executeCommand('workbench.action.closeAllEditors');
    await fs.rm(dir, { recursive: true, force: true });
  }
}

async function run() {
  try {
    await runEditorTests();
    if (process.env.LINGLANG_EDITOR_TEST_RESULT) await fs.writeFile(process.env.LINGLANG_EDITOR_TEST_RESULT, JSON.stringify({ ok: true }));
  } catch (error) {
    if (process.env.LINGLANG_EDITOR_TEST_RESULT) await fs.writeFile(process.env.LINGLANG_EDITOR_TEST_RESULT, JSON.stringify({ ok: false, error: error.stack }));
    throw error;
  }
}

module.exports = { run };
