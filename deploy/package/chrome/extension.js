/* The extension's script beside terminal.js. The program's thread
   (extension-worker.js) asks here before a request leaves for a site, and this asks
   the player for the host permission the manifest leaves optional: in the key
   press or click that started the request, else at a window's button. */
(function() {
    'use strict';

    const channel = new BroadcastChannel('vif.site');
    const hosts = chrome.runtime.getManifest().optional_host_permissions || [];
    const prompts = new Map();   // Chrome's prompt while it is up, by the host's pattern
    const sheets = new Map();    // the window asking first, by pattern: its element and asks

    channel.onmessage = function(e) {
        if (e.data.ask) answer(e.data.ask, e.data.origin);
        else settle(e.data.answer || e.data.dropped);   // another tab answered, or the request gave up
    };

    function reply(id, allowed) {
        settle(id);
        channel.postMessage({ answer: id, allowed: allowed });
    }

    // settle takes an ask off its window, which closes with its last
    function settle(id) {
        for (const [pattern, sheet] of sheets) {
            sheet.ids.delete(id);
            if (sheet.ids.size) continue;
            sheets.delete(pattern);
            sheet.el.close();
        }
    }

    /* answer lets a request go where the permission is held or is not the
       extension's to ask for, and otherwise asks: Chrome's prompt needs a gesture,
       which the press that started the request still is for a few seconds. */
    async function answer(id, origin) {
        const url = new URL(origin);
        const wanted = { origins: [url.protocol + '//' + url.hostname + '/*'] };
        if (!hosts.includes(wanted.origins[0]) || await chrome.permissions.contains(wanted)) {
            reply(id, true);
            return;
        }
        if (navigator.userActivation.isActive) {
            try {
                reply(id, await prompt(wanted));
                return;
            } catch (err) {
                // the gesture ran out first: the window's button is one
            }
        }
        wait(id, url.hostname, wanted);
    }

    // prompt has Chrome ask the player, once for every ask that comes while it is up
    function prompt(wanted) {
        const pattern = wanted.origins[0];
        if (!prompts.has(pattern)) {
            prompts.set(pattern, chrome.permissions.request(wanted).finally(function() { prompts.delete(pattern); }));
        }
        return prompts.get(pattern);
    }

    /* wait asks in a window of the page's own (terminal.css's sheet) where no
       gesture stands: its Allow is one, and Chrome's prompt follows it. Not now,
       Escape and a refusal there all refuse the asks it holds. */
    function wait(id, host, wanted) {
        const pattern = wanted.origins[0];
        let sheet = sheets.get(pattern);
        if (sheet) {
            sheet.ids.add(id);
            return;
        }
        const el = document.createElement('dialog');
        const title = document.createElement('h2');
        const about = document.createElement('p');
        const actions = document.createElement('div');
        const later = document.createElement('button');
        const allow = document.createElement('button');
        el.className = 'sheet';
        title.textContent = 'Reach ' + host + '?';
        about.className = 'about';
        about.textContent = 'Games with others are made and found at ' + host
            + ', which Chrome lets vif reach once you allow it. Playing solo needs nothing.';
        actions.className = 'actions';
        later.type = allow.type = 'button';
        later.textContent = 'Not now';
        allow.textContent = 'Allow';
        allow.className = 'go';
        actions.append(later, allow);
        el.append(title, about, actions);
        sheet = { el: el, ids: new Set([id]) };
        sheets.set(pattern, sheet);
        const all = function(allowed) {
            for (const held of [...sheet.ids]) reply(held, allowed);
        };
        later.addEventListener('click', function() { all(false); });
        allow.addEventListener('click', function() {
            prompt(wanted).then(all, function() { all(false); });
        });
        el.addEventListener('close', function() {
            el.remove();
            all(false);
        });
        document.body.append(el);
        el.showModal();
    }
})();
