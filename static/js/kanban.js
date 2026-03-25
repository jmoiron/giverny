$(function() {
    var board = $('#board-outer').data('slug');
    if (!board) return;
    var DEFAULT_LABEL_COLOR = '#888888';
    var collapsedColumnsKey = 'giverny:collapsed-columns:' + board;
    var activeLabelIndex = -1;
    var visibleLabelMatches = [];
    var dragState = {
        cardId: null,
        cardFromColumnId: null,
        cardOriginalIndex: -1,
        cardDraggedEl: null,
        cardPlaceholder: null,
        cardOriginalParent: null,
        cardOriginalNextSibling: null,
        columnId: null,
        columnOriginalIndex: -1,
        columnDraggedEl: null,
        columnPlaceholder: null,
        columnOriginalParent: null,
        columnOriginalNextSibling: null,
        suppressCardClickUntil: 0
    };

    function post(url, data) {
        var body = data instanceof FormData ? data : new URLSearchParams(data);
        return fetch(url, { method: 'POST', body: body });
    }

    function postJSON(url, data) {
        return fetch(url, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(data)
        });
    }

    function nativeEvent(e) {
        return e && (e.originalEvent || e);
    }

    function getCollapsedColumns() {
        try {
            return JSON.parse(localStorage.getItem(collapsedColumnsKey) || '[]');
        } catch (_) {
            return [];
        }
    }

    function setCollapsedColumns(ids) {
        localStorage.setItem(collapsedColumnsKey, JSON.stringify(ids));
    }

    function applyCollapsedColumns() {
        var collapsed = getCollapsedColumns();
        $('.board-column').each(function() {
            var id = Number($(this).data('id'));
            $(this).toggleClass('collapsed', collapsed.indexOf(id) >= 0);
        });
    }

    function toggleCollapsedColumn(colId, shouldCollapse) {
        var collapsed = getCollapsedColumns();
        var idx = collapsed.indexOf(colId);
        if (shouldCollapse && idx === -1) collapsed.push(colId);
        if (!shouldCollapse && idx >= 0) collapsed.splice(idx, 1);
        setCollapsedColumns(collapsed);
        applyCollapsedColumns();
    }

    function normalizeLabelTitle(title) {
        return (title || '').trim().replace(/\s+/g, ' ').toLowerCase();
    }

    function fuzzyFieldScore(value, query) {
        value = normalizeLabelTitle(value);
        query = normalizeLabelTitle(query);
        if (!query) return 0;
        var substringAt = value.indexOf(query);
        if (substringAt >= 0) return substringAt;
        var ti = 0;
        var qi = 0;
        while (ti < value.length && qi < query.length) {
            if (value.charAt(ti) === query.charAt(qi)) qi++;
            ti++;
        }
        return qi === query.length ? 100 + ti : -1;
    }

    function channelLuminance(v) {
        if (v <= 0.03928) return v / 12.92;
        return Math.pow((v + 0.055) / 1.055, 2.4);
    }

    function labelTextClass(color) {
        var hex = String(color || '').trim().replace(/^#/, '');
        if (hex.length !== 6) return 'fg-dark';
        var r = parseInt(hex.slice(0, 2), 16);
        var g = parseInt(hex.slice(2, 4), 16);
        var b = parseInt(hex.slice(4, 6), 16);
        var luminance = 0.2126 * channelLuminance(r / 255) +
            0.7152 * channelLuminance(g / 255) +
            0.0722 * channelLuminance(b / 255);
        return luminance > 0.58 ? 'fg-dark' : 'fg-light';
    }

    function applyCardColorButtonState($scope) {
        var $btn = $scope.find('.quick-color-btn').first();
        if (!$btn.length) return;
        var color = String($btn.data('card-color') || $scope.find('.card-detail').data('card-color') || '').trim();
        var $icon = $btn.find('i').first();
        if (!color) {
            $icon[0].className = 'fa-regular fa-square';
            $icon.css('color', '');
            return;
        }
        $icon[0].className = 'fa-solid fa-square';
        $icon.css('color', color);
    }

    function showCardSave($form) {
        $form.find('.card-actions').removeClass('is-hidden');
    }

    function hideCardSave($form) {
        $form.find('.card-actions').addClass('is-hidden');
    }

    function setRemoteCardState($form, title, content, renderedHTML) {
        var form = $form[0];
        if (!form) return;
        form._remoteTitle = title || '';
        form._remoteContent = content || '';
        form._remoteContentRendered = renderedHTML || '';
    }

    function getRemoteCardState($form) {
        var form = $form[0];
        if (!form) return { title: '', content: '', contentRendered: '' };
        return {
            title: form._remoteTitle || '',
            content: form._remoteContent || '',
            contentRendered: form._remoteContentRendered || ''
        };
    }

    function showCardEditWarning($form) {
        $form.find('#card-edit-warning').removeClass('is-hidden');
    }

    function hideCardEditWarning($form) {
        $form.find('#card-edit-warning').addClass('is-hidden');
    }

    function enableDescriptionEditing($form) {
        if ($form.closest('.card-detail').hasClass('editing-description')) return;
        $form.closest('.card-detail').addClass('editing-description');
        showCardSave($form);
        var textarea = $form.find('#card-content-area')[0];
        if (textarea) {
            textarea.focus();
            textarea.setSelectionRange(textarea.value.length, textarea.value.length);
        }
    }

    function syncCardDescriptionDisplay($form, content, renderedHTML) {
        var $detail = $form.closest('.card-detail');
        var $rendered = $form.find('#card-description-rendered');
        var hasContent = !!String(content || '').trim();
        if (hasContent) {
            $rendered.removeClass('is-empty').html(renderedHTML || '');
        } else {
            $rendered.addClass('is-empty').text('double-click to add a markdown description');
        }
        $detail.removeClass('editing-description');
    }

    function applyRemoteCardState($form) {
        var remote = getRemoteCardState($form);
        $form.find('.card-title-input').val(remote.title);
        $form.find('#card-content-area').val(remote.content);
        syncCardDescriptionDisplay($form, remote.content, remote.contentRendered);
        hideCardSave($form);
        hideCardEditWarning($form);
    }

    function getKnownLabels() {
        var labels = [];
        $('#known-labels-data .known-label-option').each(function() {
            labels.push({
                id: Number($(this).data('label-id')),
                title: String($(this).data('title') || '').trim(),
                color: $(this).data('color') || DEFAULT_LABEL_COLOR,
                textClass: $(this).data('text-class') || labelTextClass($(this).data('color') || DEFAULT_LABEL_COLOR),
                description: $(this).data('description') || ''
            });
        });
        return labels;
    }

    function findKnownLabelByTitle(title) {
        var normalized = normalizeLabelTitle(title);
        if (!normalized) return null;
        var known = getKnownLabels();
        for (var i = 0; i < known.length; i++) {
            if (normalizeLabelTitle(known[i].title) === normalized) return known[i];
        }
        return null;
    }

    function selectedLabelExists(labelId) {
        return $('#card-selected-labels .label-pill[data-label-id="' + labelId + '"]').length > 0;
    }

    function updateSelectedLabelsState() {
        var $selected = $('#card-selected-labels');
        if (!$selected.length) return;
        $selected.toggleClass('is-empty', $selected.find('.label-pill').length === 0);
    }

    function updateArchivedColumnState() {
        var $archivedCards = $('#archived-cards');
        var $empty = $('#archived-empty-state');
        if (!$archivedCards.length || !$empty.length) return;
        $empty.toggleClass('is-hidden', $archivedCards.find('.kanban-card').length > 0);
    }

    function createLabelPill(label) {
        var $pill = $('<span class="label-pill selected"></span>');
        $pill.addClass(label.textClass || labelTextClass(label.color || DEFAULT_LABEL_COLOR));
        $pill.attr('data-label-id', label.id);
        $pill.attr('style', '--label-color: ' + (label.color || DEFAULT_LABEL_COLOR));
        if (label.description) $pill.attr('title', label.description);
        $pill.append($('<span></span>').text(label.title));
        $pill.append(
            $('<a href="#" class="label-remove-btn"></a>')
                .attr('aria-label', 'remove ' + label.title + ' label')
                .append('<i class="fa-solid fa-circle-xmark"></i>')
        );
        $pill.append($('<input type="hidden" name="label_ids">').val(label.id));
        return $pill;
    }

    function createBoardLabelPill(label) {
        var textClass = label.textClass || labelTextClass(label.color || DEFAULT_LABEL_COLOR);
        var $pill = $('<span class="label-pill"></span>');
        $pill.addClass(textClass);
        $pill.attr('data-label-id', label.id);
        $pill.attr('style', '--label-color: ' + (label.color || DEFAULT_LABEL_COLOR));
        if (label.description) $pill.attr('title', label.description);
        $pill.text(label.title || '');
        return $pill;
    }

    function getColumnCardIDs(columnId) {
        return $('.col-cards[data-column-id="' + columnId + '"] .kanban-card').map(function() {
            return Number($(this).data('id'));
        }).get();
    }

    function reorderCardsInColumn(columnId, cardIDs) {
        var $container = $('.col-cards[data-column-id="' + columnId + '"]').first();
        if (!$container.length) return;
        var cardsByID = {};
        $container.find('.kanban-card').each(function() {
            cardsByID[String($(this).data('id'))] = this;
        });
        for (var i = 0; i < cardIDs.length; i++) {
            var el = cardsByID[String(cardIDs[i])];
            if (el) $container.append(el);
        }
    }

    function moveCardToColumn(cardId, columnId) {
        var $card = $('.kanban-card[data-id="' + cardId + '"]').first();
        var $container = $('.col-cards[data-column-id="' + columnId + '"]').first();
        if (!$card.length || !$container.length) return;
        $container.append($card);
    }

    function reorderColumns(columnIDs) {
        var $wrap = $('.board-columns').first();
        if (!$wrap.length) return;
        var columnsByID = {};
        $wrap.children('.board-column').each(function() {
            columnsByID[String($(this).data('id'))] = this;
        });
        var $addWrap = $wrap.children('.add-column-wrap').first();
        for (var i = 0; i < columnIDs.length; i++) {
            var el = columnsByID[String(columnIDs[i])];
            if (!el) continue;
            if ($addWrap.length) {
                $addWrap.before(el);
            } else {
                $wrap.append(el);
            }
        }
    }

    function shouldSuppressCardClick() {
        return Date.now() < dragState.suppressCardClickUntil;
    }

    function markDragComplete() {
        dragState.suppressCardClickUntil = Date.now() + 250;
    }

    function startDragMode(kind) {
        document.body.classList.add('drag-active');
        document.body.classList.remove('drag-card', 'drag-column');
        document.body.classList.add(kind === 'card' ? 'drag-card' : 'drag-column');
    }

    function stopDragMode() {
        document.body.classList.remove('drag-active', 'drag-card', 'drag-column');
    }

    function createDragPlaceholder($source, cls) {
        var width = $source.outerWidth();
        var height = $source.outerHeight();
        return $('<div></div>')
            .addClass(cls)
            .css({
                width: width + 'px',
                height: height + 'px'
            });
    }

    function setDragGhost(ev, el) {
        if (!ev || !ev.dataTransfer || !el) return;
        var ghost = el.cloneNode(true);
        ghost.style.position = 'absolute';
        ghost.style.top = '-10000px';
        ghost.style.left = '-10000px';
        ghost.style.width = el.getBoundingClientRect().width + 'px';
        ghost.style.opacity = '0.7';
        ghost.style.pointerEvents = 'none';
        ghost.classList.add('drag-ghost');
        document.body.appendChild(ghost);
        ev.dataTransfer.setDragImage(ghost, 24, 20);
        setTimeout(function() {
            if (ghost.parentNode) ghost.parentNode.removeChild(ghost);
        }, 0);
    }

    function childIndex(el, selector) {
        if (!el || !el.parentNode) return -1;
        var matches = el.parentNode.querySelectorAll(selector);
        for (var i = 0; i < matches.length; i++) {
            if (matches[i] === el) return i;
        }
        return -1;
    }

    function findInsertBeforeByAxis(items, pointer, axis) {
        for (var i = 0; i < items.length; i++) {
            var rect = items[i].getBoundingClientRect();
            var midpoint = axis === 'x' ? rect.left + rect.width / 2 : rect.top + rect.height / 2;
            if (pointer < midpoint) return items[i];
        }
        return null;
    }

    function isNoOpColumnPlacement(beforeEl, boardColumns) {
        if (!dragState.columnDraggedEl) return false;
        var currentIndex = childIndex(dragState.columnDraggedEl, '.board-column');
        var targetIndex = beforeEl ? childIndex(beforeEl, '.board-column') : boardColumns.querySelectorAll('.board-column').length;
        return targetIndex === currentIndex || targetIndex === currentIndex + 1;
    }

    function isNoOpCardPlacement(beforeEl, colCards) {
        if (!dragState.cardDraggedEl) return false;
        var targetColumnId = Number($(colCards).data('column-id'));
        if (targetColumnId !== dragState.cardFromColumnId) return false;
        var currentIndex = childIndex(dragState.cardDraggedEl, '.kanban-card');
        var targetIndex = beforeEl ? childIndex(beforeEl, '.kanban-card') : colCards.querySelectorAll('.kanban-card').length;
        return targetIndex === currentIndex || targetIndex === currentIndex + 1;
    }

    function resetCardDrag() {
        if (dragState.cardPlaceholder && dragState.cardPlaceholder.parentNode) {
            dragState.cardPlaceholder.parentNode.removeChild(dragState.cardPlaceholder);
        }
        if (dragState.cardDraggedEl) {
            $(dragState.cardDraggedEl).removeClass('dragging-source');
        }
        dragState.cardId = null;
        dragState.cardFromColumnId = null;
        dragState.cardOriginalIndex = -1;
        dragState.cardDraggedEl = null;
        dragState.cardPlaceholder = null;
        dragState.cardOriginalParent = null;
        dragState.cardOriginalNextSibling = null;
    }

    function resetColumnDrag() {
        if (dragState.columnPlaceholder && dragState.columnPlaceholder.parentNode) {
            dragState.columnPlaceholder.parentNode.removeChild(dragState.columnPlaceholder);
        }
        if (dragState.columnDraggedEl) {
            $(dragState.columnDraggedEl).removeClass('dragging-source');
        }
        dragState.columnId = null;
        dragState.columnOriginalIndex = -1;
        dragState.columnDraggedEl = null;
        dragState.columnPlaceholder = null;
        dragState.columnOriginalParent = null;
        dragState.columnOriginalNextSibling = null;
    }

    function restoreCardDragPosition() {
        if (!dragState.cardDraggedEl || !dragState.cardOriginalParent) return;
        if (dragState.cardOriginalNextSibling && dragState.cardOriginalNextSibling.parentNode === dragState.cardOriginalParent) {
            dragState.cardOriginalParent.insertBefore(dragState.cardDraggedEl, dragState.cardOriginalNextSibling);
            return;
        }
        dragState.cardOriginalParent.appendChild(dragState.cardDraggedEl);
    }

    function restoreColumnDragPosition() {
        if (!dragState.columnDraggedEl || !dragState.columnOriginalParent) return;
        if (dragState.columnOriginalNextSibling && dragState.columnOriginalNextSibling.parentNode === dragState.columnOriginalParent) {
            dragState.columnOriginalParent.insertBefore(dragState.columnDraggedEl, dragState.columnOriginalNextSibling);
            return;
        }
        dragState.columnOriginalParent.appendChild(dragState.columnDraggedEl);
    }

    function getActiveCardForm(cardId) {
        var $form = $('#card-detail-form');
        if (!$form.length) return $();
        if (Number($form.data('id')) !== Number(cardId)) return $();
        return $form;
    }

    function isCardViewing($form) {
        return !!($form.length && $form.find('.card-actions').hasClass('is-hidden'));
    }

    function getCardLabelContainer(cardId, createIfMissing) {
        var $card = $('.kanban-card[data-id="' + cardId + '"]');
        if (!$card.length) return $();
        var $labels = $card.find('.card-labels').first();
        if (!$labels.length && createIfMissing) {
            $labels = $('<div class="card-labels"></div>');
            $card.append($labels);
        }
        return $labels;
    }

    function applyCardLabelAdded(payload) {
        if (!payload || !payload.card_id || !payload.label || !payload.label.id) return;
        var $labels = getCardLabelContainer(payload.card_id, true);
        if (!$labels.length) return;
        if ($labels.find('.label-pill[data-label-id="' + payload.label.id + '"]').length) return;
        $labels.append(createBoardLabelPill({
            id: payload.label.id,
            title: payload.label.title,
            color: payload.label.color,
            textClass: payload.label.text_class,
            description: payload.label.description
        }));
    }

    function applyCardLabelRemoved(payload) {
        if (!payload || !payload.card_id || !payload.label_id) return;
        var $labels = getCardLabelContainer(payload.card_id, false);
        if (!$labels.length) return;
        $labels.find('.label-pill[data-label-id="' + payload.label_id + '"]').remove();
        if (!$labels.find('.label-pill').length) {
            $labels.remove();
        }
    }

    function applyCardTitleModified(payload) {
        if (!payload || !payload.card_id) return;
        $('.kanban-card[data-id="' + payload.card_id + '"] .card-title').text(payload.title || '');
        var $form = getActiveCardForm(payload.card_id);
        if (!$form.length) return;
        var remote = getRemoteCardState($form);
        setRemoteCardState($form, payload.title || '', remote.content, remote.contentRendered);
        if (isCardViewing($form)) {
            $form.find('.card-title-input').val(payload.title || '');
            hideCardEditWarning($form);
            return;
        }
        showCardEditWarning($form);
    }

    function applyCardDescriptionModified(payload) {
        if (!payload || !payload.card_id) return;
        var $form = getActiveCardForm(payload.card_id);
        if (!$form.length) return;
        var remote = getRemoteCardState($form);
        setRemoteCardState($form, remote.title, payload.content || '', payload.content_rendered || '');
        if (isCardViewing($form)) {
            $form.find('#card-content-area').val(payload.content || '');
            syncCardDescriptionDisplay($form, payload.content, payload.content_rendered);
            hideCardEditWarning($form);
            return;
        }
        showCardEditWarning($form);
    }

    function applyCardReordered(payload) {
        if (!payload || !payload.column_id || !payload.card_ids) return;
        reorderCardsInColumn(payload.column_id, payload.card_ids);
    }

    function applyCardCreated(payload) {
        if (!payload || !payload.card_id || !payload.column_id || !payload.html) return;
        var $container = $('.col-cards[data-column-id="' + payload.column_id + '"]').first();
        if (!$container.length) return;
        if ($container.find('.kanban-card[data-id="' + payload.card_id + '"]').length) return;
        $container.append(payload.html);
    }

    function applyCardMoved(payload) {
        if (!payload || !payload.from_column_id || !payload.to_column_id) return;
        moveCardToColumn(payload.card_id, payload.to_column_id);
        reorderCardsInColumn(payload.from_column_id, payload.from_card_ids || []);
        reorderCardsInColumn(payload.to_column_id, payload.to_card_ids || []);
    }

    function applyColumnReordered(payload) {
        if (!payload || !payload.column_ids) return;
        reorderColumns(payload.column_ids);
    }

    function applyColumnChanged(payload) {
        if (!payload || !payload.columns) return;
        var columnModal = document.getElementById('column-detail-form');
        payload.columns.forEach(function(column) {
            if (!column || !column.id) return;
            var $boardColumn = $('.board-column[data-id="' + column.id + '"]').first();
            if (!$boardColumn.length) return;

            $boardColumn.find('.col-name').first().text(column.name || '');

            var $wipBadge = $boardColumn.find('.wip-badge').first();
            if (column.wip_limit) {
                if (!$wipBadge.length) {
                    $wipBadge = $('<span class="wip-badge"></span>');
                    var $name = $boardColumn.find('.col-name').first();
                    if ($name.length) {
                        $name.after($wipBadge);
                    } else {
                        $boardColumn.find('.col-header').first().append($wipBadge);
                    }
                }
                $wipBadge.text(column.wip_limit);
            } else if ($wipBadge.length) {
                $wipBadge.remove();
            }

            var editForm = document.getElementById('edit-col-' + column.id);
            if (editForm) {
                if (editForm.elements.name) editForm.elements.name.value = column.name || '';
                if (editForm.elements.wip_limit) editForm.elements.wip_limit.value = String(column.wip_limit || 0);
                if (editForm.elements.color) editForm.elements.color.value = column.color || '';
                if (editForm.elements.done) editForm.elements.done.value = column.done ? '1' : '0';
                if (editForm.elements.late) editForm.elements.late.value = column.late ? '1' : '0';
            }

            var $editAction = $boardColumn.find('.column-edit-action').first();
            if ($editAction.length) {
                $editAction.attr('data-name', column.name || '');
                $editAction.attr('data-wip', column.wip_limit || 0);
                $editAction.attr('data-color', column.color || '');
                $editAction.attr('data-done', column.done ? '1' : '0');
                $editAction.attr('data-late', column.late ? '1' : '0');
            }

            if (columnModal && columnModal.action === '/boards/' + board + '/columns/' + column.id + '/edit') {
                columnModal.elements.name.value = column.name || '';
                columnModal.elements.wip_limit.value = String(column.wip_limit || 0);
                columnModal.elements.color.value = column.color || '';
                document.getElementById('column-late-input').checked = !!column.late;
                document.getElementById('column-done-input').checked = !!column.done;
                document.getElementById('column-late-value').value = column.late ? '1' : '0';
                document.getElementById('column-done-value').value = column.done ? '1' : '0';
                document.getElementById('column-done-input').disabled = !!column.done;
                $('#column-done-note').toggle(!!column.done);
            }
        });
    }

    function applyCardDeleted(payload) {
        if (!payload || !payload.card_id) return;
        $('.kanban-card[data-id="' + payload.card_id + '"]').remove();
        updateArchivedColumnState();
        var $form = getActiveCardForm(payload.card_id);
        if ($form.length) {
            $cardModal.removeClass('active');
        }
    }

    function applyBoardEvent(evt) {
        switch (evt.type) {
        case 'card.created':
            applyCardCreated(evt.payload);
            break;
        case 'card.deleted':
            applyCardDeleted(evt.payload);
            break;
        case 'card.reorder':
            applyCardReordered(evt.payload);
            break;
        case 'card.move':
            applyCardMoved(evt.payload);
            break;
        case 'card.title.modified':
            applyCardTitleModified(evt.payload);
            break;
        case 'card.description.modified':
            applyCardDescriptionModified(evt.payload);
            break;
        case 'column.reorder':
            applyColumnReordered(evt.payload);
            break;
        case 'column.changed':
            applyColumnChanged(evt.payload);
            break;
        case 'card.label.added':
            applyCardLabelAdded(evt.payload);
            break;
        case 'card.label.removed':
            applyCardLabelRemoved(evt.payload);
            break;
        }
    }

    function ensureKnownLabelOption(label) {
        if (findKnownLabelByTitle(label.title)) return;
        var $option = $('<div class="known-label-option"></div>');
        $option.attr('data-title', label.title);
        $option.attr('data-label-id', label.id);
        $option.attr('data-color', label.color || DEFAULT_LABEL_COLOR);
        $option.attr('data-text-class', label.textClass || labelTextClass(label.color || DEFAULT_LABEL_COLOR));
        $option.attr('data-description', label.description || '');
        $('#known-labels-data').append($option);
    }

    function attachLabelToEditor(label) {
        if (!label || !label.id || selectedLabelExists(label.id)) return;
        $('#card-selected-labels').append(createLabelPill(label));
        $('#card-label-input').val('');
        updateSelectedLabelsState();
    }

    function hideLabelSuggestions() {
        $('#card-label-suggestions').hide().empty();
        activeLabelIndex = -1;
        visibleLabelMatches = [];
    }

    function renderLabelSuggestions(matches) {
        var $suggestions = $('#card-label-suggestions');
        visibleLabelMatches = matches;
        activeLabelIndex = matches.length ? 0 : -1;
        if (!matches.length) {
            hideLabelSuggestions();
            return;
        }
        var html = '';
        for (var i = 0; i < matches.length; i++) {
            var label = matches[i];
            var cls = 'label-suggestion' + (i === activeLabelIndex ? ' active' : '');
            var titleAttr = label.description ? ' title="' + $('<div>').text(label.description).html() + '"' : '';
            html += '<button type="button" class="' + cls + '" data-index="' + i + '"' + titleAttr + '>' +
                '<span class="label-pill ' + label.textClass + '" style="--label-color: ' + label.color + '">' +
                $('<div>').text(label.title).html() + '</span>' +
                '</button>';
        }
        $suggestions.html(html).show();
    }

    function updateLabelSuggestionSelection() {
        $('#card-label-suggestions .label-suggestion').removeClass('active')
            .eq(activeLabelIndex).addClass('active');
    }

    function refreshLabelSuggestions() {
        var query = $('#card-label-input').val();
        var labels = getKnownLabels().filter(function(label) {
            return !selectedLabelExists(label.id);
        });
        var matches;
        if (!query.trim()) {
            matches = labels
                .sort(function(a, b) { return a.title.localeCompare(b.title); })
                .slice(0, 5);
            renderLabelSuggestions(matches);
            return;
        }
        matches = labels
            .map(function(label) {
                var titleScore = fuzzyFieldScore(label.title, query);
                var descriptionScore = fuzzyFieldScore(label.description || '', query);
                var bestScore = -1;
                if (titleScore >= 0) bestScore = titleScore;
                if (descriptionScore >= 0) {
                    var weightedDescriptionScore = descriptionScore + 250;
                    bestScore = bestScore >= 0 ? Math.min(bestScore, weightedDescriptionScore) : weightedDescriptionScore;
                }
                return { label: label, score: bestScore };
            })
            .filter(function(item) { return item.score >= 0; })
            .sort(function(a, b) {
                if (a.score !== b.score) return a.score - b.score;
                return a.label.title.localeCompare(b.label.title);
            })
            .slice(0, 8)
            .map(function(item) { return item.label; });
        renderLabelSuggestions(matches);
    }

    function persistAddLabel(cardId, label) {
        return post('/boards/' + board + '/cards/' + cardId + '/labels', { label_id: label.id })
            .then(function(r) { return r.json(); })
            .then(function() {
                attachLabelToEditor(label);
                refreshLabelSuggestions();
            });
    }

    function addLabelFromInput(cardId) {
        var rawTitle = $('#card-label-input').val();
        if (activeLabelIndex >= 0 && visibleLabelMatches[activeLabelIndex]) {
            return persistAddLabel(cardId, visibleLabelMatches[activeLabelIndex]);
        }
        var existing = findKnownLabelByTitle(rawTitle);
        if (existing) {
            return persistAddLabel(cardId, existing);
        }
        var title = (rawTitle || '').trim().replace(/\s+/g, ' ');
        if (!title) return Promise.resolve();
        return post('/labels/quick', { title: title })
            .then(function(r) { return r.json(); })
            .then(function(data) {
                if (!data.label) return;
                ensureKnownLabelOption(data.label);
                return persistAddLabel(cardId, data.label);
            });
    }

    // --- WebSocket connection ---
    var $wsIndicator = $('#ws-indicator');
    var $wsEventLog  = $('#ws-event-log');
    var wsEvents     = [];
    var MAX_EVENTS   = 5;
    var wsEventSeq   = 0;
    var ws          = null;
    var wsState     = 'idle';
    var reconnectTimer = null;
    var reconnectDelay = 1000;
    var MAX_RECONNECT_DELAY = 30000;
    var CONNECT_TIMEOUT_MS = 5000;
    var connectTimer = null;
    var wsURL = (location.protocol === 'https:' ? 'wss' : 'ws') + '://' + location.host + '/boards/' + board + '/ws';

    function pad(n) { return n < 10 ? '0' + n : n; }

    function pushWSEvent(type, payload) {
        var now = new Date();
        var time = pad(now.getHours()) + ':' + pad(now.getMinutes()) + ':' + pad(now.getSeconds());
        wsEvents.push({
            id: ++wsEventSeq,
            type: type || 'message',
            time: time,
            json: $('<div>').text(JSON.stringify({
                type: type || 'message',
                payload: payload || {}
            }, null, 2)).html(),
            expanded: false
        });
        if (wsEvents.length > MAX_EVENTS) wsEvents.shift();
        if (isShown($wsEventLog)) renderEventLog();
    }

    function setWSState(state) {
        wsState = state;
        var el = $wsIndicator[0];
        if (!el) return;
        el.className = 'fa-solid fa-plug ws-indicator ' + state;
    }

    function renderEventLog() {
        if (wsEvents.length === 0) {
            $wsEventLog.html('<div class="ws-event-empty">no events yet</div>');
            return;
        }
        var html = '';
        for (var i = wsEvents.length - 1; i >= 0; i--) {
            var ev = wsEvents[i];
            var expanded = ev.expanded ? ' expanded' : '';
            var toggleIcon = ev.expanded ? 'fa-solid fa-chevron-up' : 'fa-solid fa-chevron-down';
            html += '<div class="ws-event-entry' + expanded + '" data-ws-event-id="' + ev.id + '">' +
                '<div class="ws-event-header">' +
                '<i class="' + toggleIcon + ' ws-event-toggle"></i>' +
                '<span class="ws-event-type">' + ev.type + '</span>' +
                '<span class="ws-event-time">' + ev.time + '</span>' +
                '</div>' +
                '<div class="ws-event-payload"' + (ev.expanded ? '' : ' style="display:none"') + '>' +
                '<pre><code class="language-json">' + ev.json + '</code></pre>' +
                '</div>' +
                '</div>';
        }
        $wsEventLog.html(html);
        if (window.hljs) {
            $wsEventLog.find('code.language-json').each(function() {
                window.hljs.highlightElement(this);
            });
        }
    }

    function flashIndicator() {
        if (wsState !== 'connected') return;
        var $i = $wsIndicator;
        $i[0].className = 'fa-solid fa-plug-circle-bolt ws-indicator flash';
        setTimeout(function() {
            if (wsState === 'connected') {
                $i[0].className = 'fa-solid fa-plug ws-indicator connected';
            }
        }, 400);
    }

    function isShown($el) {
        var el = $el && $el[0];
        if (!el) return false;
        return el.style.display !== 'none' && el.offsetParent !== null;
    }

    function clearReconnectTimer() {
        if (reconnectTimer) {
            clearTimeout(reconnectTimer);
            reconnectTimer = null;
        }
    }

    function clearConnectTimer() {
        if (connectTimer) {
            clearTimeout(connectTimer);
            connectTimer = null;
        }
    }

    function scheduleReconnect(delay) {
        if (reconnectTimer || wsState === 'connecting' || wsState === 'connected') return;
        pushWSEvent('ws.reconnect.scheduled', { delay_ms: Math.max(0, delay || 0) });
        reconnectTimer = setTimeout(function() {
            reconnectTimer = null;
            connectWS();
        }, Math.max(0, delay || 0));
    }

    function connectWS() {
        if (wsState === 'connecting') return;
        if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) return;
        clearReconnectTimer();
        setWSState('connecting');
        pushWSEvent('ws.connect.attempt', { url: wsURL, timeout_ms: CONNECT_TIMEOUT_MS });
        ws = new WebSocket(wsURL);
        clearConnectTimer();
        connectTimer = setTimeout(function() {
            if (ws && ws.readyState === WebSocket.CONNECTING) {
                pushWSEvent('ws.connect.timeout', { timeout_ms: CONNECT_TIMEOUT_MS });
                try { ws.close(); } catch (_) {}
            }
        }, CONNECT_TIMEOUT_MS);

        ws.onopen = function() {
            clearConnectTimer();
            reconnectDelay = 1000;
            setWSState('connected');
            pushWSEvent('ws.connected', {});
        };

        ws.onclose = function(event) {
            clearConnectTimer();
            if (ws && ws.readyState === WebSocket.CLOSED) {
                ws = null;
            }
            setWSState('error');
            pushWSEvent('ws.closed', {
                code: event && typeof event.code === 'number' ? event.code : null,
                reason: event && event.reason ? event.reason : '',
                was_clean: !!(event && event.wasClean)
            });
            scheduleReconnect(reconnectDelay);
            reconnectDelay = Math.min(MAX_RECONNECT_DELAY, Math.round(reconnectDelay * 1.8));
        };

        ws.onerror = function() {
            clearConnectTimer();
            pushWSEvent('ws.error', { state: ws ? ws.readyState : null });
            if (ws && ws.readyState !== WebSocket.CLOSED) {
                try { ws.close(); } catch (_) {}
            }
        };

        ws.onmessage = function(e) {
            var evt = JSON.parse(e.data);
            applyBoardEvent(evt);
            pushWSEvent(evt.type || 'message', evt.payload || evt);
            flashIndicator();
        };
    }

    $('#ws-icon-row').on('click', function() {
        if (wsState === 'error' && !reconnectTimer && (!ws || ws.readyState === WebSocket.CLOSED)) {
            reconnectDelay = 1000;
            connectWS();
            return;
        }
        var open = isShown($wsEventLog);
        $wsEventLog[0].style.display = open ? 'none' : 'block';
        if (!open) renderEventLog();
    });

    $('#ws-status-box').on('click', function(event) {
        event.stopPropagation();
    });

    $wsEventLog.on('click', '.ws-event-header', function(event) {
        event.stopPropagation();
        var id = Number($(this).closest('.ws-event-entry').data('ws-event-id'));
        for (var i = 0; i < wsEvents.length; i++) {
            if (wsEvents[i].id === id) {
                wsEvents[i].expanded = !wsEvents[i].expanded;
                break;
            }
        }
        renderEventLog();
    });

    // Close if clicking outside the status box.
    $(document).on('click', function(e) {
        if (!$('#ws-status-box')[0].contains(e.target)) {
            $wsEventLog.hide();
        }
    });

    applyCollapsedColumns();
    connectWS();

    // --- Add column toggle ---
    $('#add-column-btn').on('click', function() {
        $(this).hide();
        $('.add-column-form').show().find('input[name=name]')[0].focus();
    });

    $('#archived-toggle-link').on('click', function(e) {
        e.preventDefault();
        $('#archived-column').toggleClass('is-hidden');
        updateArchivedColumnState();
    });

    $(document).on('click', '.add-column-form .btn-cancel', function() {
        $('.add-column-form').hide()[0].reset();
        $('#add-column-btn').show();
    });

    // --- Add card inline ---
    $(document).on('click', '.add-card-btn', function() {
        var $col = $(this).closest('.board-column');
        $(this).hide();
        $col.find('.add-card-form').show().find('input[name=title]')[0].focus();
    });

    $(document).on('keydown', '.add-card-form input, .add-column-form input', function(e) {
        if (e.key === 'Escape') $(this).closest('form').find('.btn-cancel')[0].click();
    });

    $(document).on('click', '.add-card-form .btn-cancel', function() {
        var $form = $(this).closest('.add-card-form');
        $form.hide()[0].reset();
        $form.closest('.board-column').find('.add-card-btn').show();
    });

    $(document).on('submit', '.add-card-form', function(e) {
        e.preventDefault();
        var $form = $(this);
        var colId = $form.data('col');
        post('/boards/' + board + '/columns/' + colId + '/cards', new URLSearchParams(new FormData($form[0])))
            .then(function(r) { return r.text(); })
            .then(function() {
                $form.hide()[0].reset();
                $form.closest('.board-column').find('.add-card-btn').show();
            });
    });

    $(document).on('click', '.col-menu-trigger', function(e) {
        e.stopPropagation();
        var $actions = $(this).closest('.col-actions');
        $('.col-actions').not($actions).removeClass('open');
        $actions.toggleClass('open');
    });

    $(document).on('click', '.col-dropdown', function(e) {
        e.stopPropagation();
    });

    $(document).on('click', '[data-submit]', function(e) {
        e.preventDefault();
        e.stopPropagation();
        var formId = $(this).data('submit');
        var message = $(this).data('confirm');
        if (message && !window.confirm(message)) return;
        var form = document.getElementById(formId);
        if (form) form.submit();
    });

    $(document).on('click', '#archived-delete-all-btn', function(e) {
        e.preventDefault();
        e.stopPropagation();
        if (!window.confirm('Delete all archived cards permanently?')) return;
        post('/boards/' + board + '/cards/archived/delete', {})
            .then(function(r) { return r.json(); })
            .then(function() {
                $('#archived-cards .kanban-card').remove();
                updateArchivedColumnState();
                $('.col-actions').removeClass('open');
            });
    });

    $(document).on('click', '.col-collapse-btn', function(e) {
        e.stopPropagation();
        var $col = $(this).closest('.board-column');
        toggleCollapsedColumn(Number($col.data('id')), true);
        $('.col-actions').removeClass('open');
    });

    $(document).on('click', '.board-column.collapsed', function(e) {
        if ($(e.target).closest('.col-actions').length) return;
        toggleCollapsedColumn(Number($(this).data('id')), false);
    });

    var $columnModal = $('#column-modal');

    $(document).on('click', '.column-edit-action', function(e) {
        e.stopPropagation();
        var $trigger = $(this);
        var formId = $trigger.data('form');
        var form = document.getElementById(formId);
        if (!form) return;
        var modalForm = document.getElementById('column-detail-form');
        var doneInput = document.getElementById('column-done-input');
        var lateInput = document.getElementById('column-late-input');
        var doneValue = document.getElementById('column-done-value');
        var lateValue = document.getElementById('column-late-value');
        var isDone = String($trigger.data('done')) === '1';
        var isLate = String($trigger.data('late')) === '1';
        modalForm.action = form.action;
        modalForm.elements.name.value = $trigger.data('name') || '';
        modalForm.elements.wip_limit.value = form.elements.wip_limit ? form.elements.wip_limit.value : '0';
        modalForm.elements.color.value = form.elements.color ? form.elements.color.value : '';
        lateInput.checked = isLate;
        doneInput.checked = isDone;
        lateValue.value = isLate ? '1' : '0';
        doneValue.value = isDone ? '1' : '0';
        doneInput.disabled = isDone;
        $('#column-done-note').toggle(isDone);
        $columnModal.addClass('active');
        $('.col-actions').removeClass('open');
        setTimeout(function() {
            var input = document.getElementById('column-name-input');
            if (!input) return;
            input.focus();
            input.select();
        }, 0);
    });

    $(document).on('click', '#column-modal-close', function(e) {
        e.preventDefault();
        $columnModal.removeClass('active');
    });

    $columnModal.on('click', function(e) {
        if (e.target === this) $columnModal.removeClass('active');
    });

    $('#column-detail-form').on('submit', function() {
        var doneInput = document.getElementById('column-done-input');
        var lateInput = document.getElementById('column-late-input');
        document.getElementById('column-late-value').value = lateInput.checked ? '1' : '0';
        document.getElementById('column-done-value').value = doneInput.checked ? '1' : '0';
    });

    $(document).on('click', function(e) {
        if (!$(e.target).closest('.col-actions').length) {
            $('.col-actions').removeClass('open');
        }
    });

    // --- Card detail modal ---
    var $cardModal = $('#card-modal');

    $(document).on('click', '.kanban-card', function() {
        if (shouldSuppressCardClick()) return;
        var cardId = $(this).data('id');
        fetch('/boards/' + board + '/cards/' + cardId)
            .then(function(r) { return r.text(); })
            .then(function(html) {
                $cardModal.find('.card-modal-inner').html(html);
                applyCardColorButtonState($cardModal);
                var $form = $cardModal.find('#card-detail-form');
                updateSelectedLabelsState();
                setRemoteCardState(
                    $form,
                    $form.find('.card-title-input').val(),
                    $form.find('#card-content-area').val(),
                    $form.find('#card-description-rendered').html()
                );
                hideCardEditWarning($form);
                $cardModal.addClass('active');
            });
    });

    document.addEventListener('dragstart', function(ev) {
        var header = ev.target.closest('.col-header');
        if (header) {
            if (ev.target.closest('.col-actions')) {
                ev.preventDefault();
                return;
            }
            var $column = $(header).closest('.board-column');
            dragState.columnId = Number($column.data('id'));
            dragState.columnOriginalIndex = $column.index();
            dragState.columnDraggedEl = $column[0];
            dragState.columnOriginalParent = $column[0].parentNode;
            dragState.columnOriginalNextSibling = $column[0].nextSibling;
            dragState.columnPlaceholder = null;
            if (ev.dataTransfer) {
                ev.dataTransfer.effectAllowed = 'move';
                ev.dataTransfer.setData('text/plain', 'column:' + dragState.columnId);
            }
            setDragGhost(ev, $column[0]);
            startDragMode('column');
            $column.addClass('dragging-source');
            return;
        }

        var card = ev.target.closest('.kanban-card');
        if (!card) return;
        var $card = $(card);
        dragState.cardId = Number($card.data('id'));
        dragState.cardFromColumnId = Number($card.closest('.col-cards').data('column-id'));
        dragState.cardOriginalIndex = $card.index();
        dragState.cardDraggedEl = card;
        dragState.cardOriginalParent = card.parentNode;
        dragState.cardOriginalNextSibling = card.nextSibling;
        dragState.cardPlaceholder = null;
        if (ev.dataTransfer) {
            ev.dataTransfer.effectAllowed = 'move';
            ev.dataTransfer.setData('text/plain', 'card:' + dragState.cardId);
        }
        setDragGhost(ev, card);
        startDragMode('card');
        $card.addClass('dragging-source');
    }, true);

    document.addEventListener('dragover', function(ev) {
        var boardColumns = ev.target.closest('.board-columns');
        if (boardColumns && dragState.columnId) {
            ev.preventDefault();
            var placeholder = dragState.columnPlaceholder;
            var columns = boardColumns.querySelectorAll('.board-column:not(.dragging-source)');
            var before = findInsertBeforeByAxis(columns, ev.clientX, 'x');
            if (isNoOpColumnPlacement(before, boardColumns)) {
                if (placeholder && placeholder.parentNode) {
                    placeholder.parentNode.removeChild(placeholder);
                }
                return;
            }
            if (!placeholder) {
                placeholder = createDragPlaceholder($(dragState.columnDraggedEl), 'column-drop-placeholder')[0];
                dragState.columnPlaceholder = placeholder;
            }
            if (before) {
                before.parentNode.insertBefore(placeholder, before);
            } else {
                var addWrap = boardColumns.querySelector('.add-column-wrap');
                if (addWrap) {
                    addWrap.parentNode.insertBefore(placeholder, addWrap);
                } else {
                    boardColumns.appendChild(placeholder);
                }
            }
            return;
        }

        var colCards = ev.target.closest('.col-cards');
        if (colCards && dragState.cardId) {
            ev.preventDefault();
            var cardPlaceholder = dragState.cardPlaceholder;
            var cards = colCards.querySelectorAll('.kanban-card:not(.dragging-source)');
            var beforeCard = findInsertBeforeByAxis(cards, ev.clientY, 'y');
            if (isNoOpCardPlacement(beforeCard, colCards)) {
                if (cardPlaceholder && cardPlaceholder.parentNode) {
                    cardPlaceholder.parentNode.removeChild(cardPlaceholder);
                }
                return;
            }
            if (!cardPlaceholder) {
                cardPlaceholder = createDragPlaceholder($(dragState.cardDraggedEl), 'card-drop-placeholder')[0];
                dragState.cardPlaceholder = cardPlaceholder;
            }
            if (beforeCard) {
                beforeCard.parentNode.insertBefore(cardPlaceholder, beforeCard);
            } else {
                colCards.appendChild(cardPlaceholder);
            }
        }
    }, true);

    document.addEventListener('drop', function(ev) {
        var boardColumns = ev.target.closest('.board-columns');
        if (boardColumns && dragState.columnId && dragState.columnDraggedEl) {
            ev.preventDefault();
            if (dragState.columnPlaceholder && dragState.columnPlaceholder.parentNode) {
                dragState.columnPlaceholder.parentNode.insertBefore(dragState.columnDraggedEl, dragState.columnPlaceholder);
            } else {
                restoreColumnDragPosition();
            }
            var ids = $(boardColumns).children('.board-column').map(function() {
                return Number($(this).data('id'));
            }).get();
            var changed = ids.indexOf(dragState.columnId) !== dragState.columnOriginalIndex;
            if (changed) {
                postJSON('/boards/' + board + '/columns/reorder', ids);
                markDragComplete();
            }
            resetColumnDrag();
            stopDragMode();
            return;
        }

        var colCards = ev.target.closest('.col-cards');
        if (colCards && dragState.cardId && dragState.cardDraggedEl) {
            ev.preventDefault();
            if (dragState.cardPlaceholder && dragState.cardPlaceholder.parentNode) {
                dragState.cardPlaceholder.parentNode.insertBefore(dragState.cardDraggedEl, dragState.cardPlaceholder);
            } else {
                restoreCardDragPosition();
            }
            var toColumnId = Number($(colCards).data('column-id'));
            var cardId = dragState.cardId;
            var toCardIDs = getColumnCardIDs(toColumnId);
            var nextIndex = toCardIDs.indexOf(cardId);
            if (dragState.cardFromColumnId === toColumnId) {
                if (nextIndex !== dragState.cardOriginalIndex) {
                    postJSON('/boards/' + board + '/columns/' + toColumnId + '/cards/reorder', toCardIDs);
                    markDragComplete();
                }
                resetCardDrag();
                stopDragMode();
                return;
            }
            post('/boards/' + board + '/cards/' + cardId + '/move', {
                column_id: toColumnId,
                position: nextIndex
            });
            markDragComplete();
            resetCardDrag();
            stopDragMode();
        }
    }, true);

    document.addEventListener('dragend', function(ev) {
        if (ev.target.closest('.col-header')) {
            if (dragState.columnDraggedEl && dragState.columnPlaceholder && dragState.columnPlaceholder.parentNode) {
                dragState.columnPlaceholder.parentNode.insertBefore(dragState.columnDraggedEl, dragState.columnPlaceholder);
            } else {
                restoreColumnDragPosition();
            }
            resetColumnDrag();
            stopDragMode();
            return;
        }
        if (ev.target.closest('.kanban-card')) {
            if (dragState.cardDraggedEl && dragState.cardPlaceholder && dragState.cardPlaceholder.parentNode) {
                dragState.cardPlaceholder.parentNode.insertBefore(dragState.cardDraggedEl, dragState.cardPlaceholder);
            } else {
                restoreCardDragPosition();
            }
            resetCardDrag();
            stopDragMode();
        }
    }, true);

    // Close card modal
    $(document).on('click', '#card-modal-close', function(e) {
        e.preventDefault();
        $cardModal.removeClass('active');
    });

    $cardModal.on('click', function(e) {
        if (e.target === this) $cardModal.removeClass('active');
    });

    $(document).on('dblclick', '#card-description-rendered', function() {
        enableDescriptionEditing($(this).closest('#card-detail-form'));
    });

    $(document).on('input', '#card-detail-form .card-title-input, #card-detail-form #card-content-area', function() {
        showCardSave($(this).closest('#card-detail-form'));
    });

    $(document).on('click', '#card-edit-cancel', function(e) {
        e.preventDefault();
        applyRemoteCardState($(this).closest('#card-detail-form'));
    });

    $(document).on('keydown', '#card-label-input', function(e) {
        var cardId = $(this).closest('#card-detail-form').data('id');
        if (e.key === 'Enter') {
            e.preventDefault();
            addLabelFromInput(cardId);
            return;
        }
        if (e.key === 'ArrowDown' && visibleLabelMatches.length) {
            e.preventDefault();
            activeLabelIndex = Math.min(activeLabelIndex + 1, visibleLabelMatches.length - 1);
            updateLabelSuggestionSelection();
            return;
        }
        if (e.key === 'ArrowUp' && visibleLabelMatches.length) {
            e.preventDefault();
            activeLabelIndex = Math.max(activeLabelIndex - 1, 0);
            updateLabelSuggestionSelection();
            return;
        }
        if (e.key === 'Escape') {
            $(this).val('');
            hideLabelSuggestions();
        }
    });

    $(document).on('input', '#card-label-input', function() {
        refreshLabelSuggestions();
    });

    $(document).on('focus', '#card-label-input', function() {
        refreshLabelSuggestions();
    });

    $(document).on('click', '.label-suggestion', function() {
        var cardId = $(this).closest('#card-detail-form').data('id');
        var idx = Number($(this).data('index'));
        if (!isNaN(idx) && visibleLabelMatches[idx]) {
            persistAddLabel(cardId, visibleLabelMatches[idx]);
        }
    });

    $(document).on('click', function(e) {
        if (!$(e.target).closest('#card-label-picker').length) {
            hideLabelSuggestions();
        }
    });

    $(document).on('click', '.label-remove-btn', function(e) {
        e.preventDefault();
        var $pill = $(this).closest('.label-pill');
        var cardId = $(this).closest('#card-detail-form').data('id');
        var labelId = $pill.data('label-id');
        post('/boards/' + board + '/cards/' + cardId + '/labels/' + labelId + '/delete', {})
            .then(function(r) { return r.json(); })
            .then(function() {
                $pill.remove();
                updateSelectedLabelsState();
                refreshLabelSuggestions();
            });
    });

    // Save card
    $(document).on('submit', '#card-detail-form', function(e) {
        e.preventDefault();
        var $form = $(this);
        var cardId = $form.data('id');
        post('/boards/' + board + '/cards/' + cardId, new URLSearchParams(new FormData(this)))
            .then(function(r) { return r.json(); })
            .then(function(data) {
                if (data.html) {
                    $('.kanban-card[data-id="' + cardId + '"]').replaceWith(data.html);
                } else {
                    $('.kanban-card[data-id="' + cardId + '"] .card-title').text(data.title);
                }
                $form.find('.card-title-input').val(data.title || '');
                $form.find('#card-content-area').val(data.content || '');
                setRemoteCardState($form, data.title || '', data.content || '', data.content_rendered || '');
                syncCardDescriptionDisplay($form, data.content, data.content_rendered);
                hideCardSave($form);
                hideCardEditWarning($form);
            });
    });

    // Move card (column select change)
    $(document).on('change', '#card-move-select', function() {
        var $form = $(this).closest('#card-detail-form');
        var cardId = $form.data('id');
        var colId = $(this).val();
        post('/boards/' + board + '/cards/' + cardId + '/move', { column_id: colId, position: 0 })
            .then(function() {});
    });

    // Archive card
    $(document).on('click', '#card-archive-btn', function() {
        var cardId = $(this).data('card-id');
        var $card = $('.board-column:not(.archived-column) .kanban-card[data-id="' + cardId + '"]').first();
        post('/boards/' + board + '/cards/' + cardId + '/archive', {})
            .then(function() {
                var $archivedCards = $('#archived-cards');
                if ($card.length && $archivedCards.length) {
                    var $archivedCard = $card.clone();
                    $archivedCard.removeAttr('draggable');
                    $archivedCards.prepend($archivedCard);
                }
                $card.remove();
                updateArchivedColumnState();
                $cardModal.removeClass('active');
            });
    });

    $(document).on('click', '#card-unarchive-btn', function() {
        var cardId = $(this).data('card-id');
        post('/boards/' + board + '/cards/' + cardId + '/unarchive', {})
            .then(function(r) { return r.json(); })
            .then(function(data) {
                if (data.column_id && data.html) {
                    $('.col-cards[data-column-id="' + data.column_id + '"]').first().append(data.html);
                }
                $('#archived-cards .kanban-card[data-id="' + cardId + '"]').remove();
                updateArchivedColumnState();
                $cardModal.removeClass('active');
            });
    });

    $(document).on('click', '#card-delete-btn', function() {
        var cardId = $(this).data('card-id');
        if (!window.confirm('Delete this archived card permanently?')) return;
        post('/boards/' + board + '/cards/' + cardId + '/delete', {})
            .then(function(r) { return r.json(); })
            .then(function() {
                $('#archived-cards .kanban-card[data-id="' + cardId + '"]').remove();
                updateArchivedColumnState();
                $cardModal.removeClass('active');
            });
    });
});
