(function () {
    'use strict';

    function ready(fn) {
        if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', fn);
        else fn();
    }

    function base64urlBytes(value) {
        var padded = value.replace(/-/g, '+').replace(/_/g, '/');
        while (padded.length % 4) padded += '=';
        var raw = atob(padded), bytes = new Uint8Array(raw.length);
        for (var i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i);
        return bytes;
    }

    function setupPushNotifications() {
        if (!document.body.hasAttribute('data-user-timezone')) return;
        if (!('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window)) return;
        if (localStorage.getItem('giverny-push-prompted') === '1' || Notification.permission === 'denied') return;
        navigator.serviceWorker.ready.then(function (registration) {
            return registration.pushManager.getSubscription().then(function (subscription) {
                if (subscription) return fetch('/mobile/push/subscribe', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(subscription)});
                return Notification.requestPermission().then(function (permission) {
                    localStorage.setItem('giverny-push-prompted', '1');
                    if (permission !== 'granted') return null;
                    return fetch('/mobile/push/vapid-key').then(function (response) { return response.json(); }).then(function (key) {
                        return registration.pushManager.subscribe({userVisibleOnly: true, applicationServerKey: base64urlBytes(key.publicKey)});
                    }).then(function (newSubscription) {
                        return fetch('/mobile/push/subscribe', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(newSubscription)});
                    });
                });
            });
        }).catch(function () {});
    }

    function toggleDrawer(name) {
        var body = document.body;
        var open = name === 'left' ? 'mobile-left-open' : 'mobile-right-open';
        var other = name === 'left' ? 'mobile-right-open' : 'mobile-left-open';
        body.classList.toggle(open);
        body.classList.remove(other);
        if (name === 'left' && body.classList.contains(open)) loadMobileNav();
    }

    var navLoaded = false;
    function loadMobileNav() {
        if (navLoaded) return;
        navLoaded = true;
        fetch('/api/nav-boards/').then(function (r) { return r.json(); }).then(function (boards) {
            var list = document.getElementById('side-nav-boards');
            if (!list) return;
            list.innerHTML = '';
            (boards || []).forEach(function (board) {
                var link = document.createElement('a');
                link.className = 'side-nav-board-link';
                link.href = '/mobile/boards/' + encodeURIComponent(board.slug) + '/';
                link.textContent = board.name;
                list.appendChild(link);
            });
        }).catch(function () {});
        fetch('/api/nav-views/').then(function (r) { return r.json(); }).then(function (views) {
            var list = document.getElementById('side-nav-views');
            var label = document.getElementById('side-nav-views-label');
            if (!list) return;
            (views || []).forEach(function (view) {
                var link = document.createElement('a');
                link.className = 'side-nav-board-link';
                link.href = '/cards/views/' + encodeURIComponent(view.slug) + '/';
                link.textContent = view.name;
                list.appendChild(link);
            });
            if (label && views && views.length) label.classList.remove('hidden');
        }).catch(function () {});
    }

    function updateOrder(slug, column, container, originalOrder) {
        var ids = Array.prototype.map.call(container.querySelectorAll('.kanban-card'), function (card) {
            return Number(card.getAttribute('data-id'));
        });
        if (originalOrder && ids.join(',') === originalOrder) return;
        fetch('/boards/' + encodeURIComponent(slug) + '/columns/' + column + '/cards/reorder', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(ids)
        }).catch(function () {});
    }

    function setupNativeReorder(boardPage) {
        var slug = boardPage.getAttribute('data-board-slug');
        var dragged = null;
        boardPage.addEventListener('dragstart', function (event) {
            var card = event.target.closest('.kanban-card');
            if (!card) return;
            dragged = card;
            card.classList.add('dragging-source');
            if (event.dataTransfer) {
                event.dataTransfer.effectAllowed = 'move';
                event.dataTransfer.setData('text/plain', card.getAttribute('data-id'));
            }
        });
        boardPage.addEventListener('dragover', function (event) {
            var container = event.target.closest('.col-cards');
            if (!container || !dragged || dragged.parentNode !== container) return;
            event.preventDefault();
            var target = event.target.closest('.kanban-card');
            if (!target || target === dragged) return;
            var rect = target.getBoundingClientRect();
            container.insertBefore(dragged, event.clientY < rect.top + rect.height / 2 ? target : target.nextSibling);
        });
        boardPage.addEventListener('drop', function (event) {
            var container = event.target.closest('.col-cards');
            if (!container || !dragged || dragged.parentNode !== container) return;
            event.preventDefault();
            updateOrder(slug, container.getAttribute('data-column-id'), container, '');
        });
        boardPage.addEventListener('dragend', function () {
            if (dragged) dragged.classList.remove('dragging-source');
            dragged = null;
        });
    }

    function setupTouchReorder(boardPage) {
        var slug = boardPage.getAttribute('data-board-slug');
        var state = null;
        var edgeLeft = document.createElement('div');
        var edgeRight = document.createElement('div');
        edgeLeft.className = 'mobile-drag-edge mobile-drag-edge-left';
        edgeRight.className = 'mobile-drag-edge mobile-drag-edge-right';
        edgeLeft.textContent = '‹';
        edgeRight.textContent = '›';
        boardPage.appendChild(edgeLeft);
        boardPage.appendChild(edgeRight);
        function setEdges(active) {
            var columns = boardPage.querySelector('.mobile-board-columns');
            var maxScroll = columns ? Math.max(0, columns.scrollWidth - columns.clientWidth) : 0;
            edgeLeft.classList.toggle('active', !!active && !!columns && columns.scrollLeft > 2);
            edgeRight.classList.toggle('active', !!active && !!columns && columns.scrollLeft < maxScroll - 2);
        }
        function setDragPaging(active) {
            var columns = boardPage.querySelector('.mobile-board-columns');
            boardPage.classList.toggle('mobile-dragging', active);
            if (columns) {
                columns.style.scrollSnapType = active ? 'none' : '';
                columns.style.scrollBehavior = active ? 'auto' : '';
            }
        }
        function cancelDrag(current) {
            if (current.ghost) current.ghost.remove();
            if (current.placeholder) current.placeholder.remove();
            current.card.style.display = '';
            current.card.classList.remove('dragging-source');
            if (current.card.parentNode !== current.sourceContainer) {
                current.sourceContainer.appendChild(current.card);
            }
            if (current.scrollFrame) cancelAnimationFrame(current.scrollFrame);
            if (current.edgePauseTimer) clearTimeout(current.edgePauseTimer);
            setDragPaging(false);
            setEdges(false);
        }
        function updateDropTarget(current, point) {
            var target = document.elementFromPoint(point.clientX, point.clientY);
            if (target === current.placeholder) return;
            var targetCard = target && target.closest ? target.closest('.kanban-card') : null;
            if (targetCard === current.card) targetCard = null;
            var targetContainer = targetCard && targetCard.closest('.col-cards');
            if (!targetContainer && target && target.closest) targetContainer = target.closest('.col-cards');
            if (!targetContainer && target && target.closest) {
                var targetColumn = target.closest('.mobile-board-column');
                targetContainer = targetColumn && targetColumn.querySelector('.col-cards');
            }
            if (!targetContainer) return;
            current.targetContainer = targetContainer;

            var cards = targetContainer.querySelectorAll('.kanban-card');
            var before = null;
            for (var i = 0; i < cards.length; i++) {
                if (cards[i] === current.card) continue;
                var rect = cards[i].getBoundingClientRect();
                if (point.clientY < rect.top + rect.height / 2) {
                    before = cards[i];
                    break;
                }
            }
            if (before) {
                if (current.placeholder.nextSibling !== before) targetContainer.insertBefore(current.placeholder, before);
            } else if (current.placeholder.parentNode !== targetContainer || current.placeholder.nextSibling) {
                targetContainer.appendChild(current.placeholder);
            }
        }
        function edgeDirectionFor(current, point) {
            var columns = boardPage.querySelector('.mobile-board-columns');
            if (!columns) return 0;
            var maxScroll = Math.max(0, columns.scrollWidth - columns.clientWidth);
            if (point.clientX < 72 && columns.scrollLeft > 2) return -1;
            if (point.clientX > window.innerWidth - 72 && columns.scrollLeft < maxScroll - 2) return 1;
            return 0;
        }
        function nextColumnScrollLeft(columns, direction) {
            var columnNodes = columns.querySelectorAll('.mobile-board-column');
            var currentScroll = columns.scrollLeft;
            var maxScroll = Math.max(0, columns.scrollWidth - columns.clientWidth);
            var columnsRect = columns.getBoundingClientRect();
            function snapLeft(column) {
                var columnRect = column.getBoundingClientRect();
                var columnLeft = columnRect.left - columnsRect.left + columns.scrollLeft;
                return Math.max(0, Math.min(maxScroll,
                    columnLeft - (columns.clientWidth - columnRect.width) / 2));
            }
            if (direction > 0) {
                for (var i = 0; i < columnNodes.length; i++) {
                    if (snapLeft(columnNodes[i]) > currentScroll + 4) return snapLeft(columnNodes[i]);
                }
                return maxScroll;
            }
            for (var j = columnNodes.length - 1; j >= 0; j--) {
                if (snapLeft(columnNodes[j]) < currentScroll - 4) return snapLeft(columnNodes[j]);
            }
            return 0;
        }
        function continueEdgeScroll(current) {
            if (!current || !current.dragging || !current.edgeDirection) {
                if (current) current.scrollFrame = null;
                return;
            }
            var columns = boardPage.querySelector('.mobile-board-columns');
            if (!columns) return;
            var animationProgress = Math.min(1, (performance.now() - current.edgeAnimationStart) / 350);
            var easedProgress = 1 - Math.pow(1 - animationProgress, 3);
            columns.scrollLeft = current.edgeAnimationFrom +
                (current.edgeTarget - current.edgeAnimationFrom) * easedProgress;
            updateDropTarget(current, current.lastPoint);
            var reachedTarget = animationProgress >= 1;
            if (reachedTarget) {
                columns.scrollLeft = current.edgeTarget;
                current.scrollFrame = null;
                current.edgeAnimationStart = 0;
                current.edgePauseDirection = current.edgeDirection;
                current.edgeDirection = 0;
                setEdges(true);
                current.edgePauseTimer = setTimeout(function () {
                    if (!state || state !== current || !current.dragging) return;
                    current.edgePauseDirection = 0;
                    current.edgePauseTimer = null;
                    current.edgeDirection = edgeDirectionFor(current, current.lastPoint);
                    if (current.edgeDirection) startEdgeScroll(current);
                }, 500);
                return;
            }
            current.scrollFrame = requestAnimationFrame(function () { continueEdgeScroll(current); });
        }
        function startEdgeScroll(current) {
            var columns = boardPage.querySelector('.mobile-board-columns');
            if (!columns || !current.edgeDirection) return;
            current.edgeTarget = nextColumnScrollLeft(columns, current.edgeDirection);
            if (current.edgeTarget === columns.scrollLeft) {
                current.edgeDirection = 0;
                setEdges(true);
                return;
            }
            current.edgeAnimationFrom = columns.scrollLeft;
            current.edgeAnimationStart = performance.now();
            current.scrollFrame = requestAnimationFrame(function () { continueEdgeScroll(current); });
        }
        boardPage.addEventListener('touchstart', function (event) {
            var card = event.target.closest('.kanban-card');
            if (!card || !card.closest('.col-cards')) return;
            var point = event.touches[0];
            state = {
                card: card, sourceContainer: card.closest('.col-cards'), targetContainer: card.closest('.col-cards'),
                x: point.clientX, y: point.clientY, dragging: false,
                lastPoint: {clientX: point.clientX, clientY: point.clientY}, edgeDirection: 0, edgeTarget: 0,
                edgePauseDirection: 0, edgePauseTimer: null, scrollFrame: null,
                edgeAnimationStart: 0, edgeAnimationFrom: 0,
                originalNextSibling: card.nextSibling,
                originalOrder: Array.prototype.map.call(card.closest('.col-cards').querySelectorAll('.kanban-card'), function (item) {
                    return item.getAttribute('data-id');
                }).join(','),
                timer: setTimeout(function () {
                    if (!state || state.card !== card) return;
                    state.dragging = true;
                    card.classList.add('dragging-source');
                    var cardRect = card.getBoundingClientRect();
                    state.placeholder = document.createElement('div');
                    state.placeholder.className = 'touch-drag-placeholder';
                    state.placeholder.style.height = cardRect.height + 'px';
                    state.sourceContainer.insertBefore(state.placeholder, card);
                    state.ghost = card.cloneNode(true);
                    state.ghost.classList.add('touch-drag-ghost');
                    state.ghost.style.width = cardRect.width + 'px';
                    // Fixed-position elements otherwise start at the page's
                    // origin until the first touchmove event. Start the ghost
                    // exactly where the card was grabbed.
                    state.ghost.style.left = cardRect.left + 'px';
                    state.ghost.style.top = cardRect.top + 'px';
                    document.body.appendChild(state.ghost);
                    // The placeholder replaces the source card in layout. Do
                    // not leave the source occupying a second invisible slot.
                    card.style.display = 'none';
                    setDragPaging(true);
                    setEdges(true);
                    if (navigator.vibrate) navigator.vibrate(10);
                }, 350)
            };
        }, {passive: true});
        boardPage.addEventListener('touchmove', function (event) {
            if (!state) return;
            var point = event.touches[0];
            if (!state.dragging) {
                if (Math.abs(point.clientX - state.x) > 8 || Math.abs(point.clientY - state.y) > 8) {
                    clearTimeout(state.timer);
                    state = null;
                }
                return;
            }
            event.preventDefault();
            state.lastPoint = {clientX: point.clientX, clientY: point.clientY};
            state.ghost.style.left = (point.clientX - state.ghost.offsetWidth / 2) + 'px';
            state.ghost.style.top = (point.clientY - 24) + 'px';
            var columns = boardPage.querySelector('.mobile-board-columns');
            if (columns) {
                var newEdgeDirection = edgeDirectionFor(state, point);
                if (state.edgePauseTimer && newEdgeDirection !== state.edgePauseDirection) {
                    clearTimeout(state.edgePauseTimer);
                    state.edgePauseTimer = null;
                    state.edgePauseDirection = 0;
                }
                if (!state.edgePauseTimer) {
                    if (newEdgeDirection !== state.edgeDirection && state.scrollFrame) {
                        cancelAnimationFrame(state.scrollFrame);
                        state.scrollFrame = null;
                    }
                    state.edgeDirection = newEdgeDirection;
                    if (state.edgeDirection && !state.scrollFrame) startEdgeScroll(state);
                }
            }
            setEdges(true);
            updateDropTarget(state, point);
        }, {passive: false});
        boardPage.addEventListener('touchend', function (event) {
            if (!state) return;
            clearTimeout(state.timer);
            var current = state;
            state = null;
            if (!current.dragging) {
                var touch = event.changedTouches[0];
                if (touch && Math.abs(touch.clientX - current.x) < 8 && Math.abs(touch.clientY - current.y) < 8) {
                    window.location.href = '/mobile/boards/' + encodeURIComponent(slug) + '/cards/' + current.card.getAttribute('data-id') + '/';
                }
                return;
            }
            var targetContainer = current.placeholder && current.placeholder.parentNode && current.placeholder.parentNode.classList.contains('col-cards') ? current.placeholder.parentNode : current.sourceContainer;
            targetContainer.insertBefore(current.card, current.placeholder || null);
            current.card.style.display = '';
            current.card.classList.remove('dragging-source');
            if (current.ghost) current.ghost.remove();
            if (current.placeholder) current.placeholder.remove();
            setEdges(false);
            setDragPaging(false);
            if (current.scrollFrame) cancelAnimationFrame(current.scrollFrame);
            if (current.edgePauseTimer) clearTimeout(current.edgePauseTimer);
            boardPage._suppressClickUntil = Date.now() + 500;
            var sourceColumn = current.sourceContainer.getAttribute('data-column-id');
            var targetColumn = targetContainer.getAttribute('data-column-id');
            if (sourceColumn === targetColumn) {
                updateOrder(slug, targetColumn, targetContainer, current.originalOrder);
                return;
            }
            var position = Array.prototype.indexOf.call(targetContainer.querySelectorAll('.kanban-card'), current.card);
            fetch('/boards/' + encodeURIComponent(slug) + '/cards/' + current.card.getAttribute('data-id') + '/move', {
                method: 'POST',
                body: new URLSearchParams({column_id: targetColumn, position: String(Math.max(0, position))})
            }).catch(function () {});
        }, {passive: true});
        boardPage.addEventListener('touchcancel', function () {
            if (!state) return;
            clearTimeout(state.timer);
            cancelDrag(state);
            setEdges(false);
            state = null;
        }, {passive: true});
    }

    ready(function () {
        var left = document.getElementById('mobile-left-toggle');
        var right = document.getElementById('mobile-right-toggle');
        var backdrop = document.getElementById('mobile-drawer-backdrop');
        if (left) left.addEventListener('click', function () { toggleDrawer('left'); });
        if (right) right.addEventListener('click', function () { toggleDrawer('right'); });
        if (backdrop) backdrop.addEventListener('click', function () {
            document.body.classList.remove('mobile-left-open', 'mobile-right-open');
        });
        document.addEventListener('click', function (event) {
            if (!event.target.closest('.mobile-topbar') && !event.target.closest('.mobile-drawer-left') && !event.target.closest('.mobile-drawer-right')) {
                document.body.classList.remove('mobile-left-open', 'mobile-right-open');
            }
        });
        document.addEventListener('keydown', function (event) {
            if (event.key === 'Escape') document.body.classList.remove('mobile-left-open', 'mobile-right-open');
        });
        var columns = document.querySelector('.mobile-board-columns');
        var dots = document.querySelectorAll('.mobile-col-pager span');
        if (columns && dots.length) columns.addEventListener('scroll', function () {
            var index = Math.round(columns.scrollLeft / (columns.clientWidth * .928));
            dots.forEach(function (dot, i) { dot.classList.toggle('active', i === index); });
        }, {passive: true});
        var close = document.getElementById('card-modal-close');
        if (close && document.querySelector('.mobile-card-page')) close.addEventListener('click', function (event) {
            event.preventDefault(); history.back();
        });
        var boardPage = document.querySelector('.mobile-board-page');
        if (boardPage) {
            boardPage.addEventListener('click', function (event) {
                var card = event.target.closest('.kanban-card');
                if (card && !event.defaultPrevented && Date.now() >= (boardPage._suppressClickUntil || 0)) window.location.href = '/mobile/boards/' + encodeURIComponent(boardPage.getAttribute('data-board-slug')) + '/cards/' + card.getAttribute('data-id') + '/';
            });
            setupNativeReorder(boardPage);
            setupTouchReorder(boardPage);
        }
        if (boardPage && window.createBoardSocket) {
            var slug = boardPage.getAttribute('data-board-slug');
            var hadError = false;
            function refreshColumn(columnID) {
                if (!columnID) return;
                fetch('/mobile/boards/' + encodeURIComponent(slug) + '/columns/' + columnID + '/cards')
                    .then(function (response) { return response.text(); })
                    .then(function (html) {
                        var target = document.querySelector('.col-cards[data-column-id="' + columnID + '"]');
                        if (target) target.innerHTML = html;
                    }).catch(function () {});
            }
            window.createBoardSocket(slug, {
                onStateChange: function (state) {
                    var banner = document.getElementById('ws-banner');
                    if (!banner) return;
                    if (state === 'error') hadError = true;
                    banner.classList.toggle('is-hidden', state !== 'error' && !(state === 'connecting' && hadError));
                },
                onEvent: function (event) {
                    var payload = event.payload || {}, ids = [];
                    if (payload.column_id) ids.push(payload.column_id);
                    if (payload.from_column_id) ids.push(payload.from_column_id);
                    if (payload.to_column_id) ids.push(payload.to_column_id);
                    if (!ids.length && payload.card_id) {
                        var card = document.querySelector('.kanban-card[data-id="' + payload.card_id + '"]');
                        var column = card && card.closest('.mobile-board-column');
                        if (column) ids.push(column.getAttribute('data-column-id'));
                    }
                    if (/^column\./.test(event.type)) { window.location.reload(); return; }
                    ids.forEach(refreshColumn);
                }
            }).connect();
            window.addEventListener('pageshow', function (event) {
                if (event.persisted) window.location.reload();
            });
        }
        var cardPage = document.querySelector('.mobile-card-page');
        if (cardPage && window.createBoardSocket) {
            var cardDetail = cardPage.querySelector('.card-detail');
            var cardID = cardDetail && cardDetail.getAttribute('data-card-id');
            var cardSlug = cardPage.getAttribute('data-board-slug');
            if (cardID && cardSlug) {
                window.createBoardSocket(cardSlug, {
                    onEvent: function (event) {
                        var payload = event.payload || {};
                        if (String(payload.card_id || '') !== String(cardID)) return;
                        if (window.CardDetail && window.CardDetail.applyEvent) window.CardDetail.applyEvent(event);
                    }
                }).connect();
            }
        }
        if ('serviceWorker' in navigator) {
            navigator.serviceWorker.register('/static/sw.js', {scope: '/mobile/'}).then(setupPushNotifications).catch(function () {});
        }
    });
}());
