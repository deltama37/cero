'use strict';

const assert = require('node:assert/strict');
const { execFileSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const { LspClient, MessageReader, encodeMessage } = require('../src/client');

const extensionRoot = path.resolve(__dirname, '..');
const repoRoot = path.resolve(extensionRoot, '../..');
const wasmPath = path.join(repoRoot, 'bin', 'ceroc-cero.wasm');

function frame(body, headerLines) {
  const bytes = Buffer.from(body, 'utf8');
  const lines = headerLines || [`Content-Length: ${bytes.length}`];
  const header = `${lines.join('\r\n')}\r\n\r\n`;
  return Buffer.concat([Buffer.from(header, 'ascii'), bytes]);
}

function bodies(messages) {
  return messages.map((message) => message.toString('utf8'));
}

test('MessageReader reads one message', () => {
  const reader = new MessageReader();
  const body = '{"jsonrpc":"2.0","method":"initialized"}';
  assert.deepEqual(bodies(reader.push(frame(body))), [body]);
  assert.deepEqual(reader.push(Buffer.alloc(0)), []);
});

test('MessageReader reads two concatenated messages', () => {
  const reader = new MessageReader();
  const first = '{"a":1}';
  const second = '{"b":2}';
  const chunk = Buffer.concat([frame(first), frame(second)]);
  assert.deepEqual(bodies(reader.push(chunk)), [first, second]);
});

test('MessageReader waits when the header is split', () => {
  const reader = new MessageReader();
  const bytes = frame('{}');
  const cut = 6;
  assert.deepEqual(reader.push(bytes.subarray(0, cut)), []);
  assert.deepEqual(bodies(reader.push(bytes.subarray(cut))), ['{}']);
});

test('MessageReader waits when the body is split', () => {
  const reader = new MessageReader();
  const body = '{"hello":"world"}';
  const bytes = frame(body);
  const headerEnd = bytes.indexOf('\r\n\r\n') + 4;
  const cut = headerEnd + 4;
  assert.ok(cut < bytes.length);
  assert.deepEqual(reader.push(bytes.subarray(0, cut)), []);
  assert.deepEqual(bodies(reader.push(bytes.subarray(cut))), [body]);
});

test('MessageReader ignores extra headers', () => {
  const reader = new MessageReader();
  const body = '{"ok":true}';
  const bytes = frame(body, [
    'Content-Type: application/vscode-jsonrpc; charset=utf-8',
    `Content-Length: ${Buffer.byteLength(body)}`,
    'X-Ignore: yes',
  ]);
  assert.deepEqual(bodies(reader.push(bytes)), [body]);
});

test('encodeMessage counts bytes for a non-ASCII body', () => {
  const body = '{"s":"あ"}';
  const bytes = encodeMessage(body);
  const sep = bytes.indexOf('\r\n\r\n');
  const header = bytes.subarray(0, sep).toString('ascii');
  const payload = bytes.subarray(sep + 4);
  assert.equal(header, `Content-Length: ${payload.length}`);
  assert.equal(payload.toString('utf8'), body);
  assert.ok(payload.length > body.length);
});

test('request rejects when the server has exited', async () => {
  const client = new LspClient(process.execPath, ['-e', 'process.exit(0)']);
  client.start();
  await assert.rejects(() => client.request('ping', {}), /server exited/);
});

test('extension.js and client.js parse', () => {
  execFileSync(process.execPath, ['--check', path.join(extensionRoot, 'src/extension.js')], {
    stdio: 'pipe',
  });
  execFileSync(process.execPath, ['--check', path.join(extensionRoot, 'src/client.js')], {
    stdio: 'pipe',
  });
});

test('extension manifest matches ADR-0017', () => {
  const pkg = JSON.parse(fs.readFileSync(path.join(extensionRoot, 'package.json'), 'utf8'));
  assert.equal(pkg.name, 'cero');
  assert.equal(pkg.main, './src/extension.js');
  assert.equal(pkg.engines.vscode, '^1.80.0');
  assert.deepEqual(pkg.activationEvents, ['onLanguage:cero']);
  assert.equal(pkg.dependencies, undefined);
  assert.equal(pkg.devDependencies, undefined);
  const properties = pkg.contributes.configuration.properties;
  assert.equal(properties['cero.wasmtimePath'].default, 'wasmtime');
  assert.equal(properties['cero.compilerWasm'].default, '${workspaceFolder}/bin/ceroc-cero.wasm');
  assert.equal(properties['cero.stdPath'].default, 'std');
  assert.equal(pkg.contributes.languages[0].id, 'cero');
  assert.equal(pkg.contributes.grammars[0].scopeName, 'source.cero');

  const language = JSON.parse(fs.readFileSync(path.join(extensionRoot, 'language-configuration.json'), 'utf8'));
  assert.equal(language.comments.lineComment, '//');
  assert.deepEqual(language.brackets, [['(', ')'], ['[', ']'], ['{', '}']]);
  const closers = language.autoClosingPairs.map((pair) => pair[0]);
  assert.ok(closers.includes('"'));
  assert.ok(closers.includes("'"));

  const grammar = JSON.parse(fs.readFileSync(path.join(extensionRoot, 'syntaxes/cero.tmLanguage.json'), 'utf8'));
  assert.equal(grammar.scopeName, 'source.cero');
  const text = JSON.stringify(grammar);
  for (const word of ['fn', 'let', 'if', 'else', 'match', 'type', 'import', 'pub', 'true', 'false', 'let!']) {
    assert.ok(text.includes(word), word);
  }
});

function serverAvailable() {
  if (!fs.existsSync(wasmPath)) {
    return false;
  }
  try {
    execFileSync('wasmtime', ['--version'], { stdio: 'ignore' });
    return true;
  } catch (err) {
    return false;
  }
}

function findIdent(src, name, nth) {
  let line = 0;
  let character = 0;
  let count = 0;
  const isIdent = (ch) => {
    return ch === '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9');
  };
  for (let i = 0; i < src.length; i++) {
    if (src.startsWith(name, i)) {
      const before = i > 0 ? src[i - 1] : '';
      const after = i + name.length < src.length ? src[i + name.length] : '';
      if (!isIdent(before) && !isIdent(after)) {
        if (count === nth) {
          return { line, character };
        }
        count++;
      }
    }
    if (src[i] === '\n') {
      line++;
      character = 0;
    } else {
      character++;
    }
  }
  throw new Error(`ident ${name} #${nth} not found`);
}

function waitForDiagnostics(client, uri) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      cleanup();
      reject(new Error(`timed out waiting for diagnostics for ${uri}\n${client.stderr}`));
    }, 90000);
    function onNote(method, params) {
      if (method !== 'textDocument/publishDiagnostics' || !params || params.uri !== uri) {
        return;
      }
      cleanup();
      resolve(params);
    }
    function cleanup() {
      clearTimeout(timer);
      client.off('notification', onNote);
    }
    client.on('notification', onNote);
  });
}

test('ceroc lsp answers initialize, diagnostics, and hover', {
  skip: serverAvailable() ? false : 'bin/ceroc-cero.wasm or wasmtime is not available',
  timeout: 180000,
}, async () => {
  const client = new LspClient(
    'wasmtime',
    ['run', '--dir=.', wasmPath, 'lsp', '--std', 'std'],
    { cwd: repoRoot },
  );
  client.start();
  const killTimer = setTimeout(() => {
    if (!client.exited && client.proc) {
      client.proc.kill('SIGKILL');
    }
  }, 170000);
  try {
    const result = await client.request('initialize', {
      rootUri: 'file://' + repoRoot,
      capabilities: {},
    });
    assert.equal(result.capabilities.textDocumentSync, 1);
    assert.equal(result.capabilities.hoverProvider, true);
    assert.equal(result.capabilities.definitionProvider, true);
    assert.equal(result.capabilities.documentFormattingProvider, true);
    assert.equal(result.serverInfo.name, 'ceroc-lsp');
    client.notify('initialized', {});

    const rel = 'internal/selfhost/testdata/lsp/bad.cero';
    const text = fs.readFileSync(path.join(repoRoot, rel), 'utf8');
    const uri = 'file://' + repoRoot + '/' + rel;
    const pending = waitForDiagnostics(client, uri);
    client.notify('textDocument/didOpen', {
      textDocument: {
        uri,
        languageId: 'cero',
        version: 1,
        text,
      },
    });
    const params = await pending;
    assert.equal(params.diagnostics.length, 1, client.stderr);
    assert.equal(params.diagnostics[0].severity, 1);
    assert.equal(params.diagnostics[0].source, 'ceroc');
    assert.equal(typeof params.diagnostics[0].message, 'string');
    assert.ok(params.diagnostics[0].message.length > 0);
    assert.equal(typeof params.diagnostics[0].range.start.line, 'number');

    const okRel = 'internal/selfhost/testdata/lsp/ok.cero';
    const okText = fs.readFileSync(path.join(repoRoot, okRel), 'utf8');
    const okUri = 'file://' + repoRoot + '/' + okRel;
    const opened = waitForDiagnostics(client, okUri);
    client.notify('textDocument/didOpen', {
      textDocument: {
        uri: okUri,
        languageId: 'cero',
        version: 1,
        text: okText,
      },
    });
    await opened;
    const pos = findIdent(okText, 'y', 1);
    const hover = await client.request('textDocument/hover', {
      textDocument: { uri: okUri },
      position: pos,
    });
    assert.equal(hover.contents.kind, 'markdown');
    assert.equal(hover.contents.value, '```cero\ny : Int\n```');

    const code = await client.stop();
    assert.equal(code, 0);
  } finally {
    clearTimeout(killTimer);
    if (!client.exited) {
      if (client.proc) {
        client.proc.kill('SIGKILL');
      }
    }
  }
});
