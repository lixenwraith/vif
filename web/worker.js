/* The Go program's thread. The page draws the frames and plays the sound posted
   from here and reports back what it saw: keys, the pointer, its size and each frame
   drawn. This side presents those to Go as the globals pkg/browser expects, and
   keeps Go's stderr for the page to show once the program ends. */
'use strict';

/* Why the program ended. stderr is a Go program's only word on that, and
   wasm_exec sends it to the console alone; kept here, the page shows it once the
   program exits instead of leaving a screen that simply stopped. */
let lastLine = '';
let crashLine = '';
const errDecoder = new TextDecoder('utf-8');
let partial = '';

// wasm_exec.js by the URL the page names, then its stderr kept as above.
function loadRuntime(exec) {
    importScripts(exec);
    const writeSync = globalThis.fs.writeSync;
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
}

// Go allocates each array for this call alone, so its buffer can move to the page.
globalThis.vifAtlas = function(data) {
    postMessage({ type: 'atlas', data: data }, [data.buffer]);
};
globalThis.vifFrame = function(data) {
    postMessage({ type: 'frame', data: data }, [data.buffer]);
};
globalThis.vifAudio = function(data) {
    postMessage({ type: 'audio', data: data }, [data.buffer]);
};
globalThis.vifPointer = function(enabled, motion) {
    postMessage({ type: 'pointer', enabled: enabled, motion: motion });
};
// A Worker has no localStorage: the page reads and keeps the settings document.
globalThis.vifKeepSettings = function(text) {
    postMessage({ type: 'settings', text: text });
};

async function start(msg) {
    try {
        loadRuntime(msg.exec || 'wasm_exec.js');
    } catch (err) {
        postMessage({ type: 'failed', reason: 'wasm_exec.js did not load: ' + err.message });
        return;
    }
    globalThis.vifGrid = { cols: msg.cols, rows: msg.rows };
    globalThis.vifSettings = msg.settings || '';
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
    case 'event':
        // A size measured before the program started is what it opens at.
        if (msg.event.type === 'resize' && globalThis.vifGrid) {
            globalThis.vifGrid.cols = msg.event.cols;
            globalThis.vifGrid.rows = msg.event.rows;
        }
        if (typeof globalThis.goEvent === 'function') globalThis.goEvent(msg.event);
        break;
    case 'visibility':
        /* Go draws nothing while the page is hidden, so no frames queue behind the
           page's throttled timers to replay on return; the next frame it draws once
           seen again is the whole screen. */
        globalThis.vifHidden = msg.hidden;
        if (!msg.hidden && typeof globalThis.goEvent === 'function') globalThis.goEvent({ type: 'sync' });
        break;
    }
};
