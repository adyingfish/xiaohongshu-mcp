/*
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
const {readFileSync} = require('node:fs');
const {join} = require('node:path');
const {runInNewContext} = require('node:vm');
const test = require('node:test');
const assert = require('node:assert/strict');

const source = readFileSync(join(__dirname, 'direct_message.js'), 'utf8');
const userId = '0123456789abcdef01234567';
const name = '测试收件人';
const content = '第一行\n😀 " \\ <script>只是文字</script>';

// 仅模拟浏览器 DOM 接口与事件，不重写被测脚本中的收件人或发送判定。
function element(text = '', attrs = {}, children = {}) {
    return {
        textContent: text,
        get childNodes() { return [{nodeType: 3, data: this.textContent}]; },
        events: [],
        width: 100,
        getBoundingClientRect() { return {width: this.width, height: 20}; },
        getAttribute(key) { return attrs[key] ?? null; },
        querySelector(selector) { return children[selector] || null; },
        querySelectorAll() { return []; },
        cloneNode() { return element(this.textContent); },
        focus() {},
        dispatchEvent(event) { this.events.push(event); },
    };
}

function fixture({id = userId, nickname = name, draft = '', disabled = false} = {}) {
    const editor = element(draft, {
        contenteditable: 'true', 'data-placeholder': disabled ? '' : '发消息...',
    });
    const active = element('', {'data-conv-id': id}, {
        '.xhs-im-conv-item__name': element(nickname),
    });
    const nodes = {
        '.xhs-im-view': [element()],
        '.xhs-im-chat-window .xhs-im-input-bar-editor': [editor],
        '.xhs-im-conv-item--active[data-conv-kind="c2c"]': [active],
        '.xhs-im-chat-window__header-name': [element(nickname)],
        '.xhs-im-msg-list .chat-item': [],
    };
    class Event {
        constructor(type, init) { this.type = type; Object.assign(this, init); }
    }
    const location = {origin: 'https://www.xiaohongshu.com', pathname: `/chat/${userId}`, search: ''};
    const context = {
        location,
        URLSearchParams,
        Node: {ELEMENT_NODE: 1, TEXT_NODE: 3},
        document: {
            querySelectorAll: selector => nodes[selector] || [],
            querySelector: selector => nodes[selector]?.[0] || null,
        },
        getComputedStyle: () => ({visibility: 'visible'}),
        InputEvent: Event,
        KeyboardEvent: Event,
    };
    const execute = params => runInNewContext(`(${source})(${JSON.stringify({
        user_id: userId, expected_name: name, content, ...params,
    })})`, context);
    return {editor, nodes, execute, location};
}

test('收件人 ID 或昵称不符时不得触发发送事件', () => {
    for (const options of [{id: 'another-id'}, {nickname: '同名之外的账号'}]) {
        const f = fixture({...options, draft: content});
        assert.match(f.execute({action: 'send'}).error, /收件人/);
        assert.equal(f.editor.events.length, 0);
    }
});

test('其他站点即使具有相同 DOM 也不能发送', () => {
    const f = fixture({draft: content});
    f.location.origin = 'https://example.com';
    assert.ok(f.execute({action: 'send'}).error);
    assert.equal(f.editor.events.length, 0);
});

test('会话控件不唯一或不可见时停止', () => {
    const f = fixture({draft: content});
    f.nodes['.xhs-im-chat-window__header-name'].push(element(name));
    assert.ok(f.execute({action: 'send'}).error);
    f.nodes['.xhs-im-chat-window__header-name'] = [element(name)];
    f.editor.width = 0;
    assert.ok(f.execute({action: 'send'}).error);
    assert.equal(f.editor.events.length, 0);
});

test('正文变化、禁发状态或附带引用时停止', () => {
    const changed = fixture({draft: '已被用户修改'});
    assert.match(changed.execute({action: 'send'}).error, /正文/);
    const disabled = fixture({draft: content, disabled: true});
    assert.match(disabled.execute({action: 'send'}).error, /输入框/);
    const quoted = fixture({draft: content});
    quoted.nodes['.xhs-im-input-bar-wrap .xhs-im-input-bar-quote'] = [element()];
    assert.match(quoted.execute({action: 'send'}).error, /引用/);
    for (const f of [changed, disabled, quoted]) assert.equal(f.editor.events.length, 0);
});

test('只读取己方消息与服务端标识，失败标记不会丢失', () => {
    const f = fixture();
    const own = element('', {'data-message-id': 'own-id', 'data-store-id': '3'}, {
        '.chat-item__bubble--me': element(), '.xhs-im-bubble__text': element(content),
        '.chat-item__status-btn--failed': element(),
    });
    f.nodes['.xhs-im-msg-list .chat-item'] = [element('对方消息'), own];
    const state = f.execute({action: 'check'});
    assert.equal(state.outgoing.length, 1);
    assert.equal(state.outgoing[0].message_id, 'own-id');
    assert.equal(state.outgoing[0].store_id, '3');
    assert.equal(state.outgoing[0].failed, true);
});

test('发送前最后一次检查拦截相同正文', () => {
    const f = fixture({draft: content});
    f.nodes['.xhs-im-msg-list .chat-item'] = [element('', {}, {
        '.chat-item__bubble--me': element(), '.xhs-im-bubble__text': element(content),
    })];
    assert.match(f.execute({action: 'send'}).error, /重复/);
    assert.equal(f.editor.events.length, 0);
});

test('匹配会话和正文时只发送一次 Enter，不模拟 Shift 换行', () => {
    const f = fixture({draft: content});
    assert.equal(f.execute({action: 'send'}).submitted, true);
    assert.equal(f.editor.events.length, 1);
    assert.equal(f.editor.events[0].type, 'keydown');
    assert.equal(f.editor.events[0].key, 'Enter');
    assert.equal(f.editor.events[0].shiftKey, false);
});

test('openUid 新会话仍须同时匹配选中的单人会话与昵称', () => {
    const f = fixture({draft: content});
    f.location.pathname = '/chat';
    f.location.search = `?openUid=${userId}`;
    assert.equal(f.execute({action: 'snapshot'}).conversation_ready, true);
    assert.equal(f.execute({action: 'send'}).submitted, true);
    assert.equal(f.editor.events.length, 1);
    for (const options of [{id: 'another-id'}, {nickname: '错误昵称'}]) {
        const wrong = fixture({...options, draft: content});
        wrong.location.pathname = '/chat';
        wrong.location.search = `?openUid=${userId}`;
        assert.match(wrong.execute({action: 'send'}).error, /收件人/);
        assert.equal(wrong.editor.events.length, 0);
    }
});

test('拒绝缺少、重复、非法或与路径冲突的 openUid', () => {
    for (const [pathname, search] of [
        ['/chat', ''], ['/chat', '?openUid=wrong'],
        ['/chat', `?openUid=${userId}&openUid=${userId}`],
        [`/chat/${userId}`, '?openUid=ffffffffffffffffffffffff'],
        [`/chat/${userId}`, '?openUid='],
        ['/chat/invalid', `?openUid=${userId}`],
    ]) {
        const f = fixture({draft: content});
        Object.assign(f.location, {pathname, search});
        assert.ok(f.execute({action: 'send'}).error);
        assert.equal(f.editor.events.length, 0);
    }
});
