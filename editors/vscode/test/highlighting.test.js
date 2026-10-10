const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { Registry, parseRawGrammar } = require('vscode-textmate');
const { loadWASM, OnigScanner, OnigString } = require('vscode-oniguruma');

const root = path.resolve(__dirname, '..');
const file = path.join(root, 'syntaxes/linglang.tmLanguage.json');
const grammar = JSON.parse(fs.readFileSync(file, 'utf8'));

test('grammar highlights real language constructs and preserves string/comment state', async () => {
  const wasm = fs.readFileSync(require.resolve('vscode-oniguruma/release/onig.wasm'));
  await loadWASM(wasm.buffer.slice(wasm.byteOffset, wasm.byteOffset + wasm.byteLength));
  const registry = new Registry({
    onigLib: Promise.resolve({ createOnigScanner: patterns => new OnigScanner(patterns), createOnigString: text => new OnigString(text) }),
    loadGrammar: async () => parseRawGrammar(JSON.stringify(grammar), file),
  });
  const loaded = await registry.loadGrammar('source.linglang');
  let state = null;
  function scopes(line, text) {
    const result = loaded.tokenizeLine(line, state);
    state = result.ruleStack;
    const start = line.indexOf(text);
    return result.tokens.find(token => token.startIndex <= start && token.endIndex > start).scopes;
  }
  assert.ok(scopes('func 雪(x int) {', '雪').includes('entity.name.function.linglang'));
  assert.ok(scopes('type Record struct { n int }', 'Record').includes('entity.name.type.linglang'));
  assert.ok(scopes('xs := List[int]{0x_FF}', 'List').includes('support.type.linglang'));
  assert.ok(scopes('println(xs)', 'println').includes('support.function.builtin.linglang'));
  for (const name of ['sourceFiles', 'buildProgram', 'runProgram']) {
    assert.ok(scopes(`${name}()`, name).includes('support.function.builtin.linglang'), name);
  }
  assert.ok(scopes('var files FilesResult', 'FilesResult').includes('support.type.linglang'));
  assert.ok(scopes('n := 0b_101', '0b_101').includes('constant.numeric.integer.linglang'));
  assert.ok(scopes('// println(List[int]{1})', 'println').includes('comment.line.double-slash.linglang'));
  assert.ok(scopes('s := "println\\\"List"', 'println').includes('string.quoted.double.linglang'));
  assert.ok(scopes('s := `println', 'println').includes('string.quoted.raw.linglang'));
  assert.ok(scopes('List[int]`', 'List').includes('string.quoted.raw.linglang'));
  assert.ok(scopes('/* println', 'println').includes('comment.block.linglang'));
  assert.ok(scopes('List[int] */', 'List').includes('comment.block.linglang'));
  registry.dispose();
});

test('builtin function and type highlighting stays synchronized with compiler prelude', () => {
  const repo = path.resolve(root, '../..');
  const declarations = ['processes.go', 'standard.go', 'maps.go'].map(name => {
    const source = fs.readFileSync(path.join(repo, 'internal/compiler', name), 'utf8');
    return source.match(/const \w+Prelude = `([^`]+)`/)[1];
  }).join('\n');
  const pattern = new RegExp(grammar.repository.builtins.patterns[0].match);
  for (const match of declarations.matchAll(/^func (\w+)(?:\[|\()/gm)) {
    assert.ok(pattern.test(match[1]), `Missing builtin function highlighting: ${match[1]}`);
  }
  const typePattern = new RegExp(grammar.repository.types.patterns[0].match);
  // Some result types share a line, separated by semicolons.
  for (const match of declarations.matchAll(/(?:^|;\s*)type (\w+)(?:\[|\s)/gm)) {
    assert.ok(typePattern.test(match[1]), `Missing builtin type highlighting: ${match[1]}`);
  }
  for (const name of ['callIdentifier', 'PreludeSource', 'supportedMapKey']) assert.ok(!pattern.test(name), name);
});
