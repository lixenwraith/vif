/* The program's thread in the extension: worker.js, behind a fetch that asks the
   page before a request leaves for a site. Go's net/http calls this scope's fetch,
   and only the page (extension.js) holds chrome.permissions and the player's
   gesture, so the page answers over the channel whether the request may go. */
(function() {
    'use strict';

    const send = self.fetch;
    const channel = new BroadcastChannel('vif.site');
    const thread = crypto.randomUUID();   // sets this thread's asks apart from another tab's
    const asked = new Map();              // the asks no page has answered, by id
    let asks = 0;

    channel.onmessage = function(e) {
        const settle = asked.get(e.data.answer);
        if (settle) settle(e.data.allowed);
    };

    /* reach resolves to whether a request may leave for origin. A request given up
       while it waits rejects as fetch does, at once: Go waits on that after its
       timeout, and the page stops asking the player. */
    function reach(origin, signal) {
        return new Promise(function(resolve, reject) {
            const id = thread + '/' + (++asks);
            const gaveUp = function() {
                asked.delete(id);
                channel.postMessage({ dropped: id });
                reject(signal.reason);
            };
            if (signal && signal.aborted) {
                reject(signal.reason);
                return;
            }
            if (signal) signal.addEventListener('abort', gaveUp, { once: true });
            asked.set(id, function(allowed) {
                asked.delete(id);
                if (signal) signal.removeEventListener('abort', gaveUp);
                resolve(allowed);
            });
            channel.postMessage({ ask: id, origin: origin });
        });
    }

    self.fetch = function(input, init) {
        let url;
        try {
            url = new URL(input instanceof Request ? input.url : input, self.location.href);
        } catch (err) {
            return send.call(self, input, init);   // fetch's own refusal
        }
        // The extension's own files, as worker.js loads them, ask nobody.
        if (url.protocol !== 'https:' && url.protocol !== 'http:') return send.call(self, input, init);
        const signal = (init && init.signal) || (input instanceof Request ? input.signal : null);
        return reach(url.origin, signal).then(function(allowed) {
            if (allowed) return send.call(self, input, init);
            // Go puts this after "fetch() failed:", on the status line or the exit screen
            throw new DOMException('reaching ' + url.hostname + ' was not allowed; try again and choose Allow',
                'NotAllowedError');
        });
    };

    importScripts('worker.js');
})();
