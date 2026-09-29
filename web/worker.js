/* The Go program's thread. A frame xterm takes long to draw holds only the page,
   never a tick, a probe echo or a socket read; the page relays the terminal both
   ways and this side presents it to Go as the page globals it expects. */
'use strict';

importScripts('wasm_exec.js');

/* Why the program ended. stderr is a Go program's only word on that, and
   wasm_exec sends it to the console alone; kept here, the page shows it once the
   program exits instead of leaving a terminal that simply stopped. */
let lastLine = '';
let crashLine = '';
const writeSync = globalThis.fs.writeSync;
const errDecoder = new TextDecoder('utf-8');
let partial = '';
globalThis.fs.writeSync = function(fd, buf) {
    if (fd === 2) {
        const lines = (partial + errDecoder.decode(buf, { stream: true })).split('\n');
        partial = lines.pop();
        for (const line of lines.map(l => l.trim()).filter(Boolean)) {
            // A crash's first line names it; the rest is its stack.
            if (!crashLine && /^(panic|fatal error|CRASH DETECTED):/.test(line)) crashLine = line;
            lastLine = line;
        }
    }
    return writeSync.call(this, fd, buf);
};

// Go allocates each array for this call alone, so its buffer can move.
globalThis.goTerminalWrite = function(data) {
    postMessage({ type: 'write', data: data }, [data.buffer]);
};

async function start(msg) {
    globalThis.xterm = { cols: msg.cols, rows: msg.rows };
    const go = new Go();
    go.argv = msg.argv;
    go.exit = function(code) {
        postMessage({ type: 'exit', code: code, reason: crashLine || lastLine });
    };
    try {
        const result = await WebAssembly.instantiate(msg.bytes, go.importObject);
        go.run(result.instance);
    } catch (err) {
        postMessage({ type: 'failed', reason: err.message });
    }
}

onmessage = function(e) {
    const msg = e.data;
    switch (msg.type) {
    case 'start':
        start(msg);
        break;
    case 'input':
        if (typeof globalThis.goTerminalInput === 'function') globalThis.goTerminalInput(msg.data);
        break;
    case 'resize':
        globalThis.xterm.cols = msg.cols;
        globalThis.xterm.rows = msg.rows;
        if (typeof globalThis.goTerminalResize === 'function') globalThis.goTerminalResize(msg.cols, msg.rows);
        break;
    case 'visibility':
        /* Go draws nothing while the page is hidden, so the page's timer-throttled
           terminal gathers no backlog to replay on return. A resize of the same size
           is what repaints the whole screen once it is seen again. */
        globalThis.vifHidden = msg.hidden;
        if (!msg.hidden && typeof globalThis.goTerminalResize === 'function') {
            globalThis.goTerminalResize(globalThis.xterm.cols, globalThis.xterm.rows);
        }
        break;
    }
};
