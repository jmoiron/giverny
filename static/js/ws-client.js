(function (global) {
    'use strict';

    global.createBoardSocket = function (boardSlug, options) {
        options = options || {};
        var socket = null, reconnectTimer = null, connectTimer = null;
        var delay = 1000, closed = false;
        var maxDelay = 30000, timeout = 5000;
        var state = 'idle';
        function setState(next) { state = next; if (options.onStateChange) options.onStateChange(next); }
        function schedule() {
            if (closed || reconnectTimer) return;
            reconnectTimer = setTimeout(function () { reconnectTimer = null; connect(); }, delay);
            delay = Math.min(maxDelay, Math.round(delay * 1.8));
        }
        function connect() {
            if (closed || (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING))) return;
            setState('connecting');
            var protocol = location.protocol === 'https:' ? 'wss' : 'ws';
            socket = new WebSocket(protocol + '://' + location.host + '/boards/' + encodeURIComponent(boardSlug) + '/ws');
            connectTimer = setTimeout(function () { if (socket && socket.readyState === WebSocket.CONNECTING) socket.close(); }, timeout);
            socket.onopen = function () { clearTimeout(connectTimer); delay = 1000; setState('connected'); };
            socket.onmessage = function (event) {
                try { if (options.onEvent) options.onEvent(JSON.parse(event.data)); } catch (_) {}
            };
            socket.onerror = function () { setState('error'); };
            socket.onclose = function () { clearTimeout(connectTimer); if (!closed) { setState('error'); schedule(); } };
        }
        function close() { closed = true; clearTimeout(connectTimer); clearTimeout(reconnectTimer); if (socket) socket.close(); }
        return {connect: connect, close: close};
    };
}(window));
