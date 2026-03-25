$(function() {
    var board = $('#board-outer').data('slug');
    if (!board) return;
    var DEFAULT_LABEL_COLOR = '#888888';
    var collapsedColumnsKey = 'giverny:collapsed-columns:' + board;
    var activeLabelIndex = -1;
    var visibleLabelMatches = [];

    function post(url, data) {
        var body = data instanceof FormData ? data : new URLSearchParams(data);
        return fetch(url, { method: 'POST', body: body });
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

    function applyBoardEvent(evt) {
        switch (evt.type) {
        case 'card.title.modified':
            applyCardTitleModified(evt.payload);
            break;
        case 'card.description.modified':
            applyCardDescriptionModified(evt.payload);
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

    function pad(n) { return n < 10 ? '0' + n : n; }

    function renderEventLog() {
        if (wsEvents.length === 0) {
            $wsEventLog.html('<div class="ws-event-empty">no events yet</div>');
            return;
        }
        var html = '';
        for (var i = wsEvents.length - 1; i >= 0; i--) {
            var ev = wsEvents[i];
            html += '<div class="ws-event-entry">' +
                '<span class="ws-event-type">' + ev.type + '</span>' +
                '<span class="ws-event-time">' + ev.time + '</span>' +
                '</div>';
        }
        $wsEventLog.html(html);
    }

    function flashIndicator() {
        var $i = $wsIndicator;
        $i[0].className = 'fa-solid fa-plug-circle-bolt ws-indicator flash';
        setTimeout(function() {
            $i[0].className = 'fa-solid fa-plug ws-indicator connected';
        }, 400);
    }

    function isShown($el) {
        var el = $el && $el[0];
        if (!el) return false;
        return el.style.display !== 'none' && el.offsetParent !== null;
    }

    $('#ws-icon-row').on('click', function() {
        var open = isShown($wsEventLog);
        $wsEventLog[0].style.display = open ? 'none' : 'block';
        if (!open) renderEventLog();
    });

    // Close if clicking outside the status box.
    $(document).on('click', function(e) {
        if (!$('#ws-status-box')[0].contains(e.target)) {
            $wsEventLog.hide();
        }
    });

    var proto = location.protocol === 'https:' ? 'wss' : 'ws';
    var ws = new WebSocket(proto + '://' + location.host + '/boards/' + board + '/ws');
    applyCollapsedColumns();

    ws.onopen = function() {
        $wsIndicator.removeClass('error').addClass('connected');
    };
    ws.onclose = function() {
        $wsIndicator.removeClass('connected').addClass('error');
    };
    ws.onerror = function() {
        $wsIndicator.removeClass('connected').addClass('error');
    };
    ws.onmessage = function(e) {
        var evt = JSON.parse(e.data);
        applyBoardEvent(evt);
        var now = new Date();
        var time = pad(now.getHours()) + ':' + pad(now.getMinutes()) + ':' + pad(now.getSeconds());
        wsEvents.push({ type: evt.type || 'message', time: time });
        if (wsEvents.length > MAX_EVENTS) wsEvents.shift();
        if (isShown($wsEventLog)) renderEventLog();
        flashIndicator();
    };

    // --- Add column toggle ---
    $('#add-column-btn').on('click', function() {
        $(this).hide();
        $('.add-column-form').show().find('input[name=name]')[0].focus();
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
            .then(function(html) {
                $form.closest('.board-column').find('.col-cards').append(html);
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

    $(document).on('click', '.column-edit-action', function(e) {
        e.stopPropagation();
        var formId = $(this).data('form');
        var form = document.getElementById(formId);
        if (!form) return;
        var currentName = $(this).data('name') || '';
        var nextName = window.prompt('column name', currentName);
        if (nextName === null) return;
        nextName = nextName.trim();
        if (!nextName) return;
        form.elements.name.value = nextName;
        form.submit();
    });

    $(document).on('click', function(e) {
        if (!$(e.target).closest('.col-actions').length) {
            $('.col-actions').removeClass('open');
        }
    });

    // --- Card detail modal ---
    var $cardModal = $('#card-modal');

    $(document).on('click', '.kanban-card', function() {
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
            .then(function() { location.reload(); });
    });

    // Archive card
    $(document).on('click', '#card-archive-btn', function() {
        var cardId = $(this).data('card-id');
        post('/boards/' + board + '/cards/' + cardId + '/archive', {})
            .then(function() {
                $('.kanban-card[data-id="' + cardId + '"]').remove();
                $cardModal.removeClass('active');
            });
    });
});
