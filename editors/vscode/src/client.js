'use strict';

const { spawn } = require('child_process');
const { EventEmitter } = require('events');

const HEADER_END = Buffer.from('\r\n\r\n');

// LspClient speaks JSON-RPC 2.0 over a child process's stdio with
// Content-Length framing (ADR-0017).
class LspClient extends EventEmitter {
  constructor(command, args, options) {
    super();
    this.command = command;
    this.args = args || [];
    this.options = options || {};
    this.reader = new MessageReader();
    this.proc = null;
    this.nextId = 1;
    this.pending = new Map();
    this.exited = false;
    this.exitCode = null;
    this.stderr = '';
    this.stopPromise = null;
  }

  start() {
    if (this.proc) {
      return;
    }
    const options = Object.assign({}, this.options, {
      stdio: ['pipe', 'pipe', 'pipe'],
    });
    this.proc = spawn(this.command, this.args, options);
    this.proc.stdout.on('data', (chunk) => {
      this._onData(chunk);
    });
    this.proc.stderr.on('data', (chunk) => {
      this.stderr += chunk.toString('utf8');
      if (this.stderr.length > 8192) {
        this.stderr = this.stderr.slice(-8192);
      }
    });
    this.proc.stdin.on('error', () => {});
    this.proc.stdout.on('error', () => {});
    this.proc.stderr.on('error', () => {});
    this.proc.on('error', () => {
      if (!this.exited) {
        this._onExit(null);
      }
    });
    this.proc.on('exit', (code) => {
      this._onExit(code);
    });
  }

  request(method, params) {
    return new Promise((resolve, reject) => {
      if (this.exited || !this.proc) {
        reject(this._exitError());
        return;
      }
      const id = this.nextId++;
      this.pending.set(id, { resolve, reject });
      const body = JSON.stringify({
        jsonrpc: '2.0',
        id,
        method,
        params: params === undefined ? null : params,
      });
      try {
        this._write(encodeMessage(body));
      } catch (err) {
        this.pending.delete(id);
        reject(err);
      }
    });
  }

  notify(method, params) {
    if (this.exited || !this.proc) {
      return;
    }
    const body = JSON.stringify({
      jsonrpc: '2.0',
      method,
      params: params === undefined ? null : params,
    });
    try {
      this._write(encodeMessage(body));
    } catch {
      // The exit event rejects in-flight requests.
    }
  }

  stop() {
    if (this.stopPromise) {
      return this.stopPromise;
    }
    this.stopPromise = this._stop();
    return this.stopPromise;
  }

  _stop() {
    if (this.exited || !this.proc) {
      return Promise.resolve(this.exitCode);
    }
    const exited = new Promise((resolve) => {
      this.once('exit', (code) => resolve(code));
    });
    return this.request('shutdown', null).catch(() => {}).then(() => {
      if (!this.exited) {
        this.notify('exit', null);
      }
      return exited;
    });
  }

  _write(buf) {
    if (!this.proc || !this.proc.stdin.writable) {
      throw this._exitError();
    }
    this.proc.stdin.write(buf);
  }

  _onData(chunk) {
    let bodies;
    try {
      bodies = this.reader.push(chunk);
    } catch (err) {
      this._rejectAll(err);
      return;
    }
    for (const body of bodies) {
      this._handle(body);
    }
  }

  _handle(body) {
    let msg;
    try {
      msg = JSON.parse(body.toString('utf8'));
    } catch (err) {
      return;
    }
    if (!msg || typeof msg !== 'object') {
      return;
    }
    const hasResult = Object.prototype.hasOwnProperty.call(msg, 'result');
    if (Object.prototype.hasOwnProperty.call(msg, 'id') && (hasResult || msg.error)) {
      const pending = this.pending.get(msg.id);
      if (!pending) {
        return;
      }
      this.pending.delete(msg.id);
      if (msg.error) {
        const err = new Error(msg.error.message || 'request failed');
        err.code = msg.error.code;
        pending.reject(err);
      } else {
        pending.resolve(msg.result);
      }
      return;
    }
    if (typeof msg.method === 'string') {
      this.emit('notification', msg.method, msg.params);
    }
  }

  _onExit(code) {
    if (this.exited) {
      return;
    }
    this.exited = true;
    this.exitCode = code;
    this._rejectAll(this._exitError());
    this.emit('exit', code);
  }

  _rejectAll(err) {
    for (const pending of this.pending.values()) {
      pending.reject(err);
    }
    this.pending.clear();
  }

  _exitError() {
    const err = new Error('server exited');
    if (this.stderr) {
      err.message += '\n' + this.stderr.trim();
    }
    return err;
  }
}

class MessageReader {
  constructor() {
    this.buf = Buffer.alloc(0);
  }

  push(chunk) {
    const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
    this.buf = this.buf.length === 0 ? Buffer.from(bytes) : Buffer.concat([this.buf, bytes]);
    const messages = [];
    for (;;) {
      const taken = takeMessage(this.buf);
      if (!taken) {
        break;
      }
      messages.push(taken.body);
      this.buf = taken.rest;
    }
    return messages;
  }
}

function takeMessage(buf) {
  const sep = buf.indexOf(HEADER_END);
  if (sep < 0) {
    return null;
  }
  const header = buf.subarray(0, sep).toString('ascii');
  let length = null;
  for (const line of header.split('\r\n')) {
    const idx = line.indexOf(':');
    if (idx < 0) {
      continue;
    }
    const name = line.slice(0, idx).trim().toLowerCase();
    if (name !== 'content-length') {
      continue;
    }
    const raw = line.slice(idx + 1).trim();
    if (!/^[0-9]+$/.test(raw)) {
      throw new Error('invalid Content-Length');
    }
    length = Number(raw);
  }
  if (length === null) {
    throw new Error('missing Content-Length');
  }
  const start = sep + HEADER_END.length;
  if (buf.length < start + length) {
    return null;
  }
  return {
    body: Buffer.from(buf.subarray(start, start + length)),
    rest: buf.subarray(start + length),
  };
}

function encodeMessage(body) {
  const bytes = Buffer.isBuffer(body) ? body : Buffer.from(String(body), 'utf8');
  const header = Buffer.from(`Content-Length: ${bytes.length}\r\n\r\n`, 'ascii');
  return Buffer.concat([header, bytes]);
}

module.exports = { LspClient, encodeMessage, MessageReader };
