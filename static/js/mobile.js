(function () {
    'use strict';

    function ready(fn) {
        if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', fn);
        else fn();
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
        boardPage.addEventListener('touchstart', function (event) {
            var card = event.target.closest('.kanban-card');
            if (!card || !card.closest('.col-cards')) return;
            var point = event.touches[0];
            state = {
                card: card, container: card.closest('.col-cards'),
                x: point.clientX, y: point.clientY, dragging: false,
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
                    state.container.insertBefore(state.placeholder, card);
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
            state.ghost.style.left = (point.clientX - state.ghost.offsetWidth / 2) + 'px';
            state.ghost.style.top = (point.clientY - 24) + 'px';
            var target = document.elementFromPoint(point.clientX, point.clientY);
            var targetCard = target && target.closest ? target.closest('.kanban-card') : null;
            if (!targetCard || targetCard === state.card || targetCard.parentNode !== state.container) return;
            var rect = targetCard.getBoundingClientRect();
            state.container.insertBefore(state.placeholder, point.clientY < rect.top + rect.height / 2 ? targetCard : targetCard.nextSibling);
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
            current.container.insertBefore(current.card, current.placeholder);
            current.card.style.display = '';
            current.card.classList.remove('dragging-source');
            if (current.ghost) current.ghost.remove();
            if (current.placeholder) current.placeholder.remove();
            updateOrder(slug, current.container.getAttribute('data-column-id'), current.container, current.originalOrder);
        }, {passive: true});
        boardPage.addEventListener('touchcancel', function () {
            if (!state) return;
            clearTimeout(state.timer);
            if (state.ghost) state.ghost.remove();
            if (state.placeholder) state.placeholder.remove();
            state.card.style.display = '';
            state.card.classList.remove('dragging-source');
            state = null;
        }, {passive: true});
    }

    ready(function () {
        var left = document.getElementById('mobile-left-toggle');
        var right = document.getElementById('mobile-right-toggle');
        if (left) left.addEventListener('click', function () { toggleDrawer('left'); });
        if (right) right.addEventListener('click', function () { toggleDrawer('right'); });
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
                if (card && !event.defaultPrevented) window.location.href = '/mobile/boards/' + encodeURIComponent(boardPage.getAttribute('data-board-slug')) + '/cards/' + card.getAttribute('data-id') + '/';
            });
            setupNativeReorder(boardPage);
            setupTouchReorder(boardPage);
        }
        if (boardPage && window.createBoardSocket) {
            var slug = boardPage.getAttribute('data-board-slug');
            var hadError = false;
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
                    if (/^column\./.test(event.type)) { window.location.reload(); return; }
                    ids.forEach(function (id) {
                        fetch('/mobile/boards/' + encodeURIComponent(slug) + '/columns/' + id + '/cards')
                            .then(function (response) { return response.text(); })
                            .then(function (html) {
                                var target = document.querySelector('.col-cards[data-column-id="' + id + '"]');
                                if (target) target.innerHTML = html;
                            }).catch(function () {});
                    });
                }
            }).connect();
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
                        // kanban.js handles local mutation DOM updates. A full
                        // refresh covers events whose detail payload is not
                        // available to the mobile presentation yet.
                        if (/^card\.(title|description|checklist|attachments|label|comment|color|date)/.test(event.type)) {
                            window.location.reload();
                        }
                    }
                }).connect();
            }
        }
        if ('serviceWorker' in navigator) navigator.serviceWorker.register('/static/sw.js', {scope: '/mobile/'}).catch(function () {});
    });
}());
