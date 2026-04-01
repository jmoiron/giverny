$(function() {
    var board = $('#board-outer').data('slug');
    var cardPermalinksEnabled = !!board || $('.card-list-wrap').length > 0;
    var DEFAULT_LABEL_COLOR = '#888888';

    function cardDetail($scope) {
        if ($scope && $scope.length) {
            if ($scope.is('.card-detail')) return $scope.first();
            var $detail = $scope.closest('.card-detail');
            if ($detail.length) return $detail.first();
        }
        return $('.card-detail').first();
    }

    function primaryCardForm($scope) {
        var $detail = cardDetail($scope);
        return $detail.find('#card-detail-form').first();
    }

    function cardID($scope) {
        var $detail = cardDetail($scope);
        if ($detail.length) return Number($detail.data('card-id')) || 0;
        var $form = $scope && $scope.length && $scope.is('#card-detail-form') ? $scope : ($scope && $scope.closest ? $scope.closest('#card-detail-form') : $());
        return Number($form && $form.data('id')) || 0;
    }

    function cardBoard($scope) {
        var $detail = cardDetail($scope);
        return ($detail && $detail.data('board-slug')) || ($scope && $scope.data && $scope.data('board-slug')) || board || '';
    }
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
    var checklistDragState = {
        itemId: null,
        draggedEl: null,
        placeholder: null
    };
    var attachmentDragDepth = 0;
    var attachmentUploadInFlight = false;
    var activeAttachmentRenameId = 0;

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

    function attachmentIconClass(mimeType) {
        return String(mimeType || '').toLowerCase().indexOf('image/') === 0 ? 'fa-solid fa-image' : 'fa-solid fa-file';
    }

    function applyCardColorButtonState($scope) {
        var $btn = $scope.find('.quick-color-btn').first();
        if (!$btn.length) return;
        var color = String($btn.attr('data-card-color') || $scope.find('.card-detail').attr('data-card-color') || '').trim();
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
        cardDetail($form).find('#card-edit-warning').removeClass('is-hidden');
    }

    function hideCardEditWarning($form) {
        cardDetail($form).find('#card-edit-warning').addClass('is-hidden');
    }

    function enableDescriptionEditing($form) {
        var $detail = cardDetail($form);
        if (!$form.length || !$detail.length || String($detail.attr('data-can-edit')) !== '1') return;
        if ($detail.hasClass('editing-description')) return;
        $detail.addClass('editing-description');
        showCardSave($form);
        var textarea = $form.find('#card-content-area')[0];
        if (textarea) {
            textarea.focus();
            textarea.setSelectionRange(textarea.value.length, textarea.value.length);
        }
    }

    function syncCardDescriptionDisplay($form, content, renderedHTML) {
        var $detail = cardDetail($form);
        var $rendered = $detail.find('#card-description-rendered');
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
        cardDetail($form).find('.card-title-input').val(remote.title);
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

    function getKnownUsers() {
        var users = [];
        $('#known-users-data .known-user-option').each(function() {
            users.push({
                id: Number($(this).data('user-id')),
                username: String($(this).data('username') || '').trim(),
                email: String($(this).data('email') || '').trim()
            });
        });
        return users;
    }

    function formatCardDateDisplay(value) {
        if (!value) return 'unset';
        var parts = String(value).split('-');
        if (parts.length !== 3) return value;
        var d = new Date(Number(parts[0]), Number(parts[1]) - 1, Number(parts[2]));
        return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
    }

    function userTimeZone() {
        return (document.body && document.body.dataset && document.body.dataset.userTimezone) ? document.body.dataset.userTimezone : 'UTC';
    }

    function zonedDateParts(date) {
        var parts = new Intl.DateTimeFormat('en-US', {
            timeZone: userTimeZone(),
            year: 'numeric',
            month: '2-digit',
            day: '2-digit'
        }).formatToParts(date || new Date());
        var out = {};
        parts.forEach(function(part) {
            if (part.type === 'year' || part.type === 'month' || part.type === 'day') out[part.type] = part.value;
        });
        return out;
    }

    function formatTimestampDisplay(value) {
        if (!value) return '';
        var date = new Date(value);
        if (isNaN(date.getTime())) return value;
        var parts = new Intl.DateTimeFormat('en-US', {
            timeZone: userTimeZone(),
            hour: '2-digit',
            minute: '2-digit',
            hourCycle: 'h23',
            month: 'short',
            day: 'numeric'
        }).formatToParts(date);
        var got = {};
        parts.forEach(function(part) {
            if (part.type === 'hour' || part.type === 'minute' || part.type === 'month' || part.type === 'day') got[part.type] = part.value;
        });
        return [got.hour + ':' + got.minute, got.month, got.day].join(' ');
    }

    function updateCardAccent($scope, color) {
        var normalized = String(color || '').trim();
        var $modalInner = $scope.find('.card-modal-inner').first();
        if ($modalInner.length) {
            $modalInner.toggleClass('has-card-color', !!normalized);
            if (normalized) {
                $modalInner.attr('style', '--card-color: ' + normalized);
            } else {
                $modalInner.removeAttr('style');
            }
        }
        var $detail = $scope.find('.card-detail').first();
        if ($detail.length) {
            $detail.attr('data-card-color', normalized);
            $detail.toggleClass('has-card-color', !!normalized);
        }
        var cardId = $scope.find('#card-detail-form').data('id');
        var $boardCard = $('.kanban-card[data-id="' + cardId + '"]').first();
        if ($boardCard.length) {
            $boardCard.attr('data-card-color', normalized);
            $boardCard.toggleClass('has-card-color', !!normalized);
            if (normalized) {
                $boardCard.attr('style', '--card-color: ' + normalized);
            } else {
                $boardCard.removeAttr('style');
            }
        }
    }

    function renderAssigneePills(assignees) {
        var html = '';
        (assignees || []).forEach(function(assignee) {
            html += '<span class="card-assignee-pill" data-user-id="' + assignee.id + '">';
            if (assignee.profile_image_uri) {
                html += '<img class="card-assignee-avatar" src="' + $('<div>').text(assignee.profile_image_uri).html() + '" alt="">';
            }
            html += '<span class="card-assignee-name">' + $('<div>').text(assignee.username).html() + '</span>' +
                '<a href="#" class="card-assignee-remove" aria-label="remove ' + $('<div>').text(assignee.username).html() + '">' +
                '<i class="fa-solid fa-xmark"></i></a></span>';
        });
        var $assignees = $('#card-assignees');
        $assignees.html(html);
        $assignees.toggleClass('is-empty', !assignees || assignees.length === 0);
    }

    function applyCardMetaResponse($form, data) {
        if (!data) return;
        var cardId = $form.data('id');
        if (data.html) {
            $('.kanban-card[data-id="' + cardId + '"]').replaceWith(data.html);
        }
        if (typeof data.color !== 'undefined') {
            var $colorBtn = $form.closest('.card-detail').find('.quick-color-btn').first();
            $colorBtn.attr('data-card-color', data.color || '');
            $form.find('input[name=color]').val(data.color || '');
            $('#card-color-custom').val(data.color || '#40BFFA');
            updateCardAccent($cardModal, data.color || '');
            applyCardColorButtonState($cardModal);
        }
        if (typeof data.assignees !== 'undefined') {
            renderAssigneePills(data.assignees || []);
        }
        if (typeof data.start_date_value !== 'undefined') {
            $('#card-start-date-input').val(data.start_date_value || '');
            $('#card-start-date-display').text(formatCardDateDisplay(data.start_date_value || ''));
        }
        if (typeof data.due_date_value !== 'undefined') {
            $('#card-due-date-input').val(data.due_date_value || '');
            $('#card-due-date-display').text(formatCardDateDisplay(data.due_date_value || ''));
        }
        if (typeof data.updated_at_display !== 'undefined') {
            $('#card-updated-at-display').text(data.updated_at_display || '');
        }
        if (typeof data.updated_at_value !== 'undefined') {
            $('#card-updated-at-display').text(formatTimestampDisplay(data.updated_at_value || ''));
        }
        if (typeof data.checklist !== 'undefined') {
            renderBoardChecklistProgress(cardId, data.checklist || { total_count: 0, completed_count: 0, percent_complete: 0 });
            renderChecklist(data.checklist || { items: [], completed_count: 0, total_count: 0, percent_complete: 0 });
        }
        if (typeof data.attachments !== 'undefined') {
            renderAttachments(data.attachments || []);
        }
    }

    function applyCardDateUpdated(payload) {
        if (!payload || !payload.card_id) return;
        var $form = getActiveCardForm(payload.card_id);
        if (!$form.length) return;
        $('#card-start-date-input').val(payload.start_date_value || '');
        $('#card-start-date-display').text(formatCardDateDisplay(payload.start_date_value || ''));
        $('#card-due-date-input').val(payload.due_date_value || '');
        $('#card-due-date-display').text(formatCardDateDisplay(payload.due_date_value || ''));
        $('#card-updated-at-display').text(formatTimestampDisplay(payload.updated_at_value || ''));
    }

    function applyCardAttachmentsUpdated(payload) {
        if (!payload || !payload.card_id) return;
        var $form = getActiveCardForm(payload.card_id);
        if (!$form.length) return;
        renderAttachments(payload.attachments || []);
        if (typeof payload.updated_at_value !== 'undefined') {
            $('#card-updated-at-display').text(formatTimestampDisplay(payload.updated_at_value || ''));
        } else if (typeof payload.updated_at_display !== 'undefined') {
            $('#card-updated-at-display').text(payload.updated_at_display || '');
        }
    }

    function renderBoardChecklistProgress(cardId, payload) {
        if (!cardId) return;
        var $boardCard = $('.kanban-card[data-id="' + cardId + '"]').first();
        if (!$boardCard.length) return;
        var total = payload && payload.total_count || 0;
        var done = payload && payload.completed_count || 0;
        var pct = payload && payload.percent_complete || 0;
        var $slot = $boardCard.find('.kanban-card-checklist-progress-slot').first();
        if (!$slot.length) {
            $slot = $('<div class="kanban-card-checklist-progress-slot is-empty"></div>');
            var $labels = $boardCard.find('.card-labels').first();
            if ($labels.length) {
                $labels.before($slot);
            } else {
                $boardCard.append($slot);
            }
        }
        var $existing = $slot.find('.kanban-card-checklist-progress').first();
        if (!total) {
            $slot.addClass('is-empty');
            $existing.attr('hidden', 'hidden');
            return;
        }
        if (!$existing.length) {
            $existing = $(
                '<div class="kanban-card-checklist-progress">' +
                    '<span class="kanban-card-checklist-copy"></span>' +
                    '<span class="kanban-card-checklist-bar">' +
                        '<span class="kanban-card-checklist-fill"></span>' +
                    '</span>' +
                '</div>'
            );
            $slot.append($existing);
        }
        $slot.removeClass('is-empty');
        $existing.removeAttr('hidden');
        $existing.find('.kanban-card-checklist-copy').text(done + '/' + total + ' ' + pct + '%');
        $existing.find('.kanban-card-checklist-fill').css('width', String(pct) + '%');
        if ($boardCard.attr('data-card-color')) {
            $existing.addClass('has-card-color').attr('style', '--card-color: ' + $boardCard.attr('data-card-color'));
        } else {
            $existing.removeClass('has-card-color').removeAttr('style');
        }
    }

    function applyCardChecklistUpdated(payload) {
        if (!payload || !payload.card_id) return;
        renderBoardChecklistProgress(payload.card_id, payload);
        var $form = getActiveCardForm(payload.card_id);
        if (!$form.length) return;
        renderChecklist(payload);
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

    function checklistPercent(completed, total) {
        if (!total) return 0;
        return Math.round((completed / total) * 100);
    }

    function createChecklistItem(item) {
        var $item = $('<label class="checklist-item" draggable="true"></label>');
        $item.attr('data-item-id', item.id);
        $item.toggleClass('is-done', !!item.done);
        $item.append($('<input type="checkbox" class="checklist-item-toggle">').prop('checked', !!item.done));
        $item.append($('<span class="checklist-item-text"></span>').text(item.text || ''));
        $item.append(
            $('<a href="#" class="checklist-item-delete inline-remove-link" aria-label="delete checklist item"></a>')
                .append('<i class="fa-solid fa-xmark"></i>')
        );
        return $item;
    }

    function renderChecklist(payload) {
        var items = payload && payload.items ? payload.items : [];
        var exists = !!(payload && payload.exists);
        var $section = $('#card-checklist-section');
        var $items = $('#card-checklist-items');
        if (!$items.length || !$section.length) return;
        $section.toggleClass('is-empty', !exists);
        $items.empty();
        items.forEach(function(item) {
            $items.append(createChecklistItem(item));
        });
        $items.toggleClass('is-empty', items.length === 0);
        $('#checklist-progress-text').text((payload && payload.completed_count || 0) + '/' + (payload && payload.total_count || 0) + ' ' + (payload && payload.percent_complete || 0) + '%');
        $('#checklist-progress-fill').css('width', String(payload && payload.percent_complete || 0) + '%');
        updateChecklistCompletedVisibility();
    }

    function updateChecklistCompletedVisibility() {
        var $section = $('#card-checklist-section');
        if (!$section.length) return;
        var hidden = $section.hasClass('hide-completed');
        $('#checklist-toggle-completed-btn').text(hidden ? 'show completed' : 'hide completed');
    }

    function createAttachmentRow(attachment) {
        var $row = $('<div class="card-attachment-row"></div>');
        $row.attr('data-attachment-id', attachment.id);
        $row.attr('data-attachment-url', attachment.filepath || '');
        var iconClass = attachment.icon_class || attachmentIconClass(attachment.mime_type);
        $row.append(
            $('<div class="card-attachment-main"></div>')
                .append(
                    $('<a class="card-attachment-link" target="_blank" rel="noopener noreferrer"></a>')
                        .attr('href', attachment.filepath || '#')
                        .append($('<i></i>').attr('class', iconClass))
                        .append($('<span></span>').text(attachment.filename || 'attachment'))
                )
                .append(
                    $('<div class="card-attachment-rename-inline" hidden></div>')
                        .append($('<i></i>').attr('class', iconClass))
                        .append($('<input type="text" class="card-attachment-rename-input" aria-label="rename attachment">').val(attachment.filename || 'attachment'))
                )
        );
        $row.append(
            $('<div class="card-attachment-actions"></div>')
                .append($('<a href="#" class="card-attachment-rename" aria-label="rename attachment"><i class="fa-solid fa-pen-to-square"></i></a>'))
                .append($('<a href="#" class="card-attachment-copy" aria-label="copy attachment link"><i class="fa-regular fa-copy"></i></a>'))
                .append($('<a href="#" class="card-attachment-delete inline-remove-link" aria-label="delete attachment"><i class="fa-solid fa-xmark"></i></a>'))
        );
        return $row;
    }

    function renderAttachments(attachments) {
        var list = attachments || [];
        var $section = $('#card-attachments-section');
        var $list = $('#card-attachments-list');
        if (!$section.length || !$list.length) return;
        $list.empty();
        activeAttachmentRenameId = 0;
        list.forEach(function(attachment) {
            $list.append(createAttachmentRow(attachment));
        });
        $section.toggleClass('is-empty', list.length === 0);
        $('#card-attachment-rename-actions').addClass('is-hidden');
    }

    function resetAttachmentOverlay() {
        attachmentDragDepth = 0;
        attachmentUploadInFlight = false;
        var $overlay = $('#card-upload-overlay');
        $overlay.removeClass('is-complete').addClass('is-hidden');
        $('#card-upload-overlay-copy').text('drop files to attach them to this card');
        $('#card-upload-overlay-name').text('');
        $('#card-upload-progress').addClass('is-hidden');
        $('#card-upload-progress-copy').text('0%');
        $('#card-upload-progress-fill').css('width', '0%');
    }

    function showAttachmentOverlay(message, name) {
        $('#card-upload-overlay-copy').text(message || 'drop files to attach them to this card');
        $('#card-upload-overlay-name').text(name || '');
        $('#card-upload-progress').addClass('is-hidden');
        $('#card-upload-overlay').removeClass('is-hidden is-complete');
    }

    function updateAttachmentOverlayProgress(message, name, percent) {
        var pct = Math.max(0, Math.min(100, Number(percent || 0)));
        $('#card-upload-overlay-copy').text(message || 'uploading attachment');
        $('#card-upload-overlay-name').text(name || '');
        $('#card-upload-progress').removeClass('is-hidden');
        $('#card-upload-progress-copy').text(String(pct) + '%');
        $('#card-upload-progress-fill').css('width', String(pct) + '%');
        $('#card-upload-overlay').removeClass('is-hidden is-complete');
    }

    function completeAttachmentOverlay() {
        var $overlay = $('#card-upload-overlay');
        $('#card-upload-progress-copy').text('100%');
        $('#card-upload-progress-fill').css('width', '100%');
        $overlay.removeClass('is-hidden').addClass('is-complete');
        setTimeout(resetAttachmentOverlay, 180);
    }

    function beginAttachmentRename($row) {
        cancelAttachmentRename($('.card-attachment-row.is-renaming').first());
        var $inline = $row.find('.card-attachment-rename-inline').first();
        var $input = $inline.find('.card-attachment-rename-input').first();
        if (!$inline.length || !$input.length) return;
        activeAttachmentRenameId = Number($row.data('attachment-id'));
        $row.addClass('is-renaming');
        $inline.prop('hidden', false);
        $('#card-attachment-rename-actions').removeClass('is-hidden');
        setTimeout(function() {
            $input.trigger('focus');
            var input = $input[0];
            if (input && input.select) input.select();
        }, 0);
    }

    function cancelAttachmentRename($row) {
        if (!$row || !$row.length) {
            $('#card-attachment-rename-actions').addClass('is-hidden');
            activeAttachmentRenameId = 0;
            return;
        }
        var $inline = $row.find('.card-attachment-rename-inline').first();
        var current = $row.find('.card-attachment-link span').first().text();
        $inline.prop('hidden', true);
        $inline.find('.card-attachment-rename-input').val(current);
        $row.removeClass('is-renaming');
        $('#card-attachment-rename-actions').addClass('is-hidden');
        activeAttachmentRenameId = 0;
    }

    function saveAttachmentRename() {
        var $row = $('.card-attachment-row.is-renaming').first();
        if (!$row.length) return;
        var $form = $('#card-detail-form');
        var cardId = $form.data('id');
        var filename = $row.find('.card-attachment-rename-input').val().trim();
        if (!filename) return;
        post('/boards/' + cardBoard($form) + '/cards/' + cardId + '/attachments/' + $row.data('attachment-id') + '/rename', { filename: filename })
            .then(function(r) { return r.json(); })
            .then(function(data) {
                applyCardMetaResponse($form, data);
            });
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

    function applyLabelColorChanged(payload) {
        if (!payload || !payload.label_id) return;
        var color = payload.color || DEFAULT_LABEL_COLOR;
        var textClass = payload.text_class || labelTextClass(color);
        $('.label-pill[data-label-id="' + payload.label_id + '"]').each(function() {
            $(this)
                .attr('style', '--label-color: ' + color)
                .removeClass('fg-light fg-dark')
                .addClass(textClass);
        });
        $('#known-labels-data .known-label-option[data-label-id="' + payload.label_id + '"]')
            .attr('data-color', color)
            .attr('data-text-class', textClass);
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

    function checklistItemIDs() {
        return $('#card-checklist-items .checklist-item').map(function() {
            return Number($(this).data('item-id'));
        }).get();
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

    function isNoOpChecklistPlacement(beforeEl, checklistItemsEl) {
        if (!checklistDragState.draggedEl) return false;
        var currentIndex = childIndex(checklistDragState.draggedEl, '.checklist-item');
        var targetIndex = beforeEl ? childIndex(beforeEl, '.checklist-item') : checklistItemsEl.querySelectorAll('.checklist-item').length;
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
            cardDetail($form).find('.card-title-input').val(payload.title || '');
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

    function applyCardColorChanged(payload) {
        if (!payload || !payload.card_id) return;
        var cardId = payload.card_id;
        var color = payload.color || '';
        var $boardCard = $('.kanban-card[data-id="' + cardId + '"]').first();
        if ($boardCard.length) {
            $boardCard.attr('data-card-color', color);
            $boardCard.toggleClass('has-card-color', !!color);
            if (color) {
                $boardCard.attr('style', '--card-color: ' + color);
            } else {
                $boardCard.removeAttr('style');
            }
        }
        var $form = getActiveCardForm(cardId);
        if (!$form.length) return;
        $form.find('input[name=color]').val(color);
        $('#card-color-custom').val(color || '#40BFFA');
        var $colorBtn = $form.closest('.card-detail').find('.quick-color-btn').first();
        $colorBtn.attr('data-card-color', color);
        updateCardAccent($cardModal, color);
        applyCardColorButtonState($cardModal);
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
            closeCardModal(hashCardID() === Number(payload.card_id));
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
        case 'card.checklist.updated':
            applyCardChecklistUpdated(evt.payload);
            break;
        case 'card.color.changed':
            applyCardColorChanged(evt.payload);
            break;
        case 'card.date.updated':
            applyCardDateUpdated(evt.payload);
            break;
        case 'card.attachments.updated':
            applyCardAttachmentsUpdated(evt.payload);
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
        case 'label.color.changed':
            applyLabelColorChanged(evt.payload);
            break;
        case 'card.comment.added':
            applyCardCommentAdded(evt.payload);
            break;
        case 'card.comment.edited':
            applyCardCommentEdited(evt.payload);
            break;
        case 'card.comment.deleted':
            applyCardCommentDeleted(evt.payload);
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
        var $form = $('#card-detail-form');
        return post('/boards/' + cardBoard($form) + '/cards/' + cardId + '/labels', { label_id: label.id })
            .then(function(r) { return r.json(); })
            .then(function() {
                attachLabelToEditor(label);
                refreshLabelSuggestions();
            });
    }

    function renderAssigneeSuggestions(query) {
        var users = getKnownUsers();
        var q = String(query || '').trim().toLowerCase();
        var assigned = {};
        $('#card-assignees .card-assignee-pill').each(function() {
            assigned[String($(this).data('user-id'))] = true;
        });
        // TODO: switch this to server-backed user autocomplete if installations grow beyond a small user list.
        if (q) {
            users = users.filter(function(user) {
                return user.username.toLowerCase().indexOf(q) >= 0 || user.email.toLowerCase().indexOf(q) >= 0;
            });
        }
        users = users.filter(function(user) {
            return !assigned[String(user.id)];
        });
        users.sort(function(a, b) { return a.username.localeCompare(b.username); });
        var html = '';
        users.forEach(function(user) {
            html += '<button type="button" class="assign-suggestion" data-user-id="' + user.id + '" data-username="' + $('<div>').text(user.username).html() + '">' +
                '<span class="assign-suggestion-name">' + $('<div>').text(user.username).html() + '</span>' +
                '<span class="assign-suggestion-meta">' + $('<div>').text(user.email).html() + '</span>' +
                '</button>';
        });
        $('#card-assignee-suggestions').html(html);
    }

    function resolvedQuickDate(kind) {
        var parts = zonedDateParts(new Date());
        var now = new Date(Date.UTC(Number(parts.year), Number(parts.month) - 1, Number(parts.day)));
        function ymd(date) {
            return [
                date.getUTCFullYear(),
                String(date.getUTCMonth() + 1).padStart(2, '0'),
                String(date.getUTCDate()).padStart(2, '0')
            ].join('-');
        }
        if (kind === 'today') {
            return ymd(now);
        }
        if (kind === 'tomorrow') {
            now.setUTCDate(now.getUTCDate() + 1);
            return ymd(now);
        }
        if (kind === 'weekend') {
            var day = now.getUTCDay();
            var delta = (6 - day + 7) % 7;
            if (delta === 0) delta = 1;
            now.setUTCDate(now.getUTCDate() + delta);
            return ymd(now);
        }
        return '';
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
        var box = $('#ws-status-box')[0];
        if (box && !box.contains(e.target)) {
            $wsEventLog.hide();
        }
    });

    if (board) {
        applyCollapsedColumns();
        connectWS();
    }

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

    $(document).on('click', '#archived-delete-all-btn', function(e) {
        e.preventDefault();
        e.stopPropagation();
        window.showConfirmModal('Delete all archived cards permanently?', function() {
            post('/boards/' + board + '/cards/archived/delete', {})
                .then(function(r) { return r.json(); })
                .then(function() {
                    $('#archived-cards .kanban-card').remove();
                    updateArchivedColumnState();
                    $('.col-actions').removeClass('open');
                });
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

    bindOverlayClose($columnModal, function() {
        $columnModal.removeClass('active');
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
    var cardModalRequestID = 0;

    function bindOverlayClose($overlay, onClose) {
        if (!$overlay || !$overlay.length) return;
        $overlay.on('mousedown', function(e) {
            this._overlayMouseDown = (e.target === this);
        });
        $overlay.on('click', function(e) {
            var startedOnOverlay = !!this._overlayMouseDown;
            this._overlayMouseDown = false;
            if (startedOnOverlay && e.target === this) onClose();
        });
    }

    function cardHash(cardId) {
        return '#card-' + Number(cardId);
    }

    function hashCardID(hash) {
        var match = String(hash || window.location.hash || '').match(/^#card-(\d+)$/);
        return match ? Number(match[1]) : 0;
    }

    function openCardModal(cardId, syncHash, boardSlug) {
        cardId = Number(cardId || 0);
        if (!cardId) return;
        if (syncHash !== false && cardPermalinksEnabled) {
            var nextHash = cardHash(cardId);
            if (window.location.hash !== nextHash) {
                window.location.hash = nextHash;
                return;
            }
        }
        var requestID = ++cardModalRequestID;
        var url = boardSlug ? ('/boards/' + boardSlug + '/cards/' + cardId) : ('/cards/' + cardId + '/');
        fetch(url)
            .then(function(r) { return r.text(); })
            .then(function(html) {
                if (requestID !== cardModalRequestID) return;
                $cardModal.find('.card-modal-inner').html(html);
                var $form = $cardModal.find('#card-detail-form');
                updateCardAccent($cardModal, $form.closest('.card-detail').attr('data-card-color') || '');
                applyCardColorButtonState($cardModal);
                updateSelectedLabelsState();
                setRemoteCardState(
                    $form,
                    cardDetail($form).find('.card-title-input').val(),
                    $form.find('#card-content-area').val(),
                    cardDetail($form).find('#card-description-rendered').html()
                );
                hideCardEditWarning($form);
                updateChecklistCompletedVisibility();
                resetAttachmentOverlay();
                $cardModal.addClass('active');
            });
    }

    function closeCardModal(syncHash) {
        cardModalRequestID++;
        resetAttachmentOverlay();
        $cardModal.removeClass('active');
        if (syncHash !== false && cardPermalinksEnabled && hashCardID()) {
            history.replaceState(null, '', window.location.pathname + window.location.search);
        }
    }

    window.closeActiveCardModal = function() {
        if (!$cardModal.hasClass('active')) return false;
        closeCardModal(true);
        return true;
    };

    function syncCardModalToHash() {
        var cardId = hashCardID();
        if (cardId) {
            openCardModal(cardId, false, board);
            return;
        }
        closeCardModal(false);
    }

    function uploadCardAttachment(file) {
        var $form = $('#card-detail-form');
        if (!$form.length || !file) return;
        var cardId = $form.data('id');
        var formData = new FormData();
        formData.append('file', file);
        attachmentUploadInFlight = true;
        updateAttachmentOverlayProgress('uploading attachment', file.name || '', 0);
        return new Promise(function(resolve, reject) {
            var xhr = new XMLHttpRequest();
            xhr.open('POST', '/boards/' + cardBoard($form) + '/cards/' + cardId + '/attachments');
            xhr.responseType = 'json';
            xhr.upload.onprogress = function(ev) {
                if (!ev.lengthComputable) return;
                updateAttachmentOverlayProgress('uploading attachment', file.name || '', Math.round((ev.loaded / ev.total) * 100));
            };
            xhr.onload = function() {
                attachmentUploadInFlight = false;
                if (xhr.status >= 200 && xhr.status < 300) {
                    var data = xhr.response;
                    if (!data && xhr.responseText) data = JSON.parse(xhr.responseText);
                    applyCardMetaResponse($form, data || {});
                    $('#card-attachment-input').val('');
                    completeAttachmentOverlay();
                    resolve(data);
                    return;
                }
                resetAttachmentOverlay();
                reject(new Error('attachment upload failed'));
            };
            xhr.onerror = function() {
                attachmentUploadInFlight = false;
                resetAttachmentOverlay();
                reject(new Error('attachment upload failed'));
            };
            xhr.send(formData);
        });
    }

    function uploadCardAttachments(files) {
        var queue = Array.prototype.slice.call(files || []).filter(Boolean);
        if (!queue.length) return Promise.resolve();
        return queue.reduce(function(promise, file) {
            return promise.then(function() {
                return uploadCardAttachment(file);
            });
        }, Promise.resolve());
    }

    if (cardPermalinksEnabled) {
        window.addEventListener('hashchange', syncCardModalToHash);
    }

    document.addEventListener('dragover', function(ev) {
        var dt = ev.dataTransfer;
        if (!$cardModal.hasClass('active') || !dt || !dt.types) return;
        if (Array.prototype.indexOf.call(dt.types, 'Files') === -1) return;
        ev.preventDefault();
        dt.dropEffect = 'copy';
    }, true);

    document.addEventListener('dragenter', function(ev) {
        var dt = ev.dataTransfer;
        if (!$cardModal.hasClass('active') || !dt || !dt.types) return;
        if (Array.prototype.indexOf.call(dt.types, 'Files') === -1) return;
        attachmentDragDepth += 1;
        if (!attachmentUploadInFlight) {
            showAttachmentOverlay('drop files to attach them to this card');
        }
    }, true);

    document.addEventListener('dragleave', function(ev) {
        var dt = ev.dataTransfer;
        if (!$cardModal.hasClass('active') || !dt || !dt.types) return;
        if (Array.prototype.indexOf.call(dt.types, 'Files') === -1) return;
        attachmentDragDepth = Math.max(0, attachmentDragDepth - 1);
        if (attachmentDragDepth === 0 && !attachmentUploadInFlight) {
            resetAttachmentOverlay();
        }
    }, true);

    document.addEventListener('drop', function(ev) {
        var dt = ev.dataTransfer;
        if (!$cardModal.hasClass('active') || !dt || !dt.files || !dt.files.length) return;
        ev.preventDefault();
        attachmentDragDepth = 0;
        uploadCardAttachments(dt.files);
    }, true);

    if (board) {
        $(document).on('click', '.kanban-card', function() {
            if (shouldSuppressCardClick()) return;
            openCardModal($(this).data('id'), true);
        });
    }

    $(document).on('click', '.card-list-title-link', function(e) {
        e.preventDefault();
        openCardModal($(this).data('card-id'), true);
    });

    if (cardPermalinksEnabled) {
        syncCardModalToHash();
    }

    document.addEventListener('dragstart', function(ev) {
        var checklistItem = ev.target.closest('.checklist-item');
        if (checklistItem) {
            checklistDragState.itemId = Number($(checklistItem).data('item-id'));
            checklistDragState.draggedEl = checklistItem;
            checklistDragState.placeholder = null;
            setDragGhost(ev, checklistItem);
            startDragMode('card');
            $(checklistItem).addClass('dragging-source');
            return;
        }

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
        var checklistItemsEl = ev.target.closest('#card-checklist-items');
        if (checklistItemsEl && checklistDragState.itemId) {
            ev.preventDefault();
            var checklistItems = checklistItemsEl.querySelectorAll('.checklist-item:not(.dragging-source)');
            var beforeChecklistItem = findInsertBeforeByAxis(checklistItems, ev.clientY, 'y');
            if (isNoOpChecklistPlacement(beforeChecklistItem, checklistItemsEl)) {
                if (checklistDragState.placeholder && checklistDragState.placeholder.parentNode) {
                    checklistDragState.placeholder.parentNode.removeChild(checklistDragState.placeholder);
                }
                return;
            }
            if (!checklistDragState.placeholder) {
                checklistDragState.placeholder = createDragPlaceholder($(checklistDragState.draggedEl), 'card-drop-placeholder checklist-drop-placeholder')[0];
            }
            if (beforeChecklistItem) {
                beforeChecklistItem.parentNode.insertBefore(checklistDragState.placeholder, beforeChecklistItem);
            } else {
                checklistItemsEl.appendChild(checklistDragState.placeholder);
            }
            return;
        }

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
        var checklistItemsEl = ev.target.closest('#card-checklist-items');
        if (checklistItemsEl && checklistDragState.itemId && checklistDragState.draggedEl) {
            ev.preventDefault();
            if (checklistDragState.placeholder && checklistDragState.placeholder.parentNode) {
                checklistDragState.placeholder.parentNode.insertBefore(checklistDragState.draggedEl, checklistDragState.placeholder);
            }
            var itemIDs = checklistItemIDs();
            if (itemIDs.length) {
                var $clForm = $('#card-detail-form');
                postJSON('/boards/' + cardBoard($clForm) + '/cards/' + $clForm.data('id') + '/checklist/reorder', itemIDs);
            }
            $(checklistDragState.draggedEl).removeClass('dragging-source');
            if (checklistDragState.placeholder) $(checklistDragState.placeholder).remove();
            checklistDragState.itemId = null;
            checklistDragState.draggedEl = null;
            checklistDragState.placeholder = null;
            stopDragMode();
            return;
        }

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
        if (ev.target.closest('.checklist-item')) {
            $(ev.target).removeClass('dragging-source');
            if (checklistDragState.placeholder) $(checklistDragState.placeholder).remove();
            checklistDragState.itemId = null;
            checklistDragState.draggedEl = null;
            checklistDragState.placeholder = null;
            stopDragMode();
            return;
        }
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
        closeCardModal(true);
    });

    bindOverlayClose($cardModal, function() {
        closeCardModal(true);
    });

    $(document).on('dblclick', '#card-description-rendered', function() {
        enableDescriptionEditing(primaryCardForm($(this)));
    });

    $(document).on('input', '.card-title-input, #card-detail-form #card-content-area', function() {
        showCardSave(primaryCardForm($(this)));
    });

    $(document).on('click', '#card-edit-cancel', function(e) {
        e.preventDefault();
        applyRemoteCardState($(this).closest('#card-detail-form'));
    });

    $(document).on('keydown', '#card-label-input', function(e) {
        var cardId = cardID($(this));
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
        var cardId = cardID($(this));
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

    $(document).on('click', '.quick-panel-trigger', function(e) {
        e.preventDefault();
        var $trigger = $(this);
        var panelId = $(this).data('panel');
        if (!panelId) return;
        var $panel = $('#' + panelId);
        $('.quick-control-panel').not($panel).hide();
        $panel.css({
            top: ($trigger.position().top + $trigger.outerHeight() + 6) + 'px'
        });
        $panel.toggle();
        if (panelId === 'card-assign-panel' && isShown($panel)) {
            renderAssigneeSuggestions($('#card-assignee-input').val());
            $('#card-assignee-input').trigger('focus');
        }
    });

    $(document).on('click', '#card-checklist-btn', function() {
        var input = document.getElementById('checklist-new-item-input');
        var section = document.getElementById('card-checklist-section');
        if (section) {
            section.classList.remove('is-empty');
            section.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
        }
        if (input) input.focus();
    });

    $(document).on('click', '#card-attach-btn', function() {
        $('#card-attachment-input').trigger('click');
    });

    $(document).on('click', function(e) {
        if (!$(e.target).closest('.quick-panel-trigger, .quick-control-panel').length) {
            $('.quick-control-panel').hide();
        }
    });

    $(document).on('input focus', '#card-assignee-input', function() {
        renderAssigneeSuggestions($(this).val());
    });

    $(document).on('click', '.assign-suggestion', function() {
        var $detail = cardDetail($(this));
        var $form = primaryCardForm($detail);
        var cardId = cardID($detail);
        var userId = $(this).data('user-id');
        post('/boards/' + cardBoard($detail) + '/cards/' + cardId + '/assign', {
            assignee_id: userId
        })
            .then(function(r) { return r.json(); })
            .then(function(data) {
                applyCardMetaResponse($form, data);
                renderAssigneeSuggestions($('#card-assignee-input').val());
                $('.quick-control-panel').hide();
            });
    });

    $(document).on('click', '.card-assignee-remove', function(e) {
        e.preventDefault();
        var $detail = cardDetail($(this));
        var $form = primaryCardForm($detail);
        var cardId = cardID($detail);
        var userId = $(this).closest('.card-assignee-pill').data('user-id');
        post('/boards/' + cardBoard($detail) + '/cards/' + cardId + '/assignees/' + userId + '/delete', {})
            .then(function(r) { return r.json(); })
            .then(function(data) {
                applyCardMetaResponse($form, data);
                renderAssigneeSuggestions($('#card-assignee-input').val());
            });
    });

    $(document).on('click', '.color-swatch-btn[data-color]', function() {
        var $detail = cardDetail($(this));
        var $form = primaryCardForm($detail);
        var cardId = cardID($detail);
        var color = $(this).attr('data-color') || '';
        post('/boards/' + cardBoard($detail) + '/cards/' + cardId + '/color', { color: color })
            .then(function(r) { return r.json(); })
            .then(function(data) {
                applyCardMetaResponse($form, data);
            });
    });

    $(document).on('click', '#card-color-more-btn', function() {
        $('#card-color-custom').trigger('click');
    });

    $(document).on('input change', '#card-color-custom', function() {
        var $detail = cardDetail($(this));
        var $form = primaryCardForm($detail);
        var cardId = cardID($detail);
        post('/boards/' + cardBoard($detail) + '/cards/' + cardId + '/color', { color: $(this).val() })
            .then(function(r) { return r.json(); })
            .then(function(data) {
                applyCardMetaResponse($form, data);
            });
    });

    $(document).on('click', '#card-done-btn', function() {
        var $detail = cardDetail($(this));
        var cardId = cardID($detail);
        post('/boards/' + cardBoard($detail) + '/cards/' + cardId + '/done', {})
            .then(function(r) { return r.json(); })
            .then(function(payload) {
                if (payload && payload.to_column_id) {
                    applyCardMoved(payload);
                    $('#card-move-select').val(String(payload.to_column_id));
                }
                var $btn = $('#card-done-btn');
                $btn.addClass('is-active done-btn');
                $btn.attr('data-done', '1');
                $btn.find('span').first().text('done');
            });
    });

    $(document).on('click', '#card-subscribe-btn', function() {
        var $btn = $(this);
        var $detail = cardDetail($btn);
        var cardId = cardID($detail);
        var next = $btn.attr('data-subscribed') === '1' ? '0' : '1';
        post('/boards/' + cardBoard($detail) + '/cards/' + cardId + '/subscribe', { subscribed: next })
            .then(function(r) { return r.json(); })
            .then(function(data) {
                var subscribed = !!data.subscribed;
                $btn.attr('data-subscribed', subscribed ? '1' : '0');
                $btn.toggleClass('is-active subscribed-btn', subscribed);
                $btn.find('span').first().text(subscribed ? 'subscribed' : 'subscribe');
            });
    });

    $(document).on('click', '.date-quick-btn', function() {
        var target = $(this).data('target');
        var value = resolvedQuickDate($(this).data('date'));
        if (target === 'due') {
            $('#card-due-date-input').val(value);
            $('#card-due-date-input').trigger('change');
        } else {
            $('#card-start-date-input').val(value);
            $('#card-start-date-input').trigger('change');
        }
    });

    $(document).on('change', '#card-due-date-input', function() {
        var $detail = cardDetail($(this));
        var $form = primaryCardForm($detail);
        var cardId = cardID($detail);
        post('/boards/' + cardBoard($detail) + '/cards/' + cardId + '/due-date', { date: $(this).val() })
            .then(function(r) { return r.json(); })
            .then(function(data) {
                applyCardMetaResponse($form, data);
            });
    });

    $(document).on('change', '#card-start-date-input', function() {
        var $detail = cardDetail($(this));
        var $form = primaryCardForm($detail);
        var cardId = cardID($detail);
        post('/boards/' + cardBoard($detail) + '/cards/' + cardId + '/start-date', { date: $(this).val() })
            .then(function(r) { return r.json(); })
            .then(function(data) {
                applyCardMetaResponse($form, data);
            });
    });

    $(document).on('click', '.label-remove-btn', function(e) {
        e.preventDefault();
        var $pill = $(this).closest('.label-pill');
        var $detail = cardDetail($(this));
        var $form = primaryCardForm($detail);
        if (!$form.length) {
            if ($pill.closest('#card-list-selected-labels').length) {
                $pill.remove();
                $('#card-list-selected-labels').toggleClass('is-empty', $('#card-list-selected-labels .label-pill').length === 0);
            }
            return;
        }
        var cardId = cardID($detail);
        var labelId = $pill.data('label-id');
        post('/boards/' + cardBoard($detail) + '/cards/' + cardId + '/labels/' + labelId + '/delete', {})
            .then(function(r) { return r.json(); })
            .then(function() {
                $pill.remove();
                updateSelectedLabelsState();
                refreshLabelSuggestions();
            });
    });

    $(document).on('submit', '#card-checklist-add-form', function(e) {
        e.preventDefault();
        var $detail = cardDetail($(this));
        var $form = primaryCardForm($detail);
        var text = $(this).find('#checklist-new-item-input').val().trim();
        var cardId = cardID($detail);
        if (!text) return;
        post('/boards/' + cardBoard($detail) + '/cards/' + cardId + '/checklist/items', { text: text })
            .then(function(r) { return r.json(); })
            .then(function(data) {
                applyCardMetaResponse($form, data);
                $('#checklist-new-item-input').val('').focus();
            });
    });

    $(document).on('change', '.checklist-item-toggle', function() {
        var $item = $(this).closest('.checklist-item');
        var $detail = cardDetail($(this));
        var $form = primaryCardForm($detail);
        var cardId = cardID($detail);
        post('/boards/' + cardBoard($detail) + '/cards/' + cardId + '/checklist/items/' + $item.data('item-id') + '/done', {
            done: this.checked ? '1' : '0'
        })
            .then(function(r) { return r.json(); })
            .then(function(data) {
                applyCardMetaResponse($form, data);
            });
    });

    $(document).on('click', '.checklist-item-delete', function(e) {
        e.preventDefault();
        e.stopPropagation();
        var $item = $(this).closest('.checklist-item');
        var $detail = cardDetail($(this));
        var $form = primaryCardForm($detail);
        var cardId = cardID($detail);
        post('/boards/' + cardBoard($detail) + '/cards/' + cardId + '/checklist/items/' + $item.data('item-id') + '/delete', {})
            .then(function(r) { return r.json(); })
            .then(function(data) {
                applyCardMetaResponse($form, data);
            });
    });

    $(document).on('change', '#card-attachment-input', function() {
        if (this.files && this.files.length) uploadCardAttachments(this.files);
    });

    $(document).on('click', '.card-attachment-copy', function(e) {
        e.preventDefault();
        var url = $(this).closest('.card-attachment-row').find('.card-attachment-link')[0];
        url = url ? url.href : '';
        if (!url) return;
        navigator.clipboard.writeText(url).then(() => {
            var $icon = $(this).find('i').first();
            $icon.removeClass('fa-copy').addClass('fa-check');
            setTimeout(function() {
                $icon.removeClass('fa-check').addClass('fa-copy');
            }, 1200);
        });
    });

    $(document).on('click', '.card-attachment-rename', function(e) {
        e.preventDefault();
        beginAttachmentRename($(this).closest('.card-attachment-row'));
    });

    $(document).on('click', '.card-attachment-rename-cancel', function(e) {
        e.preventDefault();
        cancelAttachmentRename($('.card-attachment-row.is-renaming').first());
    });

    $(document).on('keydown', '.card-attachment-rename-input', function(e) {
        if (e.key === 'Escape') {
            e.preventDefault();
            cancelAttachmentRename($(this).closest('.card-attachment-row'));
            return;
        }
        if (e.key === 'Enter') {
            e.preventDefault();
            saveAttachmentRename();
        }
    });

    $(document).on('click', '.card-attachment-rename-save', function(e) {
        e.preventDefault();
        saveAttachmentRename();
    });

    $(document).on('click', '.card-attachment-delete', function(e) {
        e.preventDefault();
        var $row = $(this).closest('.card-attachment-row');
        var $detail = cardDetail($(this));
        var $form = primaryCardForm($detail);
        var cardId = cardID($detail);
        post('/boards/' + cardBoard($detail) + '/cards/' + cardId + '/attachments/' + $row.data('attachment-id') + '/delete', {})
            .then(function(r) { return r.json(); })
            .then(function(data) {
                applyCardMetaResponse($form, data);
            });
    });

    $(document).on('click', '#checklist-toggle-completed-btn', function() {
        $('#card-checklist-section').toggleClass('hide-completed');
        updateChecklistCompletedVisibility();
    });

    $(document).on('click', '#checklist-delete-btn', function() {
        var $detail = cardDetail($(this));
        var $form = primaryCardForm($detail);
        var cardId = cardID($detail);
        window.showConfirmModal('Delete this checklist?', function() {
            post('/boards/' + cardBoard($detail) + '/cards/' + cardId + '/checklist/delete', {})
                .then(function(r) { return r.json(); })
                .then(function(data) {
                    applyCardMetaResponse($form, data);
                });
        });
    });

    // --- Comments ---

    function buildCommentElement(comment, canEdit) {
        var $c = $('<div class="card-comment"></div>').attr('data-comment-id', comment.comment_id);
        var $header = $('<div class="card-comment-header"></div>');
        var $author = $('<div class="card-comment-author"></div>');
        if (comment.author_image) {
            $author.append($('<img class="card-comment-avatar" alt="">').attr('src', comment.author_image));
        }
        $author.append($('<span class="card-comment-username"></span>').text(comment.author_username));
        $author.append($('<span class="card-comment-date"></span>').text(comment.created_at_display || ''));
        $header.append($author);
        if (canEdit) {
            var $menuWrap = $('<div class="card-comment-menu-wrap"></div>');
            var $trigger = $('<a href="#" class="card-comment-menu-trigger" aria-label="comment options"><i class="fa-solid fa-ellipsis-vertical"></i></a>');
            var $menu = $('<div class="card-comment-menu" hidden></div>');
            $menu.append($('<button type="button" class="card-comment-edit-start">edit</button>'));
            $menu.append($('<button type="button" class="card-comment-delete-btn">delete</button>'));
            $menuWrap.append($trigger).append($menu);
            $header.append($menuWrap);
        }
        $c.append($header);
        $c.append($('<div class="card-comment-body"></div>').html(comment.body_rendered));
        if (canEdit) {
            var $editWrap = $('<form class="card-comment-edit-wrap card-comment-edit-form" hidden></form>');
            $editWrap.append($('<textarea class="card-comment-edit-area"></textarea>').val(comment.body));
            var $editActions = $('<div class="add-card-actions btn-row"></div>');
            $editActions.append($('<a href="#" class="btn-cancel card-comment-edit-cancel">cancel</a>'));
            $editActions.append($('<button type="submit" class="card-comment-edit-save">save</button>'));
            $editWrap.append($editActions);
            $c.append($editWrap);
        }
        return $c;
    }

    function cardCanEdit() {
        return cardDetail().data('can-edit') == 1;
    }

    function openCommentForm() {
        $('#card-comments-section').removeClass('is-empty');
        $('#card-add-comment-inline-btn').prop('hidden', true);
        $('#card-comment-form').removeAttr('hidden');
        $('#card-comment-textarea')[0].focus();
    }

    function closeCommentForm() {
        $('#card-comment-form').attr('hidden', true);
        var hasComments = $('#card-comments-list .card-comment').length > 0;
        if (hasComments) {
            $('#card-add-comment-inline-btn').prop('hidden', false);
        } else {
            $('#card-comments-section').addClass('is-empty');
        }
    }

    function submitNewComment($commentForm) {
        var $detail = cardDetail($commentForm);
        var cardId = cardID($detail);
        var body = $commentForm.find('#card-comment-textarea').val().trim();
        if (!body) return;
        post('/boards/' + cardBoard($detail) + '/cards/' + cardId + '/comments', { body: body })
            .then(function(r) { return r.json(); })
            .then(function(data) {
                if (!data.ok) return;
                $commentForm.find('#card-comment-textarea').val('');
                if (!$('#card-comments-list .card-comment[data-comment-id="' + data.comment.comment_id + '"]').length) {
                    $('#card-comments-list').append(buildCommentElement(data.comment, cardCanEdit()));
                }
                $('#card-comments-section').removeClass('is-empty');
                closeCommentForm();
            });
    }

    function submitEditedComment($editForm) {
        var $detail = cardDetail($editForm);
        var cardId = cardID($detail);
        var $comment = $editForm.closest('.card-comment');
        var commentId = $comment.data('comment-id');
        var body = $editForm.find('.card-comment-edit-area').val().trim();
        if (!body) return;
        post('/boards/' + cardBoard($detail) + '/cards/' + cardId + '/comments/' + commentId + '/edit', { body: body })
            .then(function(r) { return r.json(); })
            .then(function(data) {
                if (!data.ok) return;
                $comment.find('.card-comment-body').html(data.comment.body_rendered).show();
                $comment.find('.card-comment-edit-area').val(data.comment.body);
                $comment.find('.card-comment-edit-wrap').attr('hidden', true);
            });
    }

    // Show comment section + form when comment button clicked (quick control or inline)
    $(document).on('click', '#card-comment-btn, #card-add-comment-inline-btn', function(e) {
        e.preventDefault();
        openCommentForm();
    });

    // Cancel adding comment
    $(document).on('click', '#card-comment-cancel', function(e) {
        e.preventDefault();
        closeCommentForm();
    });

    // Submit new comment
    $(document).on('submit', '#card-comment-form', function(e) {
        e.preventDefault();
        submitNewComment($(this));
    });

    // Toggle comment menu
    $(document).on('click', '.card-comment-menu-trigger', function(e) {
        e.preventDefault();
        e.stopPropagation();
        var $menu = $(this).siblings('.card-comment-menu');
        var isHidden = $menu.prop('hidden');
        $('.card-comment-menu').prop('hidden', true);
        $menu.prop('hidden', !isHidden);
    });

    $(document).on('click', function(e) {
        if (!$(e.target).closest('.card-comment-menu-wrap').length) {
            $('.card-comment-menu').prop('hidden', true);
        }
    });

    // Start editing a comment
    $(document).on('click', '.card-comment-edit-start', function(e) {
        e.preventDefault();
        var $comment = $(this).closest('.card-comment');
        $comment.find('.card-comment-menu').prop('hidden', true);
        $comment.find('.card-comment-body').hide();
        var $editWrap = $comment.find('.card-comment-edit-wrap');
        $editWrap.removeAttr('hidden');
        $editWrap.find('.card-comment-edit-area')[0].focus();
    });

    // Cancel editing a comment
    $(document).on('click', '.card-comment-edit-cancel', function(e) {
        e.preventDefault();
        var $comment = $(this).closest('.card-comment');
        $comment.find('.card-comment-edit-wrap').attr('hidden', true);
        $comment.find('.card-comment-body').show();
    });

    // Save edited comment
    $(document).on('submit', '.card-comment-edit-form', function(e) {
        e.preventDefault();
        submitEditedComment($(this));
    });

    // Delete comment
    $(document).on('click', '.card-comment-delete-btn', function() {
        var $comment = $(this).closest('.card-comment');
        var $detail = cardDetail($(this));
        var cardId = cardID($detail);
        var commentId = $comment.data('comment-id');
        $comment.find('.card-comment-menu').prop('hidden', true);
        window.showConfirmModal('Delete this comment?', function() {
            post('/boards/' + cardBoard($detail) + '/cards/' + cardId + '/comments/' + commentId + '/delete', {})
                .then(function(r) { return r.json(); })
                .then(function(data) {
                    if (!data.ok) return;
                    $comment.remove();
                    if ($('#card-comments-list .card-comment').length === 0) {
                        $('#card-add-comment-inline-btn').prop('hidden', true);
                        $('#card-comments-section').addClass('is-empty');
                    }
                });
        });
    });

    // WS: comment added by another client
    function applyCardCommentAdded(payload) {
        var $detail = cardDetail();
        if (!$detail.length || Number($detail.data('card-id')) !== Number(payload.card_id)) return;
        if ($('#card-comments-list .card-comment[data-comment-id="' + payload.comment_id + '"]').length) return;
        $('#card-comments-list').append(buildCommentElement(payload, cardCanEdit()));
        $('#card-comments-section').removeClass('is-empty');
        if ($('#card-comment-form').prop('hidden') !== false) {
            $('#card-add-comment-inline-btn').prop('hidden', false);
        }
    }

    function applyCardCommentEdited(payload) {
        var $comment = $('#card-comments-list .card-comment[data-comment-id="' + payload.comment_id + '"]');
        if (!$comment.length) return;
        $comment.find('.card-comment-body').html(payload.body_rendered).show();
        $comment.find('.card-comment-edit-area').val(payload.body);
        $comment.find('.card-comment-edit-wrap').attr('hidden', true);
    }

    function applyCardCommentDeleted(payload) {
        $('#card-comments-list .card-comment[data-comment-id="' + payload.comment_id + '"]').remove();
        if ($('#card-comments-list .card-comment').length === 0) {
            $('#card-add-comment-inline-btn').prop('hidden', true);
            $('#card-comments-section').addClass('is-empty');
        }
    }

    // Save card
    $(document).on('submit', '#card-detail-form', function(e) {
        e.preventDefault();
        var $form = $(this);
        var cardId = $form.data('id');
        post('/boards/' + cardBoard($form) + '/cards/' + cardId, new URLSearchParams(new FormData(this)))
            .then(function(r) { return r.json(); })
            .then(function(data) {
                if (data.html) {
                    $('.kanban-card[data-id="' + cardId + '"]').replaceWith(data.html);
                } else {
                    $('.kanban-card[data-id="' + cardId + '"] .card-title').text(data.title);
                }
                cardDetail($form).find('.card-title-input').val(data.title || '');
                $form.find('#card-content-area').val(data.content || '');
                setRemoteCardState($form, data.title || '', data.content || '', data.content_rendered || '');
                syncCardDescriptionDisplay($form, data.content, data.content_rendered);
                hideCardSave($form);
                hideCardEditWarning($form);
            });
    });

    // Move card (column select change)
    $(document).on('change', '#card-move-select', function() {
        var $detail = cardDetail($(this));
        var cardId = cardID($detail);
        var colId = $(this).val();
        post('/boards/' + cardBoard($detail) + '/cards/' + cardId + '/move', { column_id: colId, position: 0 })
            .then(function() {});
    });

    // Archive card
    $(document).on('click', '#card-archive-btn', function() {
        var cardId = $(this).data('card-id');
        var $form = $('#card-detail-form');
        var $card = $('.board-column:not(.archived-column) .kanban-card[data-id="' + cardId + '"]').first();
        post('/boards/' + cardBoard($form) + '/cards/' + cardId + '/archive', {})
            .then(function() {
                var $archivedCards = $('#archived-cards');
                if ($card.length && $archivedCards.length) {
                    var $archivedCard = $card.clone();
                    $archivedCard.removeAttr('draggable');
                    $archivedCards.prepend($archivedCard);
                }
                $card.remove();
                updateArchivedColumnState();
                closeCardModal(hashCardID() === Number(cardId));
            });
    });

    $(document).on('click', '#card-unarchive-btn', function() {
        var cardId = $(this).data('card-id');
        var $form = $('#card-detail-form');
        post('/boards/' + cardBoard($form) + '/cards/' + cardId + '/unarchive', {})
            .then(function(r) { return r.json(); })
            .then(function(data) {
                if (data.column_id && data.html) {
                    $('.col-cards[data-column-id="' + data.column_id + '"]').first().append(data.html);
                }
                $('#archived-cards .kanban-card[data-id="' + cardId + '"]').remove();
                updateArchivedColumnState();
                closeCardModal(hashCardID() === Number(cardId));
            });
    });

    $(document).on('click', '#card-delete-btn', function() {
        var cardId = $(this).data('card-id');
        var $form = $('#card-detail-form');
        window.showConfirmModal('Delete this archived card permanently?', function() {
            post('/boards/' + cardBoard($form) + '/cards/' + cardId + '/delete', {})
                .then(function(r) { return r.json(); })
                .then(function() {
                    $('#archived-cards .kanban-card[data-id="' + cardId + '"]').remove();
                    updateArchivedColumnState();
                    closeCardModal(hashCardID() === Number(cardId));
                });
        });
    });

    // --- Card list view ---
    var $cardListTable = $('#card-list-table');
    if ($cardListTable.length) {
        var CARD_LIST_COLS_KEY = 'giverny:card-list-cols';
        var DEFAULT_COLS = ['title', 'board', 'labels', 'created', 'updated', 'due'];
        var ALL_OPTIONAL = ['board', 'labels', 'created', 'updated', 'due', 'column', 'done', 'start', 'subscribed', 'comments', 'checklist'];
        var filterLabelMatches = [];
        var filterLabelIndex = -1;
        var filterUserIndex = -1;
        var filterColumnMatches = [];
        var filterColumnIndex = -1;

        function getVisibleCols() {
            // Check URL param first (set when loading a saved view).
            var params = new URLSearchParams(window.location.search);
            var colsParam = params.get('cols');
            if (colsParam) {
                return colsParam.split(',').filter(Boolean);
            }
            var stored = localStorage.getItem(CARD_LIST_COLS_KEY);
            if (stored) {
                return stored.split(',').filter(Boolean);
            }
            return DEFAULT_COLS.slice();
        }

        function applyColVisibility(cols) {
            var colSet = {};
            cols.forEach(function(c) { colSet[c] = true; });
            ALL_OPTIONAL.forEach(function(col) {
                $cardListTable.toggleClass('show-col-' + col, !!colSet[col]);
                var $cb = $('#cols-panel input[data-col="' + col + '"]');
                if (!$cb.prop('disabled')) {
                    $cb.prop('checked', !!colSet[col]);
                }
            });
        }

        function saveColVisibility() {
            var cols = ['title', 'board'];
            $('#cols-panel input[type=checkbox]').each(function() {
                if (!$(this).prop('disabled') && $(this).prop('checked')) {
                    cols.push($(this).data('col'));
                }
            });
            localStorage.setItem(CARD_LIST_COLS_KEY, cols.join(','));
            return cols;
        }

        // Initialize column visibility.
        applyColVisibility(getVisibleCols());

        function getCardListKnownLabels() {
            var labels = [];
            $('#card-list-known-labels .known-label-option').each(function() {
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

        function getCardListKnownUsers() {
            var users = [];
            $('#card-list-known-users .known-user-option').each(function() {
                users.push({
                    id: Number($(this).data('user-id')),
                    username: String($(this).data('username') || '').trim(),
                    email: String($(this).data('email') || '').trim()
                });
            });
            return users;
        }

        function getCardListKnownColumns() {
            var cols = [];
            $('#card-list-known-columns .known-column-option').each(function() {
                cols.push(String($(this).data('name') || '').trim());
            });
            return cols;
        }

        function selectedFilterLabelExists(labelId) {
            return $('#card-list-selected-labels .label-pill[data-label-id="' + labelId + '"]').length > 0;
        }

        function createFilterLabelPill(label) {
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
            $pill.append($('<input type="hidden" name="filter_label">').val(label.id));
            return $pill;
        }

        function hideCardListLabelSuggestions() {
            $('#card-list-label-suggestions').hide().empty();
            filterLabelMatches = [];
            filterLabelIndex = -1;
        }

        function renderCardListLabelSuggestions(query) {
            var labels = getCardListKnownLabels();
            var selectedIDs = {};
            $('#card-list-selected-labels .label-pill').each(function() {
                selectedIDs[String($(this).data('label-id'))] = true;
            });
            var trimmed = String(query || '').trim();
            var matches = labels.filter(function(label) {
                if (selectedIDs[String(label.id)]) return false;
                if (!trimmed) return true;
                var titleScore = fuzzyFieldScore(label.title, trimmed);
                var descScore = fuzzyFieldScore(label.description || '', trimmed);
                return titleScore >= 0 || descScore >= 0;
            }).map(function(label) {
                var titleScore = trimmed ? fuzzyFieldScore(label.title, trimmed) : 0;
                var descScore = trimmed ? fuzzyFieldScore(label.description || '', trimmed) : 0;
                var score = titleScore >= 0 ? titleScore : (descScore >= 0 ? descScore + 50 : 9999);
                return { label: label, score: score };
            }).sort(function(a, b) {
                if (a.score !== b.score) return a.score - b.score;
                return a.label.title.localeCompare(b.label.title);
            }).slice(0, trimmed ? 8 : 5).map(function(entry) {
                return entry.label;
            });
            filterLabelMatches = matches;
            filterLabelIndex = -1;
            var $list = $('#card-list-label-suggestions').empty();
            if (!matches.length) {
                $list.hide();
                return;
            }
            matches.forEach(function(label) {
                var $item = $('<button type="button" class="label-suggestion"></button>');
                $item.attr('data-label-id', label.id);
                $item.append(
                    $('<span class="label-pill"></span>')
                        .addClass(label.textClass || labelTextClass(label.color || DEFAULT_LABEL_COLOR))
                        .attr('style', '--label-color: ' + (label.color || DEFAULT_LABEL_COLOR))
                        .text(label.title)
                );
                if (label.description) {
                    $item.append($('<div class="assign-suggestion-meta"></div>').text(label.description));
                }
                $list.append($item);
            });
            $list.show();
        }

        function attachFilterLabel(label) {
            if (!label || !label.id || selectedFilterLabelExists(label.id)) return;
            $('#card-list-selected-labels').append(createFilterLabelPill(label)).removeClass('is-empty');
            $('#card-list-label-input').val('').trigger('focus');
            hideCardListLabelSuggestions();
        }

        function hideFilterColumnSuggestions() {
            $('#filter-col-suggestions').hide().empty();
            filterColumnMatches = [];
            filterColumnIndex = -1;
        }

        function renderFilterColumnSuggestions(query) {
            var trimmed = String(query || '').trim();
            var matches = getCardListKnownColumns().filter(function(name) {
                if (!trimmed) return true;
                return fuzzyFieldScore(name, trimmed) >= 0;
            }).sort(function(a, b) {
                var as = trimmed ? fuzzyFieldScore(a, trimmed) : 0;
                var bs = trimmed ? fuzzyFieldScore(b, trimmed) : 0;
                if (as !== bs) return as - bs;
                return a.localeCompare(b);
            }).slice(0, trimmed ? 8 : 5);
            filterColumnMatches = matches;
            filterColumnIndex = -1;
            var $list = $('#filter-col-suggestions').empty();
            if (!matches.length) {
                $list.hide();
                return;
            }
            matches.forEach(function(name) {
                var $item = $('<button type="button" class="label-suggestion"></button>');
                $item.attr('data-name', name);
                $item.text(name);
                $list.append($item);
            });
            $list.show();
        }

        function renderFilterUserSuggestions($picker, query) {
            $('.filter-user-suggestions').not($picker.find('.filter-user-suggestions')).hide();
            var users = getCardListKnownUsers();
            var trimmed = String(query || '').trim();
            var matches = users.filter(function(user) {
                if (!trimmed) return true;
                return fuzzyFieldScore(user.username, trimmed) >= 0 || fuzzyFieldScore(user.email, trimmed) >= 0;
            }).map(function(user) {
                var userScore = trimmed ? fuzzyFieldScore(user.username, trimmed) : 0;
                var emailScore = trimmed ? fuzzyFieldScore(user.email, trimmed) : 0;
                var score = userScore >= 0 ? userScore : (emailScore >= 0 ? emailScore + 50 : 9999);
                return { user: user, score: score };
            }).sort(function(a, b) {
                if (a.score !== b.score) return a.score - b.score;
                return a.user.username.localeCompare(b.user.username);
            }).slice(0, trimmed ? 8 : 5).map(function(entry) {
                return entry.user;
            });
            var $list = $picker.find('.filter-user-suggestions').empty();
            $list.append(
                $('<button type="button" class="assign-suggestion"></button>')
                    .attr('data-user-id', '')
                    .append($('<span class="assign-suggestion-name"></span>').text('anyone'))
            );
            if ($picker.attr('data-target') === 'filter_user') {
                $list.append(
                    $('<button type="button" class="assign-suggestion"></button>')
                        .attr('data-user-id', '__unassigned__')
                        .append($('<span class="assign-suggestion-name"></span>').text('unassigned'))
                );
            } else if ($picker.attr('data-target') === 'filter_subscribed') {
                $list.append(
                    $('<button type="button" class="assign-suggestion"></button>')
                        .attr('data-user-id', '__unsubscribed__')
                        .append($('<span class="assign-suggestion-name"></span>').text('no subscribers'))
                );
            }
            matches.forEach(function(user, idx) {
                var $item = $('<button type="button" class="assign-suggestion"></button>');
                if (idx === filterUserIndex - 1 && filterUserIndex > 0) $item.addClass('active');
                $item.attr('data-user-id', user.id);
                $item.append($('<span class="assign-suggestion-name"></span>').text(user.username));
                if (user.email) $item.append($('<span class="assign-suggestion-meta"></span>').text(user.email));
                $list.append($item);
            });
            $list.show();
            filterUserIndex = -1;
        }

        function syncFilterUserInput($picker) {
            var currentId = Number($picker.find('input[type=hidden]').val() || 0);
            var currentRaw = String($picker.find('input[type=hidden]').val() || '');
            if (currentRaw === '__unassigned__') {
                $picker.find('.filter-user-input').val('unassigned');
                return;
            }
            if (currentRaw === '__unsubscribed__') {
                $picker.find('.filter-user-input').val('no subscribers');
                return;
            }
            var user = null;
            getCardListKnownUsers().forEach(function(candidate) {
                if (candidate.id === currentId) user = candidate;
            });
            $picker.find('.filter-user-input').val(user ? user.username : '');
        }

        // Column panel toggle.
        var $colsToggle = $('#cols-toggle');
        var $colsPanel = $('#cols-panel');
        $colsToggle.on('click', function(e) {
            e.preventDefault();
            $colsPanel.toggleClass('hidden');
            $colsToggle.toggleClass('active');
        });

        // Column checkbox changes.
        $colsPanel.on('change', 'input[type=checkbox]', function() {
            saveColVisibility();
            var col = $(this).data('col');
            $cardListTable.toggleClass('show-col-' + col, $(this).prop('checked'));
        });

        // Filter panel toggle.
        var $filterToggle = $('#filter-toggle');
        var $filterPanel = $('#filter-panel');
        $filterToggle.on('click', function(e) {
            e.preventDefault();
            $filterPanel.toggleClass('hidden');
            $filterToggle.toggleClass('active');
        });

        syncFilterUserInput($('.filter-user-picker[data-target="filter_user"]'));
        syncFilterUserInput($('.filter-user-picker[data-target="filter_subscribed"]'));

        $('#filter-label-match-toggle').on('change', function() {
            $('#filter-label-match-input').val(this.checked ? 'all' : 'any');
            $('#filter-label-match-copy').text('match: ' + (this.checked ? 'all' : 'any'));
        });

        $('#card-list-label-input').on('focus input', function() {
            renderCardListLabelSuggestions($(this).val());
        }).on('keydown', function(e) {
            if (!filterLabelMatches.length) return;
            if (e.key === 'ArrowDown') {
                e.preventDefault();
                filterLabelIndex = (filterLabelIndex + 1) % filterLabelMatches.length;
                $('#card-list-label-suggestions .label-suggestion').removeClass('active').eq(filterLabelIndex).addClass('active');
            } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                filterLabelIndex = (filterLabelIndex <= 0 ? filterLabelMatches.length - 1 : filterLabelIndex - 1);
                $('#card-list-label-suggestions .label-suggestion').removeClass('active').eq(filterLabelIndex).addClass('active');
            } else if (e.key === 'Enter') {
                e.preventDefault();
                if (filterLabelIndex >= 0 && filterLabelMatches[filterLabelIndex]) {
                    attachFilterLabel(filterLabelMatches[filterLabelIndex]);
                }
            } else if (e.key === 'Escape') {
                hideCardListLabelSuggestions();
            }
        });

        $(document).on('click', '#card-list-label-suggestions .label-suggestion', function() {
            var labelId = Number($(this).data('label-id'));
            var label = null;
            getCardListKnownLabels().forEach(function(candidate) {
                if (candidate.id === labelId) label = candidate;
            });
            attachFilterLabel(label);
        });

        $('#filter-col-input').on('focus input', function() {
            var value = $(this).val();
            $('#filter-col-value').val(value);
            renderFilterColumnSuggestions(value);
        }).on('keydown', function(e) {
            if (!filterColumnMatches.length) return;
            if (e.key === 'ArrowDown') {
                e.preventDefault();
                filterColumnIndex = (filterColumnIndex + 1) % filterColumnMatches.length;
                $('#filter-col-suggestions .label-suggestion').removeClass('active').eq(filterColumnIndex).addClass('active');
            } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                filterColumnIndex = (filterColumnIndex <= 0 ? filterColumnMatches.length - 1 : filterColumnIndex - 1);
                $('#filter-col-suggestions .label-suggestion').removeClass('active').eq(filterColumnIndex).addClass('active');
            } else if (e.key === 'Enter') {
                e.preventDefault();
                if (filterColumnIndex >= 0 && filterColumnMatches[filterColumnIndex]) {
                    $('#filter-col-input').val(filterColumnMatches[filterColumnIndex]);
                    $('#filter-col-value').val(filterColumnMatches[filterColumnIndex]);
                    hideFilterColumnSuggestions();
                }
            } else if (e.key === 'Escape') {
                hideFilterColumnSuggestions();
            }
        });

        $(document).on('click', '#filter-col-suggestions .label-suggestion', function() {
            var name = String($(this).attr('data-name') || '');
            $('#filter-col-input').val(name);
            $('#filter-col-value').val(name);
            hideFilterColumnSuggestions();
        });

        $(document).on('focus input', '.filter-user-input', function() {
            var $picker = $(this).closest('.filter-user-picker');
            if (!$(this).val().trim()) {
                $picker.find('input[type=hidden]').val('');
                $picker.attr('data-unassigned', '0');
                $picker.attr('data-unsubscribed', '0');
            }
            renderFilterUserSuggestions($picker, $(this).val());
        });

        $(document).on('keydown', '.filter-user-input', function(e) {
            var $picker = $(this).closest('.filter-user-picker');
            var $items = $picker.find('.assign-suggestion');
            if (!$items.length) return;
            if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
                e.preventDefault();
                filterUserIndex = filterUserIndex < 0 ? 0 : filterUserIndex;
                filterUserIndex += e.key === 'ArrowDown' ? 1 : -1;
                if (filterUserIndex < 0) filterUserIndex = $items.length - 1;
                if (filterUserIndex >= $items.length) filterUserIndex = 0;
                $items.removeClass('active').eq(filterUserIndex).addClass('active');
            } else if (e.key === 'Enter') {
                e.preventDefault();
                var $active = $items.filter('.active').first();
                if (!$active.length) $active = $items.first();
                $active.trigger('click');
            } else if (e.key === 'Escape') {
                $picker.find('.filter-user-suggestions').hide();
            }
        });

        $(document).on('click', '.filter-user-picker .assign-suggestion', function() {
            var $picker = $(this).closest('.filter-user-picker');
            var userId = String($(this).attr('data-user-id') || '');
            var username = $(this).find('span').first().text();
            $picker.find('input[type=hidden]').val(userId);
            $picker.attr('data-unassigned', userId === '__unassigned__' ? '1' : '0');
            $picker.attr('data-unsubscribed', userId === '__unsubscribed__' ? '1' : '0');
            $picker.find('.filter-user-input').val(userId ? username : '');
            $picker.find('.filter-user-suggestions').hide();
        });

        function currentCardListQuery() {
            var currentQuery = String($('.card-list-wrap').attr('data-current-query-string') || '');
            var params = new URLSearchParams(currentQuery);
            var visibleCols = saveColVisibility();
            params.set('cols', visibleCols.join(','));
            return params.toString();
        }

        function setViewEditMode(editing) {
            $('#card-list-view-display').toggleClass('hidden', editing);
            $('#card-list-view-edit-form').toggleClass('hidden', !editing);
            if (editing) {
                $('#card-list-view-name-input').trigger('focus').trigger('select');
            }
        }

        function saveActiveView() {
            var viewId = String($('.card-list-wrap').attr('data-view-id') || '');
            if (!viewId) return;
            var editing = !$('#card-list-view-edit-form').hasClass('hidden');
            var name = editing ? $('#card-list-view-name-input').val().trim() : $('#card-list-view-title').text().trim();
            if (!name) return;
            var description = editing ? $('#card-list-view-description-input').val().trim() : $('#card-list-view-description').text().trim();
            var qs = currentCardListQuery();
            fetch('/api/views/' + viewId + '/edit', {
                method: 'POST',
                headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
                body: 'name=' + encodeURIComponent(name) +
                    '&description=' + encodeURIComponent(description) +
                    '&query_string=' + encodeURIComponent(qs)
            })
                .then(function(r) { return r.json(); })
                .then(function(v) {
                    if (!v.slug) return;
                    window.location = '/cards/views/' + v.slug + '/';
                })
                .catch(function() {});
        }

        // Bookmark dropdown toggle.
        var $bookmarkToggle = $('#bookmark-toggle');
        var $bookmarkDropdown = $('#bookmark-dropdown');
        $bookmarkToggle.on('click', function(e) {
            e.preventDefault();
            e.stopPropagation();
            $bookmarkDropdown.toggleClass('open');
        });

        var $viewActions = $('#view-actions');
        $('#view-actions-toggle').on('click', function(e) {
            e.preventDefault();
            e.stopPropagation();
            $viewActions.toggleClass('open');
        });

        $('#view-edit-action').on('click', function(e) {
            e.preventDefault();
            $viewActions.removeClass('open');
            setViewEditMode(true);
        });

        $('#view-save-action').on('click', function(e) {
            e.preventDefault();
            saveActiveView();
        });

        $('#view-edit-save').on('click', function(e) {
            e.preventDefault();
            saveActiveView();
        });

        $('#view-edit-cancel').on('click', function(e) {
            e.preventDefault();
            setViewEditMode(false);
            $('#card-list-view-name-input').val($('#card-list-view-title').text().trim());
            $('#card-list-view-description-input').val($('#card-list-view-description').text().trim());
        });

        $('#view-delete-action').on('click', function(e) {
            e.preventDefault();
            var viewId = String($('.card-list-wrap').attr('data-view-id') || '');
            if (!viewId) return;
            window.showConfirmModal('Delete this saved view?', function() {
                fetch('/api/views/' + viewId + '/delete', { method: 'POST' })
                    .then(function(r) { return r.json(); })
                    .then(function(res) {
                        if (res && res.ok) window.location = '/cards/views/';
                    })
                    .catch(function() {});
            });
        });

        $(document).on('click', function(e) {
            if (!$(e.target).closest('.card-list-action-wrap').length) {
                $bookmarkDropdown.removeClass('open');
                $viewActions.removeClass('open');
            }
            if (!$(e.target).closest('.filter-label-picker').length) {
                hideCardListLabelSuggestions();
            }
            if (!$(e.target).closest('.filter-column-picker').length) {
                hideFilterColumnSuggestions();
            }
            if (!$(e.target).closest('.filter-user-picker').length) {
                $('.filter-user-suggestions').hide();
            }
        });

        // Save view form.
        $('#save-view-form').on('submit', function(e) {
            e.preventDefault();
            var name = $('#save-view-name').val().trim();
            if (!name) return;
            fetch('/api/views/', {
                method: 'POST',
                headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
                body: 'name=' + encodeURIComponent(name) +
                    '&query_string=' + encodeURIComponent(currentCardListQuery())
            })
                .then(function(r) { return r.json(); })
                .then(function(v) {
                    if (!v.id) return;
                    // Add to side nav views list.
                    var $label = $('#side-nav-views-label');
                    var $list = $('#side-nav-views');
                    $label.removeClass('hidden');
                    var $a = $('<a class="side-nav-board-link"></a>');
                    $a.attr('href', '/cards/views/' + v.slug + '/');
                    $a.text(v.name);
                    $list.prepend($a);
                    $list.children().slice(10).remove();
                    // Reset form and close dropdown.
                    $('#save-view-name').val('');
                    $bookmarkDropdown.removeClass('open');
                })
                .catch(function() {});
        });
    }
});
