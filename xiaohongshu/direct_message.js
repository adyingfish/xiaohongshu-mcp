/*
Adapted from adyingfish/xiaohongshu-skills, scripts/xhs/direct_message.js.
MIT License

Copyright (c) 2026 Auto-Claw-CC

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/
// 从 xiaohongshu-skills 的网页状态判据适配。读取同一快照，并原子核对目标后发送。
// 填写和导航使用 go-rod；此处保留发送前核对与 Enter 事件，避免跨调用切换会话。
(params) => {
    const visible = el => !!el && el.getBoundingClientRect().width > 0 &&
        el.getBoundingClientRect().height > 0 && getComputedStyle(el).visibility !== 'hidden';
    const one = selector => {
        const nodes = [...document.querySelectorAll(selector)].filter(visible);
        return nodes.length === 1 ? nodes[0] : null;
    };
    // 使用纯文本，保留换行；已有网页表情按其 alt 文本读取，避免漏掉草稿内容。
    const plainText = el => {
        if (!el) return '';
        const copy = el.cloneNode(true);
        copy.querySelectorAll('img').forEach(img => img.replaceWith(img.alt || '\uFFFC'));
        copy.querySelectorAll('br').forEach(br => br.replaceWith('\n'));
        copy.querySelectorAll('div,p').forEach(block => block.prepend('\n'));
        return copy.textContent.replace(/\r\n/g, '\n').trim();
    };
    const onChat = location.origin === 'https://www.xiaohongshu.com' &&
        /^\/chat(?:\/|$)/.test(location.pathname);
    const editor = one('.xhs-im-chat-window .xhs-im-input-bar-editor');
    const active = one('.xhs-im-conv-item--active[data-conv-kind="c2c"]');
    const header = one('.xhs-im-chat-window__header-name');
    const pathId = location.pathname.match(/^\/chat\/([0-9a-fA-F]{24})\/?$/)?.[1]?.toLowerCase() || '';
    const openIds = new URLSearchParams(location.search).getAll('openUid');
    const openId = openIds.length === 1 && /^[0-9a-fA-F]{24}$/.test(openIds[0]) ?
        openIds[0].toLowerCase() : '';
    const queryValid = openIds.length === 0 || (openIds.length === 1 && !!openId);
    // The URL is an independent identity check, never a substitute for the active
    // c2c row and both names. Reject ambiguous or conflicting route parameters.
    const routeId = queryValid && (!pathId || !openId || pathId === openId) ?
        (pathId || (/^\/chat\/?$/.test(location.pathname) ? openId : '')) : '';
    const outgoing = () => [...document.querySelectorAll('.xhs-im-msg-list .chat-item')]
        .filter(el => el.querySelector('.chat-item__bubble--me'))
        .map(el => ({
            message_id: el.getAttribute('data-message-id') || '',
            store_id: el.getAttribute('data-store-id') || '',
            text: plainText(el.querySelector('.xhs-im-bubble__text')),
            failed: !!el.querySelector('.chat-item__status-btn--failed'),
            pending: !!el.querySelector('.chat-item__status-loading'),
        }));
    const state = () => ({
        on_chat_page: onChat,
        ready: onChat && !!one('.xhs-im-view'),
        conversation_ready: !!editor && !!header && !!active &&
            !!routeId && active.getAttribute('data-conv-id') === routeId,
        user_id: routeId,
        recipient: header?.textContent.trim() || '',
        draft: plainText(editor),
        outgoing: params.action === 'check' ? outgoing() : [],
        not_logged_in: visible(document.querySelector('.login-container')),
    });
    if (params.action === 'snapshot') return state();
    if (params.action === 'list') {
        if (!onChat) return {error: '当前不是小红书消息页面'};
        const conversations = [...document.querySelectorAll(
            '.xhs-im-conv-item[data-conv-kind="c2c"]'
        )].map(el => ({
            user_id: el.getAttribute('data-conv-id'),
            nickname: el.querySelector('.xhs-im-conv-item__name')?.textContent.trim() || '',
        })).filter(item => !params.name || item.nickname === params.name);
        return {...state(), conversations};
    }
    const check = () => {
        if (!onChat || !routeId || routeId !== params.user_id || !active ||
            active.getAttribute('data-conv-id') !== params.user_id ||
            header?.textContent.trim() !== params.expected_name ||
            active.querySelector('.xhs-im-conv-item__name')?.textContent.trim() !==
                params.expected_name) return '收件人 ID、单人会话和昵称未同时匹配，已停止';
        if (!editor || editor.getAttribute('contenteditable') !== 'true' ||
            editor.getAttribute('aria-disabled') === 'true' ||
            !editor.getAttribute('data-placeholder')) return '私信输入框不可用，可能被限制发送';
        if (document.querySelector('.xhs-im-input-bar-wrap .xhs-im-input-bar-quote')) {
            return '输入框附带引用，请先清除引用后再发送纯文字私信';
        }
        return '';
    };
    const error = check();
    if (error) return {error};
    if (params.action === 'check') return state();
    if (params.action === 'send') {
        if (plainText(editor) !== params.content) return {error: '发送前正文发生变化，已停止'};
        const messages = outgoing();
        if (messages.at(-1)?.text === params.content) return {error: '检测到重复私信，已停止'};
        const before_ids = messages.map(message => message.message_id);
        editor.focus();
        // 网页桌面端的发送入口为 Enter；Shift+Enter 为换行。
        editor.dispatchEvent(new KeyboardEvent('keydown', {
            key: 'Enter', code: 'Enter', keyCode: 13, which: 13,
            bubbles: true, cancelable: true, shiftKey: false, isComposing: false,
        }));
        return {submitted: true, before_ids};
    }
    return {error: '未知私信操作'};
}
