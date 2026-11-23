// The toolbar button opens the game in a tab, or shows the tab that already has it.
chrome.action.onClicked.addListener(async function() {
    const [open] = await chrome.runtime.getContexts({ contextTypes: ['TAB'] });
    if (!open) {
        await chrome.tabs.create({ url: 'index.html' });
        return;
    }
    await chrome.tabs.update(open.tabId, { active: true });
    await chrome.windows.update(open.windowId, { focused: true });
});
