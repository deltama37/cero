'use strict';

const vscode = require('vscode');
const { LspClient } = require('./client');

const RESTART_LIMIT = 5;
const RESTART_WINDOW_MS = 60 * 1000;

let client = null;
let diagnostics = null;
let ready = false;
let stopping = false;
let generation = 0;
let restartTimes = [];
let stopPromise = null;

function activate(context) {
  diagnostics = vscode.languages.createDiagnosticCollection('cero');
  context.subscriptions.push(diagnostics);

  const selector = { language: 'cero' };
  context.subscriptions.push(
    vscode.languages.registerHoverProvider(selector, { provideHover }),
    vscode.languages.registerDefinitionProvider(selector, { provideDefinition }),
    vscode.languages.registerDocumentFormattingEditProvider(selector, {
      provideDocumentFormattingEdits,
    }),
    vscode.workspace.onDidOpenTextDocument((document) => {
      if (document.languageId === 'cero') {
        whenReady(() => sendDidOpen(document));
      }
    }),
    vscode.workspace.onDidChangeTextDocument((event) => {
      if (event.document.languageId === 'cero') {
        whenReady(() => sendDidChange(event.document));
      }
    }),
    vscode.workspace.onDidSaveTextDocument((document) => {
      if (document.languageId === 'cero') {
        whenReady(() => sendDidSave(document));
      }
    }),
    vscode.workspace.onDidCloseTextDocument((document) => {
      if (document.languageId === 'cero') {
        whenReady(() => sendDidClose(document));
      }
    }),
    { dispose: shutdown },
  );

  startServer();
}

function deactivate() {
  return shutdown();
}

function shutdown() {
  if (stopPromise) {
    return stopPromise;
  }
  stopping = true;
  ready = false;
  generation += 1;
  const current = client;
  client = null;
  if (!current) {
    stopPromise = Promise.resolve();
    return stopPromise;
  }
  stopPromise = current.stop();
  return stopPromise;
}

function whenReady(send) {
  if (ready && client && !stopping) {
    send();
  }
}

function workspaceFolder() {
  const folders = vscode.workspace.workspaceFolders;
  if (!folders || folders.length === 0) {
    return null;
  }
  return folders[0];
}

function setting(name, fallback) {
  const value = vscode.workspace.getConfiguration('cero').get(name);
  if (typeof value !== 'string' || value === '') {
    return fallback;
  }
  return value;
}

function startServer() {
  const gen = ++generation;
  const root = workspaceFolder();
  const cwd = root ? root.uri.fsPath : undefined;
  let wasm = setting('compilerWasm', '${workspaceFolder}/bin/ceroc-cero.wasm');
  if (cwd) {
    wasm = wasm.split('${workspaceFolder}').join(cwd);
  }
  const wasmtimePath = setting('wasmtimePath', 'wasmtime');
  const stdPath = setting('stdPath', 'std');
  const rootUri = root ? root.uri.toString() : null;

  const lsp = new LspClient(
    wasmtimePath,
    ['run', '--dir=.', wasm, 'lsp', '--std', stdPath],
    { cwd },
  );
  client = lsp;
  ready = false;
  lsp.on('notification', (method, params) => {
    if (method === 'textDocument/publishDiagnostics') {
      applyDiagnostics(params);
    }
  });
  lsp.on('exit', () => {
    if (stopping || gen !== generation || client !== lsp) {
      return;
    }
    onUnexpectedExit();
  });
  lsp.start();

  lsp.request('initialize', {
    rootUri,
    capabilities: {},
  }).then(
    () => {
      if (stopping || gen !== generation || client !== lsp) {
        return;
      }
      try {
        lsp.notify('initialized', {});
      } catch {
        return;
      }
      ready = true;
      for (const document of vscode.workspace.textDocuments) {
        if (document.languageId === 'cero') {
          sendDidOpen(document);
        }
      }
    },
    () => {
      // The exit handler restarts the server when the process ends.
    },
  );
}

function onUnexpectedExit() {
  const now = Date.now();
  restartTimes = restartTimes.filter((started) => now - started < RESTART_WINDOW_MS);
  // ADR-0017: more than five restarts in one minute stops the cycle.
  if (restartTimes.length >= RESTART_LIMIT) {
    vscode.window.showErrorMessage(
      'The Cero language server stopped. It restarted more than 5 times in one minute, so it will not be restarted again.',
    );
    return;
  }
  restartTimes.push(now);
  startServer();
}

function applyDiagnostics(params) {
  if (!diagnostics || !params || typeof params.uri !== 'string') {
    return;
  }
  const uri = vscode.Uri.parse(params.uri);
  const items = Array.isArray(params.diagnostics) ? params.diagnostics : [];
  const next = items.map((item) => {
    const diagnostic = new vscode.Diagnostic(
      toRange(item.range),
      item.message,
      toSeverity(item.severity),
    );
    if (typeof item.source === 'string') {
      diagnostic.source = item.source;
    }
    return diagnostic;
  });
  diagnostics.set(uri, next);
}

function toSeverity(value) {
  if (value === 2) {
    return vscode.DiagnosticSeverity.Warning;
  }
  if (value === 3) {
    return vscode.DiagnosticSeverity.Information;
  }
  if (value === 4) {
    return vscode.DiagnosticSeverity.Hint;
  }
  return vscode.DiagnosticSeverity.Error;
}

function toRange(range) {
  return new vscode.Range(
    range.start.line,
    range.start.character,
    range.end.line,
    range.end.character,
  );
}

function provideHover(document, position) {
  return request('textDocument/hover', {
    textDocument: { uri: document.uri.toString() },
    position: { line: position.line, character: position.character },
  }).then(
    (result) => {
      if (!result || !result.contents || typeof result.contents.value !== 'string') {
        return null;
      }
      return new vscode.Hover(new vscode.MarkdownString(result.contents.value));
    },
    () => null,
  );
}

function provideDefinition(document, position) {
  return request('textDocument/definition', {
    textDocument: { uri: document.uri.toString() },
    position: { line: position.line, character: position.character },
  }).then(
    (result) => toLocation(result),
    () => null,
  );
}

function provideDocumentFormattingEdits(document) {
  return request('textDocument/formatting', {
    textDocument: { uri: document.uri.toString() },
  }).then(
    (result) => {
      if (!Array.isArray(result)) {
        return [];
      }
      return result.map((edit) => {
        return new vscode.TextEdit(toRange(edit.range), edit.newText);
      });
    },
    () => [],
  );
}

function toLocation(result) {
  if (!result || typeof result.uri !== 'string' || !result.range) {
    return null;
  }
  return new vscode.Location(vscode.Uri.parse(result.uri), toRange(result.range));
}

function request(method, params) {
  if (!client || !ready || stopping) {
    return Promise.reject(new Error('server is not running'));
  }
  return client.request(method, params);
}

function sendDidOpen(document) {
  if (!client) {
    return;
  }
  client.notify('textDocument/didOpen', {
    textDocument: {
      uri: document.uri.toString(),
      languageId: 'cero',
      version: document.version,
      text: document.getText(),
    },
  });
}

function sendDidChange(document) {
  if (!client) {
    return;
  }
  client.notify('textDocument/didChange', {
    textDocument: { uri: document.uri.toString() },
    contentChanges: [{ text: document.getText() }],
  });
}

function sendDidSave(document) {
  if (!client) {
    return;
  }
  client.notify('textDocument/didSave', {
    textDocument: { uri: document.uri.toString() },
  });
}

function sendDidClose(document) {
  if (!client) {
    return;
  }
  client.notify('textDocument/didClose', {
    textDocument: { uri: document.uri.toString() },
  });
}

module.exports = { activate, deactivate };
